package appliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// RotateManagementCredential replaces the fixed owner-only token file without
// returning the bearer over the management API. Pending digest recovery makes
// interruption before or after the file rename deterministic on restart.
func (s *Store) RotateManagementCredential(ctx context.Context) error {
	s.managementRotateMu.Lock()
	defer s.managementRotateMu.Unlock()

	credential, err := s.randomToken(32)
	if err != nil {
		return fmt.Errorf("create management credential: %w", err)
	}
	suffix, err := s.randomHex(8)
	if err != nil {
		return fmt.Errorf("create management credential staging identity: %w", err)
	}
	path := s.ManagementCredentialPath()
	if err := requireRegularOrAbsent(path); err != nil {
		return fmt.Errorf("secure management credential path: %w", err)
	}
	temporary := filepath.Join(s.dataDir, ".management-rotation-"+suffix+".token")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create rotated management credential: %w", err)
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.WriteString(credential + "\n"); err != nil {
		return fmt.Errorf("write rotated management credential: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync rotated management credential: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close rotated management credential: %w", err)
	}
	digest := digestString(credential)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO metadata(key, value) VALUES ('management_credential_digest_pending', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, digest); err != nil {
		return fmt.Errorf("prepare management credential rotation: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM metadata WHERE key = 'management_credential_digest_pending'`)
		return fmt.Errorf("install rotated management credential: %w", err)
	}
	cleanup = false
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("rotated management credential digest is invalid")
	}
	s.managementMu.Lock()
	copy(s.managementSum[:], decoded)
	s.managementMu.Unlock()
	if err := syncDirectory(s.dataDir); err != nil {
		return fmt.Errorf("sync rotated management credential: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("finalize management credential rotation: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE metadata SET value = ? WHERE key = 'management_credential_digest'`, digest); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("finalize management credential rotation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM metadata WHERE key = 'management_credential_digest_pending'`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("finalize management credential rotation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("finalize management credential rotation: %w", err)
	}
	return nil
}
