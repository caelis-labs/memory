package appliance

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
)

// applyStewardOp validates and writes exactly one bounded operation under an
// already-validated lease and read set. It never advances a structured fact
// head: a model proposal may not touch a Record with subject or fact_json set.
func (s *Store) applyStewardOp(
	ctx context.Context,
	tx *sql.Tx,
	job storedStewardJob,
	jobID stewardv1alpha1.JobID,
	op stewardv1alpha1.ProposalOp,
	source *facts.Source,
	now time.Time,
) (stewardv1alpha1.ApplyResultOp, error) {
	result := stewardv1alpha1.ApplyResultOp{Operation: op.Operation}
	if op.Operation == stewardv1alpha1.OperationIgnore {
		return result, nil
	}
	recordID := op.TargetRecordID
	revision := op.ExpectedRevision + 1
	factJSON := ""
	if op.Operation == stewardv1alpha1.OperationAdd {
		suffix, err := s.randomHex(16)
		if err != nil {
			return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("create semantic Record identity: %w", err)
		}
		recordID = stewardv1alpha1.RecordID("record-" + suffix)
		revision = 1
		subject, factKey := "", ""
		if source != nil && source.Subject != "" {
			// A trusted host Source seeds pending structured metadata; the model
			// never authors adoption and only the host may later confirm it.
			subject, factKey = source.Subject, source.FactKey
			encoded, err := json.Marshal(facts.Metadata{
				Subject: subject, Key: factKey,
				Adoption: facts.AdoptionPending, Transition: facts.TransitionEstablish,
			})
			if err != nil {
				return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("encode pending fact metadata: %w", err)
			}
			factJSON = string(encoded)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO semantic_records(record_id, space_id, kind, status, current_revision,
			 created_at, updated_at, label_set, label_set_digest, subject, fact_key)
			 VALUES (?, ?, ?, 'active', 1, ?, ?, ?, ?, ?, ?)`,
			recordID, job.spaceID, op.Kind, formatTime(now), formatTime(now),
			job.labelSetEncoded, job.labelSetDigest, subject, factKey); err != nil {
			return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("create semantic Record: %w", err)
		}
	} else {
		var spaceID v1alpha1.SpaceID
		var kind string
		var status stewardv1alpha1.RecordStatus
		var currentRevision uint64
		var labelSetDigest string
		var subject, factKey, headFactJSON string
		if err := tx.QueryRowContext(ctx,
			`SELECT r.space_id, r.kind, r.status, r.current_revision, r.label_set_digest,
			 r.subject, r.fact_key, v.fact_json
			 FROM semantic_records r
			 JOIN semantic_revisions v ON v.record_id = r.record_id AND v.revision = r.current_revision
			 WHERE r.record_id = ?`,
			recordID).Scan(&spaceID, &kind, &status, &currentRevision, &labelSetDigest,
			&subject, &factKey, &headFactJSON); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return stewardv1alpha1.ApplyResultOp{}, ErrStewardConflict
			}
			return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("read semantic Record head: %w", err)
		}
		if spaceID != job.spaceID || labelSetDigest != job.labelSetDigest ||
			status != stewardv1alpha1.RecordStatusActive || currentRevision != op.ExpectedRevision {
			return stewardv1alpha1.ApplyResultOp{}, ErrStewardConflict
		}
		// A structured fact head (host subject or any fact metadata, including
		// denied) is never model-editable. No downgrade, no in-place overwrite.
		if subject != "" || headFactJSON != "" {
			return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("%w: structured fact head is not model-editable", ErrStewardConflict)
		}
		if op.Operation == stewardv1alpha1.OperationMerge {
			if op.Kind != kind {
				return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("%w: MERGE cannot change Record kind", ErrStewardProposalInvalid)
			}
			priorEvidence, err := readSemanticEvidenceIDs(ctx, tx, recordID, currentRevision)
			if err != nil {
				return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("read MERGE evidence: %w", err)
			}
			for _, evidenceID := range priorEvidence {
				if !slices.Contains(op.EvidenceRefs, evidenceID) {
					return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("%w: MERGE must retain current evidence", ErrStewardProposalInvalid)
				}
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE semantic_records
			 SET kind = ?, status = 'active', current_revision = ?, invalidated_reason = '', updated_at = ?
			 WHERE record_id = ? AND current_revision = ? AND status = 'active'`,
			op.Kind, revision, formatTime(now), recordID, op.ExpectedRevision); err != nil {
			return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("advance semantic Record: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO semantic_revisions(record_id, revision, space_id, kind, text, operation, job_id, created_at, fact_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		recordID, revision, job.spaceID, op.Kind, op.Text, op.Operation, jobID, formatTime(now), factJSON); err != nil {
		return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("append semantic Revision: %w", err)
	}
	for ordinal, receiptID := range op.EvidenceRefs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO semantic_evidence(record_id, revision, ordinal, receipt_id, space_id)
			 VALUES (?, ?, ?, ?, ?)`, recordID, revision, ordinal, receiptID, job.spaceID); err != nil {
			return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("append semantic Evidence: %w", err)
		}
	}
	tableName, err := readSemanticSpaceIndex(ctx, tx, job.spaceID)
	if err != nil {
		return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("resolve semantic Space index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableName+` WHERE record_id = ?`, recordID); err != nil {
		return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("replace semantic projection: %w", err)
	}
	activeTerms, err := s.activeLexiconTerms(ctx, tx, job.spaceID)
	if err != nil {
		return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("read semantic Space lexicon: %w", err)
	}
	if err := indexSemanticProjection(ctx, tx, tableName, recordID, revision, op.Text, activeTerms); err != nil {
		return stewardv1alpha1.ApplyResultOp{}, fmt.Errorf("index semantic Revision: %w", err)
	}
	if _, err := s.recordMemoryChange(ctx, tx, job.spaceID, job.labelSetDigest,
		stewardMemoryChangeKind(op.Operation), job.receiptID, string(recordID)); err != nil {
		return stewardv1alpha1.ApplyResultOp{}, err
	}
	result.RecordID = recordID
	result.Revision = revision
	return result, nil
}

func stewardMemoryChangeKind(operation stewardv1alpha1.Operation) string {
	switch operation {
	case stewardv1alpha1.OperationAdd:
		return "record_added"
	case stewardv1alpha1.OperationMerge:
		return "record_merged"
	default:
		return "record_superseded"
	}
}

// ErrStewardProposalInvalid means untrusted output failed deterministic shape
// or evidence validation.
var ErrStewardProposalInvalid = errors.New("steward proposal is invalid")

// ErrStewardLeaseLost means the caller does not own the current durable Job.
var ErrStewardLeaseLost = errors.New("steward job lease is absent or stale")

// ErrStewardConflict means canonical Record state or an exact retry differs.
var ErrStewardConflict = errors.New("steward proposal conflicts with canonical state")

// ErrStewardUnknownOutcome means the worker must retry the identical lease and
// proposal to resolve whether the transaction committed.
var ErrStewardUnknownOutcome = errors.New("steward proposal commit outcome is unknown")

// StewardLease is appliance-issued execution authority. Its token is never
// passed to a model and is not part of a Proposal.
type StewardLease = stewardv1alpha1.Lease

type storedStewardJob struct {
	receiptID       v1alpha1.ReceiptID
	spaceID         v1alpha1.SpaceID
	profileID       stewardv1alpha1.ProfileID
	profileVersion  uint64
	state           string
	leaseExpiresAt  sql.NullString
	leaseDigest     string
	proposalDigest  string
	resultJSON      string
	attempts        int
	labelSetEncoded string
	labelSetDigest  string
}

// ApplyStewardProposal validates and atomically applies one untrusted model
// proposal under a durable job lease.
func (s *Store) ApplyStewardProposal(
	ctx context.Context,
	lease StewardLease,
	proposal stewardv1alpha1.Proposal,
) (stewardv1alpha1.ApplyResult, error) {
	s.stewardJobMu.Lock()
	defer s.stewardJobMu.Unlock()
	if err := s.requireMutableGeneration(); err != nil {
		return stewardv1alpha1.ApplyResult{}, err
	}
	if err := proposal.ValidateShape(); err != nil {
		return stewardv1alpha1.ApplyResult{}, fmt.Errorf("%w: %v", ErrStewardProposalInvalid, err)
	}
	if lease.JobID == "" || lease.Token == "" {
		return stewardv1alpha1.ApplyResult{}, ErrStewardLeaseLost
	}
	canonical, digest, encodedSize, err := canonicalStewardProposal(proposal)
	if err != nil {
		return stewardv1alpha1.ApplyResult{}, fmt.Errorf("%w: proposal normalization failed", ErrStewardProposalInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stewardv1alpha1.ApplyResult{}, fmt.Errorf("begin Steward proposal: %w", err)
	}
	rollback := func(err error) (stewardv1alpha1.ApplyResult, error) {
		_ = tx.Rollback()
		return stewardv1alpha1.ApplyResult{}, err
	}
	job, err := readStewardJob(ctx, tx, lease.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return rollback(ErrStewardLeaseLost)
	}
	if err != nil {
		return rollback(fmt.Errorf("read Steward job: %w", err))
	}
	if job.state == "completed" {
		if subtle.ConstantTimeCompare([]byte(job.leaseDigest), []byte(digestString(lease.Token))) != 1 {
			return rollback(ErrStewardLeaseLost)
		}
		if subtle.ConstantTimeCompare([]byte(job.proposalDigest), []byte(digest)) != 1 {
			return rollback(ErrStewardConflict)
		}
		var result stewardv1alpha1.ApplyResult
		if err := json.Unmarshal([]byte(job.resultJSON), &result); err != nil {
			return rollback(fmt.Errorf("read completed Steward result: %w", err))
		}
		result.DeduplicatedRetry = true
		return rollbackResult(result, tx)
	}
	if job.state != "leased" || subtle.ConstantTimeCompare([]byte(job.leaseDigest), []byte(digestString(lease.Token))) != 1 {
		return rollback(ErrStewardLeaseLost)
	}
	if !job.leaseExpiresAt.Valid {
		return rollback(ErrStewardLeaseLost)
	}
	leaseExpiry, err := parseTime(job.leaseExpiresAt.String)
	if err != nil || !leaseExpiry.After(s.now()) {
		return rollback(ErrStewardLeaseLost)
	}
	profile, err := readStewardProfile(ctx, tx, job.profileID, job.profileVersion)
	if err != nil {
		return rollback(fmt.Errorf("read Steward proposal profile: %w", err))
	}
	if encodedSize > profile.MaxOutputBytes {
		return rollback(fmt.Errorf("%w: proposal exceeds profile output budget", ErrStewardProposalInvalid))
	}

	if forgotten, err := s.forgottenReceipt(ctx, tx, job.spaceID, job.receiptID); err != nil {
		return rollback(err)
	} else if forgotten {
		return rollback(ErrStewardLeaseLost)
	}
	readSet, err := s.revalidateStewardReadSet(ctx, tx, job, lease.JobID, job.attempts)
	if err != nil {
		return rollback(err)
	}
	source, err := readStewardHostSource(ctx, tx, job.receiptID)
	if err != nil {
		return rollback(err)
	}
	appliedAt := s.now().UTC()
	result := stewardv1alpha1.ApplyResult{}
	ops := canonical.BatchOps()
	opResults := make([]stewardv1alpha1.ApplyResultOp, 0, len(ops))
	for _, op := range ops {
		if op.Operation != stewardv1alpha1.OperationIgnore {
			if err := validateStewardEvidence(ctx, tx, job, op.EvidenceRefs, readSet); err != nil {
				return rollback(err)
			}
		}
		applied, err := s.applyStewardOp(ctx, tx, job, lease.JobID, op, source, appliedAt)
		if err != nil {
			return rollback(err)
		}
		opResults = append(opResults, applied)
	}
	if canonical.IsBatch() {
		result.Ops = opResults
	} else {
		result.Operation = canonical.Operation
		if len(opResults) == 1 {
			result.RecordID = opResults[0].RecordID
			result.Revision = opResults[0].Revision
		}
	}
	result.LexiconActivated, err = s.applyStewardLexiconTerms(
		ctx, tx, job, canonical.LexiconTerms, formatTime(s.now().UTC()),
	)
	if err != nil {
		return rollback(err)
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return rollback(fmt.Errorf("encode Steward result: %w", err))
	}
	now := formatTime(s.now().UTC())
	update, err := tx.ExecContext(ctx,
		`UPDATE steward_jobs
		 SET state = 'completed', lease_expires_at = NULL, proposal_digest = ?,
		 result_json = ?, terminal_error_code = '', updated_at = ?
		 WHERE job_id = ? AND state = 'leased' AND lease_token_digest = ?`,
		digest, string(resultJSON), now, lease.JobID, job.leaseDigest)
	if err != nil {
		return rollback(fmt.Errorf("complete Steward job: %w", err))
	}
	affected, err := update.RowsAffected()
	if err != nil || affected != 1 {
		return rollback(ErrStewardLeaseLost)
	}
	semanticGeneration := string(job.profileID) + "@" + strconv.FormatUint(job.profileVersion, 10)
	if _, err := tx.ExecContext(ctx,
		`UPDATE receipt_processing
		 SET state = 'organized', last_attempt_at = ?, terminal_error_code = '', semantic_generation = ?
		 WHERE receipt_id = ?`, now, semanticGeneration, job.receiptID); err != nil {
		return rollback(fmt.Errorf("complete receipt semantic processing: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return stewardv1alpha1.ApplyResult{}, ErrStewardUnknownOutcome
	}
	if s.faults.AfterStewardCommit != nil {
		if err := s.faults.AfterStewardCommit(); err != nil {
			return stewardv1alpha1.ApplyResult{}, ErrStewardUnknownOutcome
		}
	}
	return result, nil
}

func rollbackResult(result stewardv1alpha1.ApplyResult, tx *sql.Tx) (stewardv1alpha1.ApplyResult, error) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return stewardv1alpha1.ApplyResult{}, err
	}
	return result, nil
}

func readStewardJob(ctx context.Context, db databaseExecutor, jobID stewardv1alpha1.JobID) (storedStewardJob, error) {
	var job storedStewardJob
	err := db.QueryRowContext(ctx,
		`SELECT receipt_id, space_id, profile_id, profile_version, state, lease_expires_at,
		 lease_token_digest, proposal_digest, result_json, attempts, label_set, label_set_digest
		 FROM steward_jobs WHERE job_id = ?`, jobID).Scan(
		&job.receiptID, &job.spaceID, &job.profileID, &job.profileVersion, &job.state,
		&job.leaseExpiresAt, &job.leaseDigest, &job.proposalDigest, &job.resultJSON, &job.attempts,
		&job.labelSetEncoded, &job.labelSetDigest)
	return job, err
}

func canonicalStewardProposal(proposal stewardv1alpha1.Proposal) (stewardv1alpha1.Proposal, string, int, error) {
	canonical := proposal
	canonical.EvidenceRefs = append([]v1alpha1.ReceiptID(nil), proposal.EvidenceRefs...)
	sort.Slice(canonical.EvidenceRefs, func(i, j int) bool { return canonical.EvidenceRefs[i] < canonical.EvidenceRefs[j] })
	canonical.LexiconTerms = append([]string(nil), proposal.LexiconTerms...)
	sort.Strings(canonical.LexiconTerms)
	if len(proposal.Ops) > 0 {
		canonical.Ops = make([]stewardv1alpha1.ProposalOp, len(proposal.Ops))
		for index, op := range proposal.Ops {
			op.EvidenceRefs = append([]v1alpha1.ReceiptID(nil), op.EvidenceRefs...)
			sort.Slice(op.EvidenceRefs, func(i, j int) bool { return op.EvidenceRefs[i] < op.EvidenceRefs[j] })
			canonical.Ops[index] = op
		}
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return stewardv1alpha1.Proposal{}, "", 0, err
	}
	return canonical, digestString(string(encoded)), len(encoded), nil
}

func validateStewardEvidence(
	ctx context.Context,
	tx *sql.Tx,
	job storedStewardJob,
	evidenceRefs []v1alpha1.ReceiptID,
	readSet []stewardReadDependency,
) error {
	if !slices.Contains(evidenceRefs, job.receiptID) {
		return fmt.Errorf("%w: proposal must cite its job receipt", ErrStewardProposalInvalid)
	}
	readReceipts := make(map[v1alpha1.ReceiptID]struct{}, len(readSet))
	for _, dependency := range readSet {
		readReceipts[dependency.receiptID] = struct{}{}
	}
	for _, receiptID := range evidenceRefs {
		var spaceID v1alpha1.SpaceID
		var labelSetDigest string
		if err := tx.QueryRowContext(ctx,
			`SELECT space_id, label_set_digest FROM receipts WHERE receipt_id = ?`, receiptID).Scan(
			&spaceID, &labelSetDigest); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: evidence receipt is unavailable", ErrStewardProposalInvalid)
			}
			return fmt.Errorf("read proposal Evidence: %w", err)
		}
		if spaceID != job.spaceID {
			return fmt.Errorf("%w: evidence crosses Space boundary", ErrStewardProposalInvalid)
		}
		if labelSetDigest != job.labelSetDigest {
			return fmt.Errorf("%w: evidence crosses LabelSet boundary", ErrStewardProposalInvalid)
		}
		// A v0.6 Claim always persists the exact evidence the Worker read. A
		// proposal may only cite the job receipt or context evidence it was
		// actually shown, never a model-supplied reference, even when the read
		// context is legitimately empty.
		if receiptID != job.receiptID {
			if _, read := readReceipts[receiptID]; !read {
				return fmt.Errorf("%w: evidence receipt was not part of the read context", ErrStewardProposalInvalid)
			}
		}
		// A corrected original receipt is never authoritative evidence.
		if corrected, err := rowExists(ctx, tx,
			`SELECT EXISTS(SELECT 1 FROM receipt_corrections WHERE original_receipt_id = ?)`, receiptID); err != nil {
			return fmt.Errorf("read proposal Evidence: %w", err)
		} else if corrected {
			return fmt.Errorf("%w: evidence receipt was corrected", ErrStewardProposalInvalid)
		}
	}
	return nil
}

func readSemanticEvidenceIDs(
	ctx context.Context,
	db databaseExecutor,
	recordID stewardv1alpha1.RecordID,
	revision uint64,
) ([]v1alpha1.ReceiptID, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT receipt_id FROM semantic_evidence WHERE record_id = ? AND revision = ? ORDER BY ordinal`, recordID, revision)
	if err != nil {
		return nil, err
	}
	var result []v1alpha1.ReceiptID
	for rows.Next() {
		var receiptID v1alpha1.ReceiptID
		if err := rows.Scan(&receiptID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		result = append(result, receiptID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetSemanticRecord returns one management-plane semantic head and current
// immutable Revision. It is not a data-plane operation.
func (s *Store) GetSemanticRecord(
	ctx context.Context,
	recordID stewardv1alpha1.RecordID,
) (stewardv1alpha1.Record, stewardv1alpha1.Revision, error) {
	var record stewardv1alpha1.Record
	var labelSetEncoded, labelSetDigest, createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT record_id, space_id, label_set, label_set_digest, kind, status,
		 current_revision, invalidated_reason, created_at, updated_at
		 FROM semantic_records WHERE record_id = ?`, recordID).Scan(
		&record.RecordID, &record.SpaceID, &labelSetEncoded, &labelSetDigest,
		&record.Kind, &record.Status, &record.CurrentRevision,
		&record.InvalidatedReason, &createdAt, &updatedAt)
	if err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	labels, err := decodeStoredLabelSet(labelSetEncoded, labelSetDigest)
	if err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	record.Labels = labels.labels
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	if err := s.blankForgottenRecordHead(ctx, s.db, &record); err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	var revision stewardv1alpha1.Revision
	var revisionAt string
	err = s.db.QueryRowContext(ctx,
		`SELECT record_id, revision, space_id, kind, text, operation, COALESCE(job_id, ''), created_at
		 FROM semantic_revisions WHERE record_id = ? AND revision = ?`, recordID, record.CurrentRevision).Scan(
		&revision.RecordID, &revision.Revision, &revision.SpaceID, &revision.Kind, &revision.Text,
		&revision.Operation, &revision.JobID, &revisionAt)
	if err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	revision.CreatedAt, err = parseTime(revisionAt)
	if err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	// A committed forgetting barrier denies derived payload immediately, even if
	// managed content cleansing has not completed yet.
	if err := s.blankForgottenRevision(ctx, s.db, recordID, &revision); err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	evidence, err := readSemanticEvidenceIDs(ctx, s.db, recordID, record.CurrentRevision)
	if err != nil {
		return stewardv1alpha1.Record{}, stewardv1alpha1.Revision{}, err
	}
	for _, receiptID := range evidence {
		revision.Evidence = append(revision.Evidence, stewardv1alpha1.Evidence{ReceiptID: receiptID, SpaceID: record.SpaceID})
	}
	return record, revision, nil
}

func (s *Store) invalidateSemanticRecordsForReceipt(
	ctx context.Context,
	tx *sql.Tx,
	receiptID v1alpha1.ReceiptID,
	reason string,
) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT r.record_id, r.space_id
		 FROM semantic_records r
		 JOIN semantic_evidence e ON e.record_id = r.record_id AND e.revision = r.current_revision
		 WHERE r.status = 'active' AND e.receipt_id = ?
		 ORDER BY r.record_id`, receiptID)
	if err != nil {
		return fmt.Errorf("find semantic Records for receipt: %w", err)
	}
	type affectedRecord struct {
		id      stewardv1alpha1.RecordID
		spaceID v1alpha1.SpaceID
	}
	var records []affectedRecord
	for rows.Next() {
		var record affectedRecord
		if err := rows.Scan(&record.id, &record.spaceID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read semantic Record invalidation: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("find semantic Records for receipt: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close semantic Record invalidation: %w", err)
	}
	now := formatTime(s.now().UTC())
	for _, record := range records {
		tableName, err := readSemanticSpaceIndex(ctx, tx, record.spaceID)
		if err != nil {
			return fmt.Errorf("resolve semantic invalidation index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableName+` WHERE record_id = ?`, record.id); err != nil {
			return fmt.Errorf("remove invalidated semantic projection: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE semantic_records SET status = 'invalidated', invalidated_reason = ?, updated_at = ? WHERE record_id = ?`,
			reason, now, record.id); err != nil {
			return fmt.Errorf("invalidate semantic Record: %w", err)
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
