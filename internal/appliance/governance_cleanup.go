package appliance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// Forgetting barrier kinds. A deletion barrier authorizes physical content
// cleansing of transitively derived history. A correction barrier only
// invalidates prior derived use and preserves the wrong-source audit.
const (
	forgettingKindReceiptDeleted   = "receipt_deleted"
	forgettingKindReceiptCorrected = "receipt_corrected"
)

// Memory ledger kinds written for owner change notification.
const (
	memoryChangeReceiptDeleted    = "receipt_deleted"
	memoryChangeReceiptCorrected  = "receipt_corrected"
	memoryChangeRecordForgotten   = "record_forgotten"
	memoryChangeRecordInvalidated = "record_invalidated"
)

const forgettingBarrierTable = "forgetting_barriers"

// governanceMigrationSQL is the additive M02 governance schema owned by this
// package. It is applied by the parent migration orchestration after the facts
// revision rebuild (which adds semantic_revisions.fact_json) and before any
// Steward migration. It is never appended to schema_baseline.go.
//
// The replacement trigger keeps the v0.5.0 immutability invariant for every
// Revision except the exact content-cleansing update authorized by a committed
// deletion barrier: text and fact_json may be cleared, nothing else may change.
var governanceMigrationSQL = []string{
	`CREATE TABLE IF NOT EXISTS forgetting_barriers (
		barrier_sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		space_id TEXT NOT NULL REFERENCES spaces(id),
		label_set_digest TEXT NOT NULL,
		kind TEXT NOT NULL CHECK (kind IN ('receipt_deleted', 'receipt_corrected')),
		receipt_id TEXT NOT NULL,
		status TEXT NOT NULL CHECK (status IN ('pending', 'cleaned')),
		created_at TEXT NOT NULL,
		cleaned_at TEXT NOT NULL DEFAULT '',
		UNIQUE (kind, receipt_id)
	) STRICT`,
	`CREATE INDEX IF NOT EXISTS forgetting_barriers_cleanup
	 ON forgetting_barriers(status, barrier_sequence)`,
	`CREATE TABLE IF NOT EXISTS forgetting_barrier_records (
		barrier_sequence INTEGER NOT NULL REFERENCES forgetting_barriers(barrier_sequence),
		record_id TEXT NOT NULL,
		space_id TEXT NOT NULL REFERENCES spaces(id),
		PRIMARY KEY (barrier_sequence, record_id)
	) STRICT`,
	`CREATE INDEX IF NOT EXISTS forgetting_barrier_records_record
	 ON forgetting_barrier_records(record_id, barrier_sequence)`,
	// Revisions that already existed when this migration runs have no Steward
	// read set, so their derivation can never be reconstructed. They are
	// recorded once here and treated as unattributable by the forgetting
	// closure, which is what forces the conservative widening for legacy data.
	`CREATE TABLE IF NOT EXISTS governance_legacy_revisions AS
	 SELECT record_id, revision FROM semantic_revisions`,
	// The transitive forgetting closure walks steward_read_set by Record, so the
	// Steward-owned table needs a Record-side index. The table itself is created
	// by stewardMigrationSQL, which runs before this migration.
	`CREATE INDEX IF NOT EXISTS steward_read_set_record
	 ON steward_read_set(record_id, revision)`,
	`DROP TRIGGER IF EXISTS semantic_revisions_immutable_update`,
	`CREATE TRIGGER semantic_revisions_immutable_update
	BEFORE UPDATE ON semantic_revisions
	WHEN NOT (
		NEW.record_id IS OLD.record_id AND
		NEW.revision IS OLD.revision AND
		NEW.space_id IS OLD.space_id AND
		NEW.operation IS OLD.operation AND
		NEW.job_id IS OLD.job_id AND
		NEW.created_at IS OLD.created_at AND
		NEW.text = '' AND
		NEW.fact_json = '' AND
		NEW.kind = '' AND
		EXISTS (
			SELECT 1 FROM forgetting_barrier_records br
			JOIN forgetting_barriers f ON f.barrier_sequence = br.barrier_sequence
			WHERE br.record_id = OLD.record_id AND f.kind = 'receipt_deleted' AND f.space_id = OLD.space_id
		)
	)
	BEGIN
		SELECT RAISE(ABORT, 'semantic revision is immutable');
	END`,
}

// sessionCopyBoundaryStatement states the exact scope of an appliance-owned
// deletion without naming a downstream consumer. Producers, hosts, and Session
// stores remain responsible for their own copies.
const sessionCopyBoundaryStatement = "Memory deletes appliance-owned evidence only; copies held by external producers or Session stores must be deleted or redacted separately"

type forgettingBarrier struct {
	sequence    uint64
	spaceID     v1alpha1.SpaceID
	labelDigest string
	kind        string
	receiptID   v1alpha1.ReceiptID
	status      string
	createdAt   string
	cleanedAt   string
}

// derivedRecord identifies one semantic Record invalidated by a receipt.
type derivedRecord struct {
	recordID stewardv1alpha1.RecordID
	spaceID  v1alpha1.SpaceID
}

