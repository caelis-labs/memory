package appliance

import (
	"context"
	"database/sql"
	"fmt"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func migrateSemanticSpaceIndexes(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM spaces ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list existing semantic Spaces: %w", err)
	}
	var spaceIDs []v1alpha1.SpaceID
	for rows.Next() {
		var spaceID v1alpha1.SpaceID
		if err := rows.Scan(&spaceID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read existing semantic Space: %w", err)
		}
		spaceIDs = append(spaceIDs, spaceID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("list existing semantic Spaces: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close existing semantic Spaces: %w", err)
	}
	for _, spaceID := range spaceIDs {
		if err := createSemanticSpaceIndex(ctx, tx, spaceID); err != nil {
			return err
		}
	}
	return nil
}

func createSemanticSpaceIndex(ctx context.Context, tx *sql.Tx, spaceID v1alpha1.SpaceID) error {
	tableName := semanticSpaceIndexTable(spaceID)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO semantic_space_indexes(space_id, table_name) VALUES (?, ?)`, spaceID, tableName); err != nil {
		return fmt.Errorf("record Space %q semantic index: %w", spaceID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`CREATE VIRTUAL TABLE `+tableName+` USING fts5(
			record_id UNINDEXED,
			revision UNINDEXED,
			text,
			tokenize = 'unicode61'
		)`); err != nil {
		return fmt.Errorf("create Space %q semantic index: %w", spaceID, err)
	}
	return nil
}

func readSemanticSpaceIndex(ctx context.Context, db databaseExecutor, spaceID v1alpha1.SpaceID) (string, error) {
	var tableName string
	if err := db.QueryRowContext(ctx,
		`SELECT table_name FROM semantic_space_indexes WHERE space_id = ?`, spaceID).Scan(&tableName); err != nil {
		return "", err
	}
	if tableName != semanticSpaceIndexTable(spaceID) {
		return "", fmt.Errorf("Space semantic index identity mismatch")
	}
	return tableName, nil
}

func semanticSpaceIndexTable(spaceID v1alpha1.SpaceID) string {
	return "semantic_fts_" + digestString(string(spaceID))
}
