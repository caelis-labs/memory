package appliance

import (
	"context"
	"database/sql"
	"fmt"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// Inspect returns bounded topology and row counts without bearer credentials or
// receipt text.
func (s *Store) Inspect(ctx context.Context) (Inspection, error) {
	result := Inspection{
		ProtocolVersion: managementv1alpha1.ProtocolVersion,
		SchemaVersion:   CurrentSchemaVersion,
		Generation:      s.generation,
		Counts:          make(map[string]int64),
	}
	for _, table := range []string{
		"realms", "identities", "spaces", "views", "grants", "capabilities",
		"receipts", "receipt_corrections", "receipt_tombstones",
	} {
		var count int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			return Inspection{}, fmt.Errorf("count %s: %w", table, err)
		}
		result.Counts[table] = count
	}
	var ftsCount int64
	indexRows, err := s.db.QueryContext(ctx, `SELECT table_name FROM space_indexes ORDER BY space_id`)
	if err != nil {
		return Inspection{}, fmt.Errorf("list Space indexes: %w", err)
	}
	for indexRows.Next() {
		var tableName string
		if err := indexRows.Scan(&tableName); err != nil {
			_ = indexRows.Close()
			return Inspection{}, fmt.Errorf("read Space index: %w", err)
		}
		var count int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableName).Scan(&count); err != nil {
			_ = indexRows.Close()
			return Inspection{}, fmt.Errorf("count Space index: %w", err)
		}
		ftsCount += count
	}
	if err := indexRows.Close(); err != nil {
		return Inspection{}, fmt.Errorf("close Space indexes: %w", err)
	}
	if err := indexRows.Err(); err != nil {
		return Inspection{}, fmt.Errorf("list Space indexes: %w", err)
	}
	result.Counts["receipt_fts"] = ftsCount
	rows, err := s.db.QueryContext(ctx, `SELECT id, realm_id, identity_id, class FROM spaces ORDER BY id`)
	if err != nil {
		return Inspection{}, fmt.Errorf("list Spaces: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var space Space
		var identity sql.NullString
		if err := rows.Scan(&space.ID, &space.RealmID, &identity, &space.Class); err != nil {
			return Inspection{}, fmt.Errorf("read Space: %w", err)
		}
		if identity.Valid {
			space.IdentityID = v1alpha1.IdentityID(identity.String)
		}
		result.Spaces = append(result.Spaces, space)
	}
	if err := rows.Err(); err != nil {
		return Inspection{}, fmt.Errorf("list Spaces: %w", err)
	}
	return result, nil
}

// RebuildFTS reconstructs the disposable lexical projection solely from
// immutable receipts.
func (s *Store) RebuildFTS(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin FTS rebuild: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT space_id, table_name FROM space_indexes ORDER BY space_id`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("list Space indexes: %w", err)
	}
	type index struct {
		spaceID   v1alpha1.SpaceID
		tableName string
	}
	var indexes []index
	for rows.Next() {
		var item index
		if err := rows.Scan(&item.spaceID, &item.tableName); err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			return fmt.Errorf("read Space index: %w", err)
		}
		indexes = append(indexes, item)
	}
	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("close Space indexes: %w", err)
	}
	if err := rows.Err(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("list Space indexes: %w", err)
	}
	for _, item := range indexes {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+item.tableName); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("clear Space FTS projection: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO `+item.tableName+`(receipt_id, text)
			 SELECT receipt_id, text FROM receipts WHERE space_id = ? ORDER BY commit_sequence`, item.spaceID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rebuild Space FTS projection: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit FTS rebuild: %w", err)
	}
	return nil
}

func createSpaceIndex(ctx context.Context, tx *sql.Tx, spaceID v1alpha1.SpaceID) error {
	tableName := spaceIndexTable(spaceID)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO space_indexes(space_id, table_name) VALUES (?, ?)`, spaceID, tableName); err != nil {
		return fmt.Errorf("record Space %q index: %w", spaceID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`CREATE VIRTUAL TABLE `+tableName+` USING fts5(
			receipt_id UNINDEXED,
			text,
			tokenize = 'unicode61'
		)`); err != nil {
		return fmt.Errorf("create Space %q index: %w", spaceID, err)
	}
	return nil
}

func spaceIndexTable(spaceID v1alpha1.SpaceID) string {
	return "receipt_fts_" + digestString(string(spaceID))
}