// insertForgettingBarrier commits the logical barrier inside the mutation
// transaction that removes or shadows receipt evidence. Returned sequence is
// the durable invalidation version for that receipt.
func (s *Store) insertForgettingBarrier(
	ctx context.Context,
	tx *sql.Tx,
	spaceID v1alpha1.SpaceID,
	labelDigest, kind string,
	receiptID v1alpha1.ReceiptID,
) (uint64, error) {
	result, err := tx.ExecContext(ctx,
		`INSERT INTO forgetting_barriers(space_id, label_set_digest, kind, receipt_id, status, created_at)
		 VALUES (?, ?, ?, ?, 'pending', ?)`,
		spaceID, labelDigest, kind, receiptID, formatTime(s.now().UTC()))
	if err != nil {
		return 0, fmt.Errorf("record forgetting barrier: %w", err)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read forgetting barrier sequence: %w", err)
	}
	return uint64(sequence), nil
}

// suppressEvidenceSource prevents the producer source that created a receipt from
// being reprocessed after a deletion or correction. The opaque hashed source key
// is always retained. A deletion also clears the source payload because the
// receipt itself is gone; a correction keeps that payload as owner-visible audit
// and only stops re-ingestion.
func suppressEvidenceSource(ctx context.Context, tx *sql.Tx, receiptID v1alpha1.ReceiptID, clearPayload bool) error {
	statement := `UPDATE evidence_sources SET suppressed = 1 WHERE receipt_id = ?`
	if clearPayload {
		statement = `UPDATE evidence_sources SET suppressed = 1, source_json = '' WHERE receipt_id = ?`
	}
	if _, err := tx.ExecContext(ctx, statement, receiptID); err != nil {
		return fmt.Errorf("suppress evidence source: %w", err)
	}
	return nil
}

// forgettingClosure computes the exact set of semantic Records transitively
// derivable from one forgotten receipt. Direct attribution comes from
// semantic_evidence; indirect attribution comes from the persisted Steward read
// set, so a Record whose text was derived while reading another Record is found
// even when its own Evidence no longer cites the forgotten receipt.
//
// A Record is attributable to a Job when the Job's read set referenced that
// Record or the forgotten receipt, and the Job produced a Revision of it. The
// closure repeats until no new Record or Job is reached.
//
// Legacy Revisions have no read-set attribution. When an affected Record
// contains such a Revision, derivation cannot be bounded, so every Record in
// the same Space and LabelSet partition that also contains un-attributed
// Revisions is conservatively included. This is the smallest safe superset, and
// it is reported through the widended flag so cleanup stays auditable.
type forgettingClosure struct {
	records map[stewardv1alpha1.RecordID]derivedRecord
	jobs    map[stewardv1alpha1.JobID]struct{}
	widened bool
}

func (c forgettingClosure) ordered() []derivedRecord {
	result := make([]derivedRecord, 0, len(c.records))
	for _, record := range c.records {
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].recordID < result[j].recordID })
	return result
}

func (s *Store) forgettingClosure(
	ctx context.Context,
	db databaseExecutor,
	receiptID v1alpha1.ReceiptID,
	spaceID v1alpha1.SpaceID,
	labelDigest string,
) (forgettingClosure, error) {
	closure := forgettingClosure{
		records: make(map[stewardv1alpha1.RecordID]derivedRecord),
		jobs:    make(map[stewardv1alpha1.JobID]struct{}),
	}
	seeds, err := collectDerivedRecords(ctx, db,
		`SELECT DISTINCT e.record_id, e.space_id FROM semantic_evidence e WHERE e.receipt_id = ?`, receiptID)
	if err != nil {
		return forgettingClosure{}, err
	}
	for _, record := range seeds {
		closure.records[record.recordID] = record
	}
	// Jobs whose own receipt is the forgotten receipt, and Jobs that read it.
	seedJobs, err := collectJobs(ctx, db, `SELECT job_id FROM steward_jobs WHERE receipt_id = ?`, receiptID)
	if err != nil {
		return forgettingClosure{}, err
	}
	readSetJobs, err := collectJobs(ctx, db,
		`SELECT DISTINCT job_id FROM steward_read_set WHERE receipt_id = ?`, receiptID)
	if err != nil {
		return forgettingClosure{}, err
	}
	seedJobs = append(seedJobs, readSetJobs...)
	for _, jobID := range seedJobs {
		closure.jobs[jobID] = struct{}{}
	}
	if err := expandForgettingClosure(ctx, db, spaceID, labelDigest, &closure, seeds, seedJobs); err != nil {
		return forgettingClosure{}, err
	}
	widened, err := widenUnattributedClosure(ctx, db, &closure, spaceID, labelDigest)
	if err != nil {
		return forgettingClosure{}, err
	}
	closure.widened = len(widened) != 0
	if len(widened) != 0 {
		if err := expandForgettingClosure(ctx, db, spaceID, labelDigest, &closure, widened, nil); err != nil {
			return forgettingClosure{}, err
		}
	}
	return closure, nil
}

