package appliance

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func migrateAdaptiveLexicons(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM spaces ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list adaptive lexicon Spaces: %w", err)
	}
	var spaceIDs []v1alpha1.SpaceID
	for rows.Next() {
		var spaceID v1alpha1.SpaceID
		if err := rows.Scan(&spaceID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read adaptive lexicon Space: %w", err)
		}
		spaceIDs = append(spaceIDs, spaceID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("list adaptive lexicon Spaces: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close adaptive lexicon Spaces: %w", err)
	}
	migrationStore := &Store{now: func() time.Time { return time.Now().UTC() }, lexiconPolicy: normalizeLexiconPolicy(nil)}
	for _, spaceID := range spaceIDs {
		now := formatTime(migrationStore.now())
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO space_lexicons(space_id, algorithm_version, updated_at) VALUES (?, ?, ?)`,
			spaceID, lexiconAlgorithmVersion, now); err != nil {
			return fmt.Errorf("initialize adaptive lexicon: %w", err)
		}
		receipts, err := tx.QueryContext(ctx,
			`SELECT r.receipt_id, r.commit_sequence, r.text
			 FROM receipts r
			 WHERE r.space_id = ? AND NOT EXISTS (
			  SELECT 1 FROM receipt_corrections c WHERE c.original_receipt_id = r.receipt_id
			 ) ORDER BY r.commit_sequence`, spaceID)
		if err != nil {
			return fmt.Errorf("read adaptive lexicon history: %w", err)
		}
		type receipt struct {
			id       v1alpha1.ReceiptID
			sequence int64
			text     string
		}
		var history []receipt
		for receipts.Next() {
			var item receipt
			if err := receipts.Scan(&item.id, &item.sequence, &item.text); err != nil {
				_ = receipts.Close()
				return fmt.Errorf("read adaptive lexicon history: %w", err)
			}
			history = append(history, item)
		}
		if err := receipts.Err(); err != nil {
			_ = receipts.Close()
			return fmt.Errorf("read adaptive lexicon history: %w", err)
		}
		if err := receipts.Close(); err != nil {
			return fmt.Errorf("close adaptive lexicon history: %w", err)
		}
		for _, item := range history {
			if _, err := migrationStore.learnReceiptLexicon(ctx, tx, spaceID, item.id, item.sequence, item.text); err != nil {
				return fmt.Errorf("learn adaptive lexicon history: %w", err)
			}
		}
		terms, err := readActiveLexiconTerms(ctx, tx, spaceID)
		if err != nil {
			return fmt.Errorf("read migrated lexicon terms: %w", err)
		}
		receiptTable := spaceIndexTable(spaceID)
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+receiptTable); err != nil {
			return fmt.Errorf("clear migrated receipt projection: %w", err)
		}
		if err := rebuildReceiptProjection(ctx, tx, spaceID, receiptTable, terms); err != nil {
			return err
		}
		semanticTable := semanticSpaceIndexTable(spaceID)
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+semanticTable); err != nil {
			return fmt.Errorf("clear migrated semantic projection: %w", err)
		}
		if err := rebuildSemanticProjection(ctx, tx, spaceID, semanticTable, terms); err != nil {
			return err
		}
		if err := markLexiconIndexed(ctx, tx, spaceID, now); err != nil {
			return fmt.Errorf("publish migrated lexicon generation: %w", err)
		}
	}
	return nil
}
