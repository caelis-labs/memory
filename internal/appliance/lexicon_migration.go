package appliance

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func migrateAdaptiveLexicons(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO space_lexicons(space_id, algorithm_version, updated_at)
		 SELECT id, ?, ? FROM spaces ORDER BY id`,
		lexiconAlgorithmVersion, formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("initialize dormant adaptive lexicon: %w", err)
	}
	return nil
}

// retireAdaptiveLexiconProjection removes adaptive terms from disposable FTS
// projections created by schema 6 while retaining their evidence for explicit
// offline experiments. This one-time migration makes an upgraded database
// behave exactly like a fresh default runtime.
func retireAdaptiveLexiconProjection(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT space_id FROM lexicon_terms WHERE status = 'active' ORDER BY space_id`)
	if err != nil {
		return fmt.Errorf("list active experimental lexicons: %w", err)
	}
	var spaceIDs []v1alpha1.SpaceID
	for rows.Next() {
		var spaceID v1alpha1.SpaceID
		if err := rows.Scan(&spaceID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read active experimental lexicon: %w", err)
		}
		spaceIDs = append(spaceIDs, spaceID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("list active experimental lexicons: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close active experimental lexicons: %w", err)
	}
	updatedAt := formatTime(time.Now().UTC())
	for _, spaceID := range spaceIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE lexicon_terms SET status = 'retired', updated_at = ?
			 WHERE space_id = ? AND status = 'active'`, updatedAt, spaceID); err != nil {
			return fmt.Errorf("retire experimental lexicon: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE space_lexicons SET generation = generation + 1, updated_at = ? WHERE space_id = ?`,
			updatedAt, spaceID); err != nil {
			return fmt.Errorf("advance retired lexicon generation: %w", err)
		}
		if err := rebuildSpaceLexicalProjection(ctx, tx, spaceID, nil, updatedAt); err != nil {
			return fmt.Errorf("remove experimental lexicon projection: %w", err)
		}
	}
	return nil
}