// expandForgettingClosure grows the closure from a worklist of newly attributed
// Records and newly tainted Jobs until a full pass adds nothing new.
//
// Each pass adds at least one entry to a set that is bounded by existing
// database rows, and no entry is ever added twice, so the loop terminates on its
// own. There is deliberately no iteration cap: a capped loop would return a
// partial closure and silently leave forgotten derived content readable. A
// cancelled context aborts instead, which fails the whole mutation
// transaction.
func expandForgettingClosure(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	labelDigest string,
	closure *forgettingClosure,
	newRecords []derivedRecord,
	newJobs []stewardv1alpha1.JobID,
) error {
	for len(newRecords) != 0 || len(newJobs) != 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		var nextRecords []derivedRecord
		var nextJobs []stewardv1alpha1.JobID
		for _, jobID := range newJobs {
			// Every Revision produced by a tainted Job, not only the first: one
			// bounded-batch Job can create several Records.
			produced, err := collectDerivedRecords(ctx, db,
				`SELECT record_id, space_id FROM semantic_revisions WHERE job_id = ?`, jobID)
			if err != nil {
				return err
			}
			nextRecords = append(nextRecords, addDerivedRecords(closure, produced)...)
		}
		for _, record := range newRecords {
			// Jobs that read an attributed Record are tainted by it.
			readers, err := collectJobs(ctx, db,
				`SELECT DISTINCT job_id FROM steward_read_set WHERE record_id = ?`, record.recordID)
			if err != nil {
				return err
			}
			nextJobs = append(nextJobs, addClosureJobs(closure, readers)...)
			// Structured fact metadata is a declared dependency edge with no read
			// set: a Revision whose fact_json relates to an attributed Record was
			// derived from it. The relation is partition-local by contract.
			referrers, err := collectDerivedRecords(ctx, db,
				`SELECT DISTINCT r.record_id, r.space_id
				 FROM semantic_revisions v
				 JOIN semantic_records r ON r.record_id = v.record_id
				 WHERE r.space_id = ? AND r.label_set_digest = ? AND v.fact_json != ''
				   AND json_extract(v.fact_json, '$.related_record_id') = ?`,
				spaceID, labelDigest, string(record.recordID))
			if err != nil {
				return err
			}
			nextRecords = append(nextRecords, addDerivedRecords(closure, referrers)...)
		}
		newRecords, newJobs = nextRecords, nextJobs
	}
	return nil
}

// addDerivedRecords inserts Records that are not yet attributed and returns
// only the newly added ones, which keeps each closure pass strictly growing.
func addDerivedRecords(closure *forgettingClosure, records []derivedRecord) []derivedRecord {
	var added []derivedRecord
	for _, record := range records {
		if _, exists := closure.records[record.recordID]; exists {
			continue
		}
		closure.records[record.recordID] = record
		added = append(added, record)
	}
	return added
}

func addClosureJobs(closure *forgettingClosure, jobs []stewardv1alpha1.JobID) []stewardv1alpha1.JobID {
	var added []stewardv1alpha1.JobID
	for _, jobID := range jobs {
		if _, exists := closure.jobs[jobID]; exists {
			continue
		}
		closure.jobs[jobID] = struct{}{}
		added = append(added, jobID)
	}
	return added
}

