package appliance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	steward "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// cancelGovernedStewardJob settles both mutable state machines in the barrier
// transaction. Completed work is immutable history, not a cancellation target.
func cancelGovernedStewardJob(ctx context.Context, tx *sql.Tx, jobID steward.JobID, reason, now string) error {
	var receiptID memory.ReceiptID
	err := tx.QueryRowContext(ctx, `UPDATE steward_jobs
	 SET state = 'failed', lease_expires_at = NULL, lease_token_digest = '',
	     terminal_error_code = ?, updated_at = ?
	 WHERE job_id = ? AND state IN ('pending', 'leased') RETURNING receipt_id`, reason, now, jobID).Scan(&receiptID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cancel governed Steward job: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE receipt_processing
	 SET state = 'failed', last_attempt_at = ?, terminal_error_code = ?
	 WHERE receipt_id = ? AND state IN ('accepted', 'processing')`, now, reason, receiptID); err != nil {
		return fmt.Errorf("settle governed receipt processing: %w", err)
	}
	return nil
}

const governanceProcessingMigration = "data_migration:governance_receipt_processing:v0.6.1"

// migrateGovernanceReceiptProcessing repairs the v0.6.0 cancellation bug once,
// atomically with its marker. Schema 2 and immutable evidence stay unchanged.
// A known failure code alone is insufficient: the failed job must be linked to
// a committed same-partition barrier by its own receipt or persisted read set.
// An aborted repair leaves no marker and is retried on Open.
func (s *Store) migrateGovernanceReceiptProcessing(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var applied bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM metadata WHERE key = ?)`, governanceProcessingMigration).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE receipt_processing AS p
	 SET state = 'failed',
	     last_attempt_at = (SELECT j.updated_at FROM steward_jobs j WHERE j.receipt_id = p.receipt_id),
	     terminal_error_code = (SELECT j.terminal_error_code FROM steward_jobs j WHERE j.receipt_id = p.receipt_id)
	 WHERE p.state IN ('accepted', 'processing') AND EXISTS (
	   SELECT 1 FROM steward_jobs j
	   JOIN receipts r ON r.receipt_id = j.receipt_id
	     AND r.space_id = j.space_id AND r.label_set_digest = j.label_set_digest
	   WHERE j.receipt_id = p.receipt_id AND j.state = 'failed'
	     AND j.terminal_error_code IN ('receipt_deleted', 'receipt_corrected', 'receipt_forgotten')
	     AND EXISTS (
	       SELECT 1 FROM forgetting_barriers b
	       WHERE b.space_id = j.space_id AND b.label_set_digest = j.label_set_digest
	         AND (b.kind = j.terminal_error_code OR (b.kind = 'receipt_deleted' AND j.terminal_error_code = 'receipt_forgotten'))
	         AND (b.receipt_id = j.receipt_id OR EXISTS (
	           SELECT 1 FROM steward_read_set rs WHERE rs.job_id = j.job_id
	             AND (rs.receipt_id = b.receipt_id OR EXISTS (
	               SELECT 1 FROM forgetting_barrier_records br
	               WHERE br.barrier_sequence = b.barrier_sequence AND br.record_id = rs.record_id
	                 AND br.space_id = j.space_id
	             ))
	         ))
	     )
	 )`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key, value) VALUES (?, '1')`, governanceProcessingMigration); err != nil {
		return err
	}
	return tx.Commit()
}
