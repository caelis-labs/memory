package appliance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// Export writes one consistent, versioned NDJSON snapshot of management-visible
// evidence. The caller owns protecting the plaintext output.
func (s *Store) Export(ctx context.Context, request managementv1alpha1.ExportRequest, output io.Writer) error {
	if output == nil {
		return s.serviceError(v1alpha1.ErrorCodeInvalidArgument, "export output is required", false)
	}
	if s.closing.Load() {
		return s.serviceError(v1alpha1.ErrorCodeUnavailable, "memoryd is shutting down", true)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return s.databaseError("begin management export", err)
	}
	defer tx.Rollback()
	if request.SpaceID != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM spaces WHERE id = ?)`, request.SpaceID).Scan(&exists); err != nil {
			return s.databaseError("validate export Space", err)
		}
		if !exists {
			return s.serviceError(v1alpha1.ErrorCodeNotFound, "Space not found", false)
		}
	}
	encoder := json.NewEncoder(output)
	createdAt := s.now().UTC()
	if err := encoder.Encode(managementv1alpha1.ExportRecord{
		Kind: managementv1alpha1.ExportRecordHeader, Format: managementv1alpha1.ExportFormat,
		Generation: s.generation, CreatedAt: &createdAt,
	}); err != nil {
		return s.serviceError(v1alpha1.ErrorCodeUnavailable, "write export header", true)
	}
	predicate := `(? = '' OR r.space_id = ?)`
	if !request.IncludeCorrected {
		predicate += ` AND NOT EXISTS (
			SELECT 1 FROM receipt_corrections c WHERE c.original_receipt_id = r.receipt_id
		)`
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT r.receipt_id, r.space_id, r.label_set, r.label_set_digest,
		 r.text, r.source_context, r.occurred_at,
		 r.received_at, r.commit_sequence, p.state,
		 COALESCE((SELECT replacement_receipt_id FROM receipt_corrections WHERE original_receipt_id = r.receipt_id), ''),
		 COALESCE((SELECT original_receipt_id FROM receipt_corrections WHERE replacement_receipt_id = r.receipt_id), '')
		 FROM receipts r JOIN receipt_processing p ON p.receipt_id = r.receipt_id
		 WHERE `+predicate+`
		 ORDER BY r.space_id, r.commit_sequence, r.receipt_id`, request.SpaceID, request.SpaceID)
	if err != nil {
		return s.databaseError("list export receipts", err)
	}
	for rows.Next() {
		var receipt managementv1alpha1.Receipt
		if err := scanManagementReceipt(rows, &receipt); err != nil {
			_ = rows.Close()
			return s.serviceError(v1alpha1.ErrorCodeInternal, "stored export receipt is invalid", false)
		}
		if err := encoder.Encode(managementv1alpha1.ExportRecord{Kind: managementv1alpha1.ExportRecordReceipt, Receipt: &receipt}); err != nil {
			_ = rows.Close()
			return s.serviceError(v1alpha1.ErrorCodeUnavailable, "write export receipt", true)
		}
	}
	if err := rows.Close(); err != nil {
		return s.databaseError("close export receipts", err)
	}
	if err := rows.Err(); err != nil {
		return s.databaseError("list export receipts", err)
	}
	if request.IncludeDeleted {
		tombstones, err := tx.QueryContext(ctx,
			`SELECT tombstone_id, receipt_id, space_id, deleted_at, reason
			 FROM receipt_tombstones
			 WHERE (? = '' OR space_id = ?)
			 ORDER BY space_id, deleted_at, receipt_id`, request.SpaceID, request.SpaceID)
		if err != nil {
			return s.databaseError("list export tombstones", err)
		}
		for tombstones.Next() {
			var tombstone managementv1alpha1.Tombstone
			var deletedAt string
			if err := tombstones.Scan(
				&tombstone.TombstoneID, &tombstone.ReceiptID, &tombstone.SpaceID, &deletedAt, &tombstone.Reason,
			); err != nil {
				_ = tombstones.Close()
				return s.databaseError("read export tombstone", err)
			}
			tombstone.DeletedAt, err = parseTime(deletedAt)
			if err != nil {
				_ = tombstones.Close()
				return s.serviceError(v1alpha1.ErrorCodeInternal, "stored tombstone time is invalid", false)
			}
			if err := encoder.Encode(managementv1alpha1.ExportRecord{Kind: managementv1alpha1.ExportRecordTombstone, Tombstone: &tombstone}); err != nil {
				_ = tombstones.Close()
				return s.serviceError(v1alpha1.ErrorCodeUnavailable, "write export tombstone", true)
			}
		}
		if err := tombstones.Close(); err != nil {
			return s.databaseError("close export tombstones", err)
		}
		if err := tombstones.Err(); err != nil {
			return s.databaseError("list export tombstones", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return s.databaseError("complete management export", err)
	}
	return nil
}

// BackupSnapshot is a short-lived consistent SQLite snapshot. Close removes
// the plaintext temporary file from the owner-only appliance directory.
type BackupSnapshot struct {
	file *os.File
	path string
	size int64
}

func (b *BackupSnapshot) Read(output []byte) (int, error) {
	return b.file.Read(output)
}

func (b *BackupSnapshot) Size() int64 {
	return b.size
}

func (b *BackupSnapshot) Close() error {
	if b == nil {
		return nil
	}
	return errors.Join(b.file.Close(), os.Remove(b.path))
}

// CreateBackupSnapshot produces a consistent SQLite image before returning any
// response bytes, allowing transport errors to remain distinguishable.
func (s *Store) CreateBackupSnapshot(ctx context.Context) (*BackupSnapshot, error) {
	if s.closing.Load() {
		return nil, s.serviceError(v1alpha1.ErrorCodeUnavailable, "memoryd is shutting down", true)
	}
	suffix, err := s.randomHex(16)
	if err != nil {
		return nil, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to create backup identity", false)
	}
	path := filepath.Join(s.dataDir, ".memory-backup-"+suffix+".db")
	if err := requireRegularOrAbsent(path); err != nil {
		return nil, s.serviceError(v1alpha1.ErrorCodeInternal, "backup path is unsafe", false)
	}
	cleanup := func() { _ = os.Remove(path) }
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		cleanup()
		return nil, s.databaseError("create consistent backup snapshot", err)
	}
	if err := secureOwnerPath(path, 0o600); err != nil {
		cleanup()
		return nil, s.serviceError(v1alpha1.ErrorCodeUnavailable, "secure backup snapshot", true)
	}
	if err := verifySQLiteFile(ctx, path, false); err != nil {
		cleanup()
		return nil, s.serviceError(v1alpha1.ErrorCodeInternal, "backup snapshot failed integrity verification", false)
	}
	file, err := os.Open(path)
	if err != nil {
		cleanup()
		return nil, s.serviceError(v1alpha1.ErrorCodeUnavailable, "open backup snapshot", true)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		cleanup()
		return nil, s.serviceError(v1alpha1.ErrorCodeUnavailable, "inspect backup snapshot", true)
	}
	return &BackupSnapshot{file: file, path: path, size: info.Size()}, nil
}

func verifySQLiteFile(ctx context.Context, path string, foreignKeys bool) error {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer database.Close()
	if foreignKeys {
		if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
			return err
		}
	}
	var integrity string
	if err := database.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity_check: %s", integrity)
	}
	if foreignKeys {
		rows, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			return fmt.Errorf("SQLite foreign_key_check reported a violation")
		}
		return rows.Err()
	}
	return nil
}