func collectDerivedRecords(
	ctx context.Context,
	db databaseExecutor,
	query string,
	args ...any,
) ([]derivedRecord, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find derived Records: %w", err)
	}
	defer rows.Close()
	var records []derivedRecord
	for rows.Next() {
		var record derivedRecord
		if err := rows.Scan(&record.recordID, &record.spaceID); err != nil {
			return nil, fmt.Errorf("read derived Record: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func collectJobs(
	ctx context.Context,
	db databaseExecutor,
	query string,
	args ...any,
) ([]stewardv1alpha1.JobID, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find attributable Steward jobs: %w", err)
	}
	defer rows.Close()
	var jobs []stewardv1alpha1.JobID
	for rows.Next() {
		var jobID stewardv1alpha1.JobID
		if err := rows.Scan(&jobID); err != nil {
			return nil, fmt.Errorf("read attributable Steward job: %w", err)
		}
		jobs = append(jobs, jobID)
	}
	return jobs, rows.Err()
}

// widenUnattributedClosure conservatively attributes every pre-M02 Record in the
// same Space and LabelSet partition as soon as anything is attributed. A
// pre-migration Worker could have read partition Records without any recorded
// reference, so its derivation cannot be ruled out for any of them; sweeping the
// whole legacy partition is the honest safe superset. Fully attributed (v0.6)
// Records are still reached only through the read-set and metadata closure.
// It returns the newly added Records so the caller can continue the fixpoint.
func widenUnattributedClosure(
	ctx context.Context,
	db databaseExecutor,
	closure *forgettingClosure,
	spaceID v1alpha1.SpaceID,
	labelDigest string,
) ([]derivedRecord, error) {
	if len(closure.records) == 0 {
		return nil, nil
	}
	found, err := collectDerivedRecords(ctx, db,
		`SELECT DISTINCT r.record_id, r.space_id
		 FROM semantic_records r
		 JOIN governance_legacy_revisions l ON l.record_id = r.record_id
		 WHERE r.space_id = ? AND r.label_set_digest = ?`,
		spaceID, labelDigest)
	if err != nil {
		return nil, err
	}
	return addDerivedRecords(closure, found), nil
}

// recordGovernanceChange is the single seam onto the owner change ledger. The
// sequence it returns is the host-visible cursor for the change.
func (s *Store) recordGovernanceChange(
	ctx context.Context,
	tx *sql.Tx,
	spaceID v1alpha1.SpaceID,
	labelDigest, kind string,
	receiptID v1alpha1.ReceiptID,
	recordID stewardv1alpha1.RecordID,
) (uint64, error) {
	return s.recordMemoryChange(ctx, tx, spaceID, labelDigest, kind, receiptID, string(recordID))
}

// persistForgettingClosure records the exact Record set a barrier attributes to
// one receipt. It is the durable, restart-stable target of managed cleansing
// and the authority the immutability trigger consults, so a later phase can
// never cleanse a Record the closure did not name.
func (s *Store) persistForgettingClosure(
	ctx context.Context,
	tx *sql.Tx,
	barrierSequence uint64,
	closure forgettingClosure,
) error {
	for _, record := range closure.ordered() {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO forgetting_barrier_records(barrier_sequence, record_id, space_id) VALUES (?, ?, ?)`,
			barrierSequence, record.recordID, record.spaceID); err != nil {
			return fmt.Errorf("record barrier derived Record: %w", err)
		}
	}
	return nil
}

func (s *Store) invalidateForgettingClosure(
	ctx context.Context,
	tx *sql.Tx,
	closure forgettingClosure,
	receiptID v1alpha1.ReceiptID,
	reason string,
) error {
	now := formatTime(s.now().UTC())
	for _, record := range closure.ordered() {
		tableName, err := readSemanticSpaceIndex(ctx, tx, record.spaceID)
		if err != nil {
			return fmt.Errorf("resolve invalidation Space index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableName+` WHERE record_id = ?`, record.recordID); err != nil {
			return fmt.Errorf("remove invalidated semantic projection: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE semantic_records SET status = 'invalidated', invalidated_reason = ?, updated_at = ?
			 WHERE record_id = ? AND status = 'active'`, reason, now, record.recordID); err != nil {
			return fmt.Errorf("invalidate semantic Record: %w", err)
		}
	}
	for jobID := range closure.jobs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE steward_jobs
			 SET state = 'failed', lease_expires_at = NULL, lease_token_digest = '', terminal_error_code = ?, updated_at = ?
			 WHERE job_id = ? AND state IN ('pending', 'leased')`, reason, now, jobID); err != nil {
			return fmt.Errorf("cancel attributable Steward job: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE steward_jobs
		 SET state = 'failed', lease_expires_at = NULL, lease_token_digest = '', terminal_error_code = ?, updated_at = ?
		 WHERE receipt_id = ? AND state IN ('pending', 'leased')`, reason, now, receiptID); err != nil {
		return fmt.Errorf("cancel receipt Steward job: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE receipt_processing
		 SET state = 'failed', last_attempt_at = ?, terminal_error_code = ?
		 WHERE receipt_id = ? AND state IN ('accepted', 'processing')
		   AND EXISTS (
			SELECT 1 FROM steward_jobs j
			WHERE j.receipt_id = ? AND j.state = 'failed' AND j.terminal_error_code = ?
		   )`, now, reason, receiptID, receiptID, reason); err != nil {
		return fmt.Errorf("fail governed receipt semantic processing: %w", err)
	}
	return nil
}

func (s *Store) recordReceiptChange(
	ctx context.Context,
	tx *sql.Tx,
	spaceID v1alpha1.SpaceID,
	labelDigest, kind string,
	receiptID v1alpha1.ReceiptID,
	closure forgettingClosure,
	recordKind string,
) error {
	records := closure.ordered()
	// The receipt-level row always advances the owner change cursor, even when
	// no derived Record was attributed to the receipt.
	if _, err := s.recordGovernanceChange(ctx, tx, spaceID, labelDigest, kind, receiptID, ""); err != nil {
		return err
	}
	for _, record := range records {
		if _, err := s.recordGovernanceChange(ctx, tx, record.spaceID, labelDigest, recordKind, receiptID, record.recordID); err != nil {
			return err
		}
	}
	return nil
}

// forgottenReceipt reports whether a receipt has a durable deletion barrier.
// Steward claim and apply paths use it so a forgotten receipt can never be
// reprocessed or cited, even while physical cleansing is still pending.
func (s *Store) forgottenReceipt(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	receiptID v1alpha1.ReceiptID,
) (bool, error) {
	var forgotten bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM forgetting_barriers
			WHERE kind = ? AND space_id = ? AND receipt_id = ?
		)`, forgettingKindReceiptDeleted, spaceID, receiptID).Scan(&forgotten); err != nil {
		return false, fmt.Errorf("read receipt forgetting barrier: %w", err)
	}
	return forgotten, nil
}

