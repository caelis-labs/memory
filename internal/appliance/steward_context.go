package appliance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// readStewardHostSource returns the trusted host attribution stored for one
// receipt, or nil when the receipt was admitted without a Source. The value is
// copied verbatim from evidence_sources; a model never influences it.
func readStewardHostSource(ctx context.Context, db databaseExecutor, receiptID v1alpha1.ReceiptID) (*facts.Source, error) {
	var encoded string
	err := db.QueryRowContext(ctx,
		`SELECT source_json FROM evidence_sources WHERE receipt_id = ? AND suppressed = 0`,
		receiptID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Steward host source: %w", err)
	}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}
	var source facts.Source
	if err := json.Unmarshal([]byte(encoded), &source); err != nil {
		return nil, fmt.Errorf("decode Steward host source: %w", err)
	}
	return &source, nil
}

// stewardSourceRef projects only the host-authored attribution a Worker may see.
func stewardSourceRef(source *facts.Source) []stewardv1alpha1.SourceRef {
	if source == nil {
		return nil
	}
	return []stewardv1alpha1.SourceRef{{
		Producer: source.Producer, EventID: source.EventID, Revision: source.Revision, Fragment: source.Fragment,
		Subject: source.Subject, FactKey: source.FactKey, Role: stewardv1alpha1.SourceRole(source.Role),
	}}
}

// readStewardContextRecords builds the bounded relevant context for one Job:
// exact structured subject/fact-key matches first, then receipt-text lexical
// FTS matches, then the most recent same-Space heads. Priority order is stable
// and every returned Record is an active head in the exact Space and LabelSet,
// so authorization precedes candidate generation.
func (s *Store) readStewardContextRecords(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	labelSetDigest string,
	source *facts.Source,
	receiptText string,
	limit int,
) ([]stewardv1alpha1.RecordContext, error) {
	records := make([]stewardv1alpha1.RecordContext, 0, limit)
	if limit <= 0 {
		return records, nil
	}
	seen := make(map[stewardv1alpha1.RecordID]struct{}, limit)
	appendRecords := func(candidates []stewardv1alpha1.RecordContext) {
		for _, candidate := range candidates {
			if len(records) >= limit {
				return
			}
			if _, exists := seen[candidate.RecordID]; exists {
				continue
			}
			seen[candidate.RecordID] = struct{}{}
			records = append(records, candidate)
		}
	}
	// A subject alone is too broad to preempt lexical relevance: every fact
	// about one user would otherwise compete solely by recency.
	if source != nil && source.Subject != "" && source.FactKey != "" {
		matched, err := readStewardSubjectContext(ctx, db, spaceID, labelSetDigest, source.Subject, source.FactKey, limit)
		if err != nil {
			return nil, err
		}
		appendRecords(matched)
	}
	if len(records) < limit {
		tableName, err := readSemanticSpaceIndex(ctx, db, spaceID)
		if err != nil {
			return nil, fmt.Errorf("resolve semantic Space index: %w", err)
		}
		privateTerms, err := s.activeLexiconTerms(ctx, db, spaceID)
		if err != nil {
			return nil, fmt.Errorf("read semantic Space lexicon: %w", err)
		}
		ftsQuery, err := lexicalFTSQuery(receiptText, privateTerms)
		if err != nil {
			return nil, fmt.Errorf("build context lexical query: %w", err)
		}
		if ftsQuery != "" {
			matched, err := readStewardLexicalContext(ctx, db, tableName, spaceID, labelSetDigest, ftsQuery, limit)
			if err != nil {
				return nil, err
			}
			appendRecords(matched)
		}
	}
	if len(records) < limit && source != nil && source.Subject != "" && source.FactKey == "" {
		matched, err := readStewardSubjectContext(ctx, db, spaceID, labelSetDigest, source.Subject, "", limit)
		if err != nil {
			return nil, err
		}
		appendRecords(matched)
	}
	if len(records) < limit {
		recent, err := readStewardRecentContext(ctx, db, spaceID, labelSetDigest, limit)
		if err != nil {
			return nil, err
		}
		appendRecords(recent)
	}
	for index := range records {
		evidence, err := readSemanticEvidenceIDs(ctx, db, records[index].RecordID, records[index].Revision)
		if err != nil {
			return nil, fmt.Errorf("read Steward Record evidence: %w", err)
		}
		records[index].EvidenceRefs = evidence
	}
	return records, nil
}

func readStewardSubjectContext(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	labelSetDigest, subject, factKey string,
	limit int,
) ([]stewardv1alpha1.RecordContext, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT r.record_id, r.current_revision, v.kind, v.text, r.subject, r.fact_key
		 FROM semantic_records r
		 JOIN semantic_revisions v ON v.record_id = r.record_id AND v.revision = r.current_revision
		 WHERE r.space_id = ? AND r.label_set_digest = ? AND r.status = 'active'
		   AND r.subject = ? AND (? = '' OR r.fact_key = ?)
		 ORDER BY r.updated_at DESC, r.record_id
		 LIMIT ?`, spaceID, labelSetDigest, subject, factKey, factKey, limit)
	if err != nil {
		return nil, fmt.Errorf("read Steward subject context: %w", err)
	}
	return scanStewardContextRecords(rows)
}

func readStewardLexicalContext(
	ctx context.Context,
	db databaseExecutor,
	tableName string,
	spaceID v1alpha1.SpaceID,
	labelSetDigest, ftsQuery string,
	limit int,
) ([]stewardv1alpha1.RecordContext, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT r.record_id, r.current_revision, v.kind, v.text, r.subject, r.fact_key
		 FROM `+tableName+` f
		 JOIN semantic_records r ON r.record_id = f.record_id AND r.space_id = ?
		 JOIN semantic_revisions v ON v.record_id = r.record_id AND v.revision = r.current_revision
		 WHERE `+tableName+` MATCH ? AND r.label_set_digest = ? AND r.status = 'active'
		   AND r.current_revision = f.revision
		 ORDER BY bm25(`+tableName+`, 0.0, 0.0, 4.0, 2.0, 0.25), r.updated_at DESC, r.record_id
		 LIMIT ?`, spaceID, ftsQuery, labelSetDigest, limit)
	if err != nil {
		return nil, fmt.Errorf("read Steward lexical context: %w", err)
	}
	return scanStewardContextRecords(rows)
}

func readStewardRecentContext(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	labelSetDigest string,
	limit int,
) ([]stewardv1alpha1.RecordContext, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT r.record_id, r.current_revision, v.kind, v.text, r.subject, r.fact_key
		 FROM semantic_records r
		 JOIN semantic_revisions v ON v.record_id = r.record_id AND v.revision = r.current_revision
		 WHERE r.space_id = ? AND r.label_set_digest = ? AND r.status = 'active'
		 ORDER BY r.updated_at DESC, r.record_id
		 LIMIT ?`, spaceID, labelSetDigest, limit)
	if err != nil {
		return nil, fmt.Errorf("read Steward recent context: %w", err)
	}
	return scanStewardContextRecords(rows)
}

func scanStewardContextRecords(rows *sql.Rows) ([]stewardv1alpha1.RecordContext, error) {
	defer rows.Close()
	var records []stewardv1alpha1.RecordContext
	for rows.Next() {
		var record stewardv1alpha1.RecordContext
		if err := rows.Scan(&record.RecordID, &record.Revision, &record.Kind, &record.Text,
			&record.Subject, &record.FactKey); err != nil {
			return nil, fmt.Errorf("scan Steward Record context: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Steward Record context: %w", err)
	}
	return records, nil
}

// persistStewardReadSet records exactly the Record revisions and evidence
// receipts the Worker was shown for one attempt. Apply revalidates these rows,
// never the model's self-reported evidence references.
func persistStewardReadSet(
	ctx context.Context,
	tx *sql.Tx,
	jobID stewardv1alpha1.JobID,
	attempt int,
	records []stewardv1alpha1.RecordContext,
) error {
	ordinal := 0
	for _, record := range records {
		for _, receiptID := range record.EvidenceRefs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO steward_read_set(job_id, attempt, ordinal, record_id, revision, receipt_id)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				jobID, attempt, ordinal, record.RecordID, record.Revision, receiptID); err != nil {
				return fmt.Errorf("record Steward read set: %w", err)
			}
			ordinal++
		}
	}
	return nil
}

type stewardReadDependency struct {
	recordID  stewardv1alpha1.RecordID
	revision  uint64
	receiptID v1alpha1.ReceiptID
}

func readStewardReadSet(
	ctx context.Context,
	db databaseExecutor,
	jobID stewardv1alpha1.JobID,
	attempt int,
) ([]stewardReadDependency, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT record_id, revision, receipt_id FROM steward_read_set
		 WHERE job_id = ? AND attempt = ? ORDER BY ordinal`, jobID, attempt)
	if err != nil {
		return nil, fmt.Errorf("read Steward read set: %w", err)
	}
	defer rows.Close()
	var dependencies []stewardReadDependency
	for rows.Next() {
		var dependency stewardReadDependency
		if err := rows.Scan(&dependency.recordID, &dependency.revision, &dependency.receiptID); err != nil {
			return nil, fmt.Errorf("scan Steward read set: %w", err)
		}
		dependencies = append(dependencies, dependency)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Steward read set: %w", err)
	}
	return dependencies, nil
}