// forgottenRecord reports whether a Record is attributed to a deletion barrier,
// directly through Evidence or transitively through the recorded read set.
func (s *Store) forgottenRecord(
	ctx context.Context,
	db databaseExecutor,
	recordID stewardv1alpha1.RecordID,
) (bool, error) {
	var forgotten bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM forgetting_barrier_records br
			JOIN forgetting_barriers f ON f.barrier_sequence = br.barrier_sequence
			WHERE br.record_id = ? AND f.kind = ?
		)`, recordID, forgettingKindReceiptDeleted).Scan(&forgotten); err != nil {
		return false, fmt.Errorf("read Record forgetting barrier: %w", err)
	}
	return forgotten, nil
}

// recordGovernanceState summarizes the deletion barriers attributed to one
// Record: the highest invalidation version and whether any cleansing is pending.
type recordGovernanceState struct {
	version uint64
	pending uint64
}

func readRecordGovernanceState(
	ctx context.Context,
	db databaseExecutor,
	recordID stewardv1alpha1.RecordID,
) (recordGovernanceState, error) {
	var state recordGovernanceState
	if err := db.QueryRowContext(ctx,
		`SELECT
		 COALESCE(MAX(f.barrier_sequence), 0),
		 COALESCE(SUM(CASE WHEN f.status = 'pending' THEN 1 ELSE 0 END), 0)
		 FROM forgetting_barrier_records br
		 JOIN forgetting_barriers f ON f.barrier_sequence = br.barrier_sequence
		 WHERE br.record_id = ? AND f.kind = ?`, recordID, forgettingKindReceiptDeleted).Scan(
		&state.version, &state.pending); err != nil {
		return recordGovernanceState{}, fmt.Errorf("read Record governance state: %w", err)
	}
	return state, nil
}

func governanceRecordState(status stewardv1alpha1.RecordStatus, state recordGovernanceState) managementv1alpha1.RecordState {
	switch {
	case state.version != 0 && state.pending != 0:
		return managementv1alpha1.RecordStateForgetting
	case state.version != 0:
		return managementv1alpha1.RecordStateForgotten
	case status == stewardv1alpha1.RecordStatusInvalidated:
		return managementv1alpha1.RecordStateInvalidated
	default:
		return managementv1alpha1.RecordStateActive
	}
}

// blankForgottenRevision removes derived payload from an owner read while a
// deletion barrier exists. A crash between the barrier commit and physical
// cleansing therefore cannot disclose forgotten content. Identity, Space,
// timestamps, operation, and Evidence references survive.
func (s *Store) blankForgottenRevision(
	ctx context.Context,
	db databaseExecutor,
	recordID stewardv1alpha1.RecordID,
	revision *stewardv1alpha1.Revision,
) error {
	state, err := readRecordGovernanceState(ctx, db, recordID)
	if err != nil {
		return err
	}
	if state.version != 0 {
		revision.Text = ""
		revision.Kind = ""
	}
	return nil
}

// blankForgottenRecordHead removes the model-supplied Record kind from an owner
// read of a Record head under a deletion barrier. Record identity, Space,
// LabelSet, status, and timestamps survive so the deletion stays auditable.
//
// The head subject and fact_key are not blanked here because no read path
// exposes them: the management record views never select them, and the Steward
// context builder and the facts reader both require status='active', which a
// forgotten Record never is. Their stored values are cleared by managed
// cleansing instead.
func (s *Store) blankForgottenRecordHead(
	ctx context.Context,
	db databaseExecutor,
	record *stewardv1alpha1.Record,
) error {
	state, err := readRecordGovernanceState(ctx, db, record.RecordID)
	if err != nil {
		return err
	}
	if state.version == 0 {
		return nil
	}
	record.Kind = ""
	return nil
}

// cleanupForgettingBarrier completes managed history cleansing for one
// committed barrier in its own transaction. It is idempotent: re-running it
// after a crash clears nothing new and re-settles nothing new.
func (s *Store) cleanupForgettingBarrier(ctx context.Context, barrier forgettingBarrier) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin governance cleanup: %w", err)
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM forgetting_barriers WHERE barrier_sequence = ?`, barrier.sequence).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return nil
		}
		return rollback(fmt.Errorf("read governance cleanup state: %w", err))
	}
	if status == "cleaned" {
		_ = tx.Rollback()
		return nil
	}
	now := s.now().UTC()
	if barrier.kind == forgettingKindReceiptDeleted {
		if err := s.cleanseDerivedHistory(ctx, tx, barrier, now); err != nil {
			return rollback(err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE forgetting_barriers SET status = 'cleaned', cleaned_at = ?
		 WHERE barrier_sequence = ? AND status = 'pending'`, formatTime(now), barrier.sequence); err != nil {
		return rollback(fmt.Errorf("complete governance cleanup: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit governance cleanup: %w", err)
	}
	return nil
}

// barrierDerivedRecords returns the persisted attributed Record set of one
// barrier. Cleanup never recomputes attribution, so the cleansing target is
// exactly what the barrier committed.
func barrierDerivedRecords(ctx context.Context, db databaseExecutor, barrierSequence uint64) ([]derivedRecord, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT record_id, space_id FROM forgetting_barrier_records
		 WHERE barrier_sequence = ? ORDER BY record_id`, barrierSequence)
	if err != nil {
		return nil, fmt.Errorf("read barrier derived Records: %w", err)
	}
	defer rows.Close()
	var records []derivedRecord
	for rows.Next() {
		var record derivedRecord
		if err := rows.Scan(&record.recordID, &record.spaceID); err != nil {
			return nil, fmt.Errorf("read barrier derived Record: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// cleanseDerivedHistory clears every transitively derived payload attributable
// to a forgotten receipt while keeping the content-free audit skeleton:
// semantic_evidence receipt identities, Revision identity, kind, operation, job
// linkage, and timestamps all survive.
func (s *Store) cleanseDerivedHistory(
	ctx context.Context,
	tx *sql.Tx,
	barrier forgettingBarrier,
	now time.Time,
) error {
	records, err := barrierDerivedRecords(ctx, tx, barrier.sequence)
	if err != nil {
		return err
	}
	for _, record := range records {
		// Every Revision of an attributed Record is cleared, not only the
		// Revision that cites the receipt: a later SUPERSEDE or MERGE derives
		// its text from the Record's earlier Revision contents. Kind is cleared
		// too, because it is free model-supplied text and can itself carry
		// forgotten content.
		if _, err := tx.ExecContext(ctx,
			`UPDATE semantic_revisions SET text = '', fact_json = '', kind = ''
			 WHERE record_id = ? AND space_id = ? AND (text != '' OR fact_json != '' OR kind != '')`,
			record.recordID, record.spaceID); err != nil {
			return fmt.Errorf("cleanse derived Revision contents: %w", err)
		}
		// The head kind is model-supplied text and subject/fact_key are host
		// attribution strings that may carry forgotten fact content. Identity,
		// Space, LabelSet, timestamps, and status all survive.
		if _, err := tx.ExecContext(ctx,
			`UPDATE semantic_records SET kind = '', subject = '', fact_key = ''
			 WHERE record_id = ? AND (kind != '' OR subject != '' OR fact_key != '')`,
			record.recordID); err != nil {
			return fmt.Errorf("cleanse derived Record contents: %w", err)
		}
		tableName, err := readSemanticSpaceIndex(ctx, tx, record.spaceID)
		if err != nil {
			return fmt.Errorf("resolve governance cleanup index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableName+` WHERE record_id = ?`, record.recordID); err != nil {
			return fmt.Errorf("remove forgotten semantic projection: %w", err)
		}
	}
	// Job cleanup is bounded by the surviving Revision skeleton: a Job that
	// still anchors a Revision must stay for that attribution, while a Job with
	// no surviving Revision is removed. Any pending or leased Job that either
	// belongs to the forgotten receipt or read a forgotten Record is settled, so
	// no in-flight Worker can still produce derived state from forgotten content.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM steward_jobs
		 WHERE receipt_id = ?
		   AND NOT EXISTS (
			SELECT 1 FROM semantic_revisions v
			WHERE v.job_id = steward_jobs.job_id AND v.space_id = steward_jobs.space_id
		   )`, barrier.receiptID); err != nil {
		return fmt.Errorf("remove forgotten Steward jobs: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE steward_jobs
		 SET state = 'failed', lease_expires_at = NULL, lease_token_digest = '', terminal_error_code = ?,
		     updated_at = ?
		 WHERE state IN ('pending', 'leased')
		   AND (receipt_id = ? OR EXISTS (
			SELECT 1 FROM steward_read_set rs
			JOIN forgetting_barrier_records br ON br.record_id = rs.record_id
			WHERE rs.job_id = steward_jobs.job_id AND br.barrier_sequence = ?
		   ))`,
		"receipt_forgotten", formatTime(now), barrier.receiptID, barrier.sequence); err != nil {
		return fmt.Errorf("settle forgotten Steward jobs: %w", err)
	}
	return nil
}

// RecoverGovernanceCleanup resumes every committed barrier whose physical
// cleansing did not complete, so a crash between the barrier commit and the
// cleanup transaction can never leave forgotten derived payload readable. It is
// called on Open and after an embedded restore commit.
func (s *Store) RecoverGovernanceCleanup(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT barrier_sequence, space_id, label_set_digest, kind, receipt_id, status, created_at, cleaned_at
		 FROM forgetting_barriers WHERE status = 'pending' ORDER BY barrier_sequence`)
	if err != nil {
		return fmt.Errorf("list pending governance cleanups: %w", err)
	}
	var pending []forgettingBarrier
	for rows.Next() {
		var barrier forgettingBarrier
		if err := rows.Scan(
			&barrier.sequence, &barrier.spaceID, &barrier.labelDigest, &barrier.kind,
			&barrier.receiptID, &barrier.status, &barrier.createdAt, &barrier.cleanedAt,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read pending governance cleanup: %w", err)
		}
		pending = append(pending, barrier)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("list pending governance cleanups: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close pending governance cleanups: %w", err)
	}
	for _, barrier := range pending {
		if err := s.cleanupForgettingBarrier(ctx, barrier); err != nil {
			return err
		}
	}
	return nil
}

// refreshCleanupReporting fills the deletion reporting fields of a replayed or
// reconciled response from the durable barrier state, so a retry of an
// already-committed forget reports the current cleansing state instead of a
// stale stored value.
func (s *Store) refreshCleanupReporting(
	ctx context.Context,
	db databaseExecutor,
	receiptID v1alpha1.ReceiptID,
	response *managementv1alpha1.DeleteReceiptResponse,
) error {
	var version uint64
	var pending int64
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(barrier_sequence), 0),
		 COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0)
		 FROM forgetting_barriers WHERE receipt_id = ?`, receiptID).Scan(&version, &pending); err != nil {
		return s.databaseError("read forgetting barrier state", err)
	}
	response.InvalidationVersion = version
	switch {
	case version == 0:
		response.Cleanup = managementv1alpha1.CleanupStateNone
	case pending != 0:
		response.Cleanup = managementv1alpha1.CleanupStatePending
	default:
		response.Cleanup = managementv1alpha1.CleanupStateCompleted
	}
	return nil
}

// CleanupStatus reports managed history cleansing for one receipt. A caller
// that lost a deletion response polls this instead of retrying the effect.
func (s *Store) CleanupStatus(
	ctx context.Context,
	request managementv1alpha1.CleanupStatusRequest,
) (managementv1alpha1.CleanupStatusResponse, error) {
	if err := request.Validate(); err != nil {
		return managementv1alpha1.CleanupStatusResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	var response managementv1alpha1.CleanupStatusResponse
	var total, pending int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(barrier_sequence), 0),
		 COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
		 COALESCE(MIN(created_at), ''),
		 COALESCE(MAX(cleaned_at), '')
		 FROM forgetting_barriers WHERE receipt_id = ?`, request.ReceiptID).Scan(
		&response.InvalidationVersion, &pending, &response.StartedAt, &response.CompletedAt); err != nil {
		return managementv1alpha1.CleanupStatusResponse{}, s.databaseError("read cleanup status", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM forgetting_barriers WHERE status = 'pending'`).Scan(&total); err != nil {
		return managementv1alpha1.CleanupStatusResponse{}, s.databaseError("count pending governance cleanups", err)
	}
	response.ReceiptID = request.ReceiptID
	response.PendingBarriers = total
	switch {
	case response.InvalidationVersion == 0:
		response.State = managementv1alpha1.CleanupStateNone
		response.StartedAt = ""
		response.CompletedAt = ""
	case pending != 0:
		response.State = managementv1alpha1.CleanupStatePending
		response.CompletedAt = ""
	default:
		response.State = managementv1alpha1.CleanupStateCompleted
	}
	return response, nil
}

// ListRecords pages owner-visible semantic Record heads within one exact Space.
// The cursor is the last Record identity of the previous page, which is stable
// because Record identities are immutable and never reused.
func (s *Store) ListRecords(
	ctx context.Context,
	request managementv1alpha1.ListRecordsRequest,
) (managementv1alpha1.ListRecordsResponse, error) {
	if err := request.Validate(); err != nil {
		return managementv1alpha1.ListRecordsResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM spaces WHERE id = ?)`, request.SpaceID).Scan(&exists); err != nil {
		return managementv1alpha1.ListRecordsResponse{}, s.databaseError("validate record listing Space", err)
	}
	if !exists {
		return managementv1alpha1.ListRecordsResponse{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "Space not found", false)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT record_id, space_id, label_set, label_set_digest, kind, status,
		 current_revision, invalidated_reason, created_at, updated_at
		 FROM semantic_records
		 WHERE space_id = ? AND record_id > ?
		 ORDER BY record_id
		 LIMIT ?`, request.SpaceID, request.Cursor, request.Limit+1)
	if err != nil {
		return managementv1alpha1.ListRecordsResponse{}, s.databaseError("list semantic Records", err)
	}
	response := managementv1alpha1.ListRecordsResponse{Records: make([]managementv1alpha1.RecordSummary, 0)}
	for rows.Next() {
		var record stewardv1alpha1.Record
		var labelSetEncoded, labelSetDigest, createdAt, updatedAt string
		if err := rows.Scan(
			&record.RecordID, &record.SpaceID, &labelSetEncoded, &labelSetDigest,
			&record.Kind, &record.Status, &record.CurrentRevision, &record.InvalidatedReason,
			&createdAt, &updatedAt,
		); err != nil {
			_ = rows.Close()
			return managementv1alpha1.ListRecordsResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Record is invalid", false)
		}
		labels, err := decodeStoredLabelSet(labelSetEncoded, labelSetDigest)
		if err != nil {
			_ = rows.Close()
			return managementv1alpha1.ListRecordsResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Record LabelSet is invalid", false)
		}
		record.Labels = labels.labels
		if record.CreatedAt, err = parseTime(createdAt); err != nil {
			_ = rows.Close()
			return managementv1alpha1.ListRecordsResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Record time is invalid", false)
		}
		if record.UpdatedAt, err = parseTime(updatedAt); err != nil {
			_ = rows.Close()
			return managementv1alpha1.ListRecordsResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Record time is invalid", false)
		}
		state, err := readRecordGovernanceState(ctx, s.db, record.RecordID)
		if err != nil {
			_ = rows.Close()
			return managementv1alpha1.ListRecordsResponse{}, s.databaseError("read semantic Record governance state", err)
		}
		if err := s.blankForgottenRecordHead(ctx, s.db, &record); err != nil {
			_ = rows.Close()
			return managementv1alpha1.ListRecordsResponse{}, s.databaseError("apply forgetting fence", err)
		}
		response.Records = append(response.Records, managementv1alpha1.RecordSummary{
			Record: record,
			State:  governanceRecordState(record.Status, state),
		})
	}
	if err := rows.Close(); err != nil {
		return managementv1alpha1.ListRecordsResponse{}, s.databaseError("list semantic Records", err)
	}
	if err := rows.Err(); err != nil {
		return managementv1alpha1.ListRecordsResponse{}, s.databaseError("list semantic Records", err)
	}
	if len(response.Records) > request.Limit {
		response.Truncated = true
		response.Records = response.Records[:request.Limit]
	}
	if len(response.Records) != 0 {
		response.NextCursor = string(response.Records[len(response.Records)-1].RecordID)
		if !response.Truncated {
			response.NextCursor = ""
		}
	}
	return response, nil
}

// TraceRecord returns one Record head and its immutable Revision audit. A
// forgetting or forgotten Record returns only a content-free skeleton with
// Evidence receipt identities so the deletion stays attributable.
func (s *Store) TraceRecord(
	ctx context.Context,
	request managementv1alpha1.TraceRecordRequest,
) (managementv1alpha1.TraceRecordResponse, error) {
	if err := request.Validate(); err != nil {
		return managementv1alpha1.TraceRecordResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	var record stewardv1alpha1.Record
	var labelSetEncoded, labelSetDigest, createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT record_id, space_id, label_set, label_set_digest, kind, status,
		 current_revision, invalidated_reason, created_at, updated_at
		 FROM semantic_records WHERE record_id = ?`, request.RecordID).Scan(
		&record.RecordID, &record.SpaceID, &labelSetEncoded, &labelSetDigest,
		&record.Kind, &record.Status, &record.CurrentRevision, &record.InvalidatedReason,
		&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return managementv1alpha1.TraceRecordResponse{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "Record not found", false)
	}
	if err != nil {
		return managementv1alpha1.TraceRecordResponse{}, s.databaseError("trace semantic Record", err)
	}
	labels, err := decodeStoredLabelSet(labelSetEncoded, labelSetDigest)
	if err != nil {
		return managementv1alpha1.TraceRecordResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Record LabelSet is invalid", false)
	}
	record.Labels = labels.labels
	if record.CreatedAt, err = parseTime(createdAt); err != nil {
		return managementv1alpha1.TraceRecordResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Record time is invalid", false)
	}
	if record.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return managementv1alpha1.TraceRecordResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Record time is invalid", false)
	}
	governance, err := readRecordGovernanceState(ctx, s.db, record.RecordID)
	if err != nil {
		return managementv1alpha1.TraceRecordResponse{}, s.databaseError("read semantic Record governance state", err)
	}
	if err := s.blankForgottenRecordHead(ctx, s.db, &record); err != nil {
		return managementv1alpha1.TraceRecordResponse{}, s.databaseError("apply forgetting fence", err)
	}
	revisions, err := s.readGovernedRevisions(ctx, s.db, record)
	if err != nil {
		return managementv1alpha1.TraceRecordResponse{}, err
	}
	response := managementv1alpha1.TraceRecordResponse{
		State:               governanceRecordState(record.Status, governance),
		Record:              &record,
		Revisions:           revisions,
		InvalidationVersion: governance.version,
		Cleanup:             managementv1alpha1.CleanupStateCompleted,
	}
	if governance.version != 0 && governance.pending != 0 {
		response.Cleanup = managementv1alpha1.CleanupStatePending
	}
	if governance.version == 0 {
		response.Cleanup = managementv1alpha1.CleanupStateNone
	}
	return response, nil
}

func (s *Store) readGovernedRevisions(
	ctx context.Context,
	db databaseExecutor,
	record stewardv1alpha1.Record,
) ([]stewardv1alpha1.Revision, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT record_id, revision, space_id, kind, text, operation, COALESCE(job_id, ''), created_at
		 FROM semantic_revisions WHERE record_id = ? ORDER BY revision`, record.RecordID)
	if err != nil {
		return nil, s.databaseError("read semantic Revisions", err)
	}
	defer rows.Close()
	revisions := make([]stewardv1alpha1.Revision, 0)
	for rows.Next() {
		var revision stewardv1alpha1.Revision
		var revisionAt string
		if err := rows.Scan(
			&revision.RecordID, &revision.Revision, &revision.SpaceID, &revision.Kind,
			&revision.Text, &revision.Operation, &revision.JobID, &revisionAt,
		); err != nil {
			return nil, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Revision is invalid", false)
		}
		if revision.CreatedAt, err = parseTime(revisionAt); err != nil {
			return nil, s.serviceError(v1alpha1.ErrorCodeInternal, "stored semantic Revision time is invalid", false)
		}
		if err := s.blankForgottenRevision(ctx, db, revision.RecordID, &revision); err != nil {
			return nil, s.databaseError("apply forgetting fence", err)
		}
		evidence, err := readSemanticEvidenceIDs(ctx, db, revision.RecordID, revision.Revision)
		if err != nil {
			return nil, s.databaseError("read semantic Revision Evidence", err)
		}
		for _, receiptID := range evidence {
			revision.Evidence = append(revision.Evidence, stewardv1alpha1.Evidence{ReceiptID: receiptID, SpaceID: revision.SpaceID})
		}
		revisions = append(revisions, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, s.databaseError("read semantic Revisions", err)
	}
	return revisions, nil
}

// inspectGovernanceDiagnostics reports forgetting barrier health without
// exposing receipt or Record identity.
func (s *Store) inspectGovernanceDiagnostics(ctx context.Context, result *Inspection) error {
	var barriers, pending, cleared int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		 COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
		 COALESCE(SUM(CASE WHEN status = 'cleaned' THEN 1 ELSE 0 END), 0)
		 FROM forgetting_barriers`).Scan(&barriers, &pending, &cleared); err != nil {
		return fmt.Errorf("inspect forgetting barriers: %w", err)
	}
	result.Governance.Barriers = barriers
	result.Governance.PendingCleanups = pending
	result.Governance.CompletedCleanups = cleared
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(barrier_sequence), 0) FROM forgetting_barriers`).Scan(
		&result.Governance.LastInvalidationVersion); err != nil {
		return fmt.Errorf("inspect forgetting barrier version: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_revisions v
		 WHERE v.text = '' AND v.fact_json = '' AND v.kind = '' AND EXISTS (
		  SELECT 1 FROM forgetting_barrier_records br
		  JOIN forgetting_barriers f ON f.barrier_sequence = br.barrier_sequence
		  WHERE br.record_id = v.record_id AND f.space_id = v.space_id
		    AND f.kind = 'receipt_deleted' AND f.status = 'cleaned'
		 )`).Scan(
		&result.Governance.ClearedRevisions); err != nil {
		return fmt.Errorf("inspect cleansed revisions: %w", err)
	}
	return nil
}