// revalidateStewardReadSet proves every dependency of the claimed attempt is
// still exactly what the Worker read, and returns the persisted set so Apply
// can also refuse model evidence references that were never read. Every Claim
// persists a read set (empty when the context is empty); there is no
// compatibility path that validates a proposal without one. Any drift is a
// conflict so a retry re-claims fresh context.
func (s *Store) revalidateStewardReadSet(
	ctx context.Context,
	db databaseExecutor,
	job storedStewardJob,
	jobID stewardv1alpha1.JobID,
	attempt int,
) ([]stewardReadDependency, error) {
	dependencies, err := readStewardReadSet(ctx, db, jobID, attempt)
	if err != nil {
		return nil, err
	}
	checkedRecords := make(map[stewardv1alpha1.RecordID]uint64)
	checkedReceipts := make(map[v1alpha1.ReceiptID]struct{})
	for _, dependency := range dependencies {
		if recorded, seen := checkedRecords[dependency.recordID]; !seen || recorded != dependency.revision {
			var spaceID v1alpha1.SpaceID
			var labelSetDigest string
			var status stewardv1alpha1.RecordStatus
			var currentRevision uint64
			err := db.QueryRowContext(ctx,
				`SELECT space_id, label_set_digest, status, current_revision
				 FROM semantic_records WHERE record_id = ?`, dependency.recordID).Scan(
				&spaceID, &labelSetDigest, &status, &currentRevision)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: read dependency Record is gone", ErrStewardConflict)
			}
			if err != nil {
				return nil, fmt.Errorf("revalidate Steward read dependency: %w", err)
			}
			if spaceID != job.spaceID || labelSetDigest != job.labelSetDigest ||
				status != stewardv1alpha1.RecordStatusActive || currentRevision != dependency.revision {
				return nil, fmt.Errorf("%w: read dependency Record changed", ErrStewardConflict)
			}
			checkedRecords[dependency.recordID] = dependency.revision
		}
		if _, seen := checkedReceipts[dependency.receiptID]; seen {
			continue
		}
		checkedReceipts[dependency.receiptID] = struct{}{}
		if err := s.validateStewardEvidenceReceipt(ctx, db, job, dependency.receiptID); err != nil {
			return nil, err
		}
	}
	return dependencies, nil
}

// validateStewardEvidenceReceipt proves one receipt is still usable evidence:
// same Space and LabelSet, not corrected, and not under a deletion barrier.
func (s *Store) validateStewardEvidenceReceipt(
	ctx context.Context,
	db databaseExecutor,
	job storedStewardJob,
	receiptID v1alpha1.ReceiptID,
) error {
	var spaceID v1alpha1.SpaceID
	var labelSetDigest string
	err := db.QueryRowContext(ctx,
		`SELECT space_id, label_set_digest FROM receipts WHERE receipt_id = ?`, receiptID).Scan(
		&spaceID, &labelSetDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: read dependency receipt is gone", ErrStewardConflict)
	}
	if err != nil {
		return fmt.Errorf("revalidate Steward read dependency: %w", err)
	}
	if spaceID != job.spaceID || labelSetDigest != job.labelSetDigest {
		return fmt.Errorf("%w: read dependency receipt changed scope", ErrStewardConflict)
	}
	if corrected, err := rowExists(ctx, db, `SELECT EXISTS(SELECT 1 FROM receipt_corrections WHERE original_receipt_id = ?)`, receiptID); err != nil {
		return fmt.Errorf("revalidate Steward read dependency: %w", err)
	} else if corrected {
		return fmt.Errorf("%w: read dependency receipt was corrected", ErrStewardConflict)
	}
	if forgotten, err := s.forgottenReceipt(ctx, db, job.spaceID, receiptID); err != nil {
		return err
	} else if forgotten {
		return fmt.Errorf("%w: read dependency receipt was forgotten", ErrStewardConflict)
	}
	return nil
}
