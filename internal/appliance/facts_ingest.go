package appliance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	steward "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func (s *Store) SetIngestionPolicy(ctx context.Context, p facts.IngestionPolicy) (resultErr error) {
	defer func() { resultErr = s.factServiceError(resultErr) }()
	if err := s.requireMutableGeneration(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return s.serviceError(memory.ErrorCodeInvalidArgument, err.Error(), false)
	}
	labels, err := normalizeLabelSet(p.Scope.Labels)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err = readSpaceScope(ctx, tx, p.Scope.SpaceID); errors.Is(err, sql.ErrNoRows) {
		return s.serviceError(memory.ErrorCodeNotFound, "Space not found", false)
	} else if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ingestion_policies(space_id,label_set_digest,producer,subject,denied,reason) VALUES(?,?,?,?,?,?) ON CONFLICT(space_id,label_set_digest,producer,subject) DO UPDATE SET denied=excluded.denied,reason=excluded.reason`, p.Scope.SpaceID, labels.digest, p.Producer, p.Subject, p.Deny, p.Reason)
	if err != nil {
		return err
	}
	_, err = s.recordMemoryChange(ctx, tx, p.Scope.SpaceID, labels.digest, "ingestion_policy", "", "")
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SubmitEvidence never calls a model. The whole source admission, receipt,
// optional structured lifecycle batch and indexes commit as one effect.
func (s *Store) SubmitEvidence(ctx context.Context, auth memory.CallAuthorization, r facts.SubmitEvidenceRequest) (response facts.SubmitEvidenceResponse, resultErr error) {
	defer func() { resultErr = s.factServiceError(resultErr) }()
	out := facts.SubmitEvidenceResponse{Organization: facts.OrganizationDisabled}
	if err := s.requireMutableGeneration(); err != nil {
		return out, err
	}
	if err := r.Validate(); err != nil {
		return out, s.serviceError(memory.ErrorCodeInvalidArgument, err.Error(), false)
	}
	// Clone before normalization: callers may concurrently retry the same value.
	r.Mutations = append([]facts.Mutation(nil), r.Mutations...)
	// Sort exact conditions so semantically identical host retries normalize.
	for i := range r.Mutations {
		r.Mutations[i].Conditions = append([]facts.Condition(nil), r.Mutations[i].Conditions...)
		sort.Slice(r.Mutations[i].Conditions, func(a, b int) bool { return r.Mutations[i].Conditions[a].Key < r.Mutations[i].Conditions[b].Key })
	}
	sourceJSON, _ := json.Marshal(r.Source)
	identityJSON, _ := json.Marshal([]string{r.Source.Producer, r.Source.EventID, r.Source.Revision, r.Source.Fragment})
	sourceKey := digestString(string(identityJSON))
	canonical := r
	canonical.IdempotencyKey = ""
	encoded, _ := json.Marshal(canonical)
	digest := digestString(string(encoded))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	view, err := s.authorize(ctx, tx, auth, memory.OperationRemember)
	if err != nil {
		return out, err
	}
	if view.writeSpaceID == "" {
		return out, s.serviceError(memory.ErrorCodeUnauthorized, "View has no writable Space", false)
	}
	var denied bool
	var reason string
	err = tx.QueryRowContext(ctx, `SELECT denied,reason FROM ingestion_policies WHERE space_id=? AND label_set_digest=? AND producer=? AND subject=?`, view.writeSpaceID, view.labelSetDigest, r.Source.Producer, r.Source.Subject).Scan(&denied, &reason)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if denied {
		out.RejectionReason = "ingestion_policy: " + reason
		return out, nil
	}
	var priorDigest string
	var suppressed bool
	err = tx.QueryRowContext(ctx, `SELECT receipt_id,request_digest,suppressed FROM evidence_sources WHERE space_id=? AND label_set_digest=? AND source_key=?`, view.writeSpaceID, view.labelSetDigest, sourceKey).Scan(&out.ReceiptID, &priorDigest, &suppressed)
	if err == nil {
		if suppressed {
			out.ReceiptID = ""
			out.RejectionReason = "source_suppressed"
			return out, nil
		}
		if priorDigest != digest {
			return out, s.serviceError(memory.ErrorCodeConflict, "source identity already has different content or assertions", false)
		}
		out.Accepted = true
		out.Deduplicated = true
		if err = tx.QueryRowContext(ctx, `SELECT consistency_token FROM receipts WHERE receipt_id=?`, out.ReceiptID).Scan(&out.ConsistencyToken); err != nil {
			return out, err
		}
		out.Organization, err = sourceOrganization(ctx, tx, out.ReceiptID)
		if err != nil {
			return out, err
		}
		out.Facts, err = s.factsForReceipt(ctx, tx, out.ReceiptID)
		return out, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	occupied, err := rowExists(ctx, tx, `SELECT EXISTS(SELECT 1 FROM receipts WHERE space_id=? AND idempotency_key=? UNION ALL SELECT 1 FROM receipt_tombstones WHERE space_id=? AND idempotency_key=?)`, view.writeSpaceID, r.IdempotencyKey, view.writeSpaceID, r.IdempotencyKey)
	if err != nil {
		return out, err
	}
	if occupied {
		return out, s.serviceError(memory.ErrorCodeConflict, "effect identity already used", false)
	}
	suffix, err := s.randomHex(16)
	if err != nil {
		return out, err
	}
	token, err := s.randomToken(24)
	if err != nil {
		return out, err
	}
	out.ReceiptID = memory.ReceiptID("receipt-" + suffix)
	out.ConsistencyToken = memory.ConsistencyToken(token)
	now := s.now().UTC()
	var occurred any
	if r.OccurredAt != nil {
		occurred = formatTime(*r.OccurredAt)
	}
	legacy := memory.RememberRequest{Text: r.Text, IdempotencyKey: r.IdempotencyKey, OccurredAt: r.OccurredAt, SourceContext: memory.SourceContext{SourceType: "trusted_evidence"}}
	legacyDigest, err := rememberRequestDigest(legacy, view.labels)
	if err != nil {
		return out, err
	}
	audit, _ := json.Marshal(legacy.SourceContext)
	result, err := tx.ExecContext(ctx, `INSERT INTO receipts(receipt_id,space_id,text,source_context,occurred_at,received_at,idempotency_key,request_digest,consistency_token,label_set,label_set_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, out.ReceiptID, view.writeSpaceID, r.Text, string(audit), occurred, formatTime(now), r.IdempotencyKey, legacyDigest, token, view.labelSetEncoded, view.labelSetDigest)
	if err != nil {
		return out, err
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO receipt_processing(receipt_id,state) VALUES(?,'accepted')`, out.ReceiptID); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO consistency_cursors(token,generation,space_id,commit_sequence,label_set_digest) VALUES(?,?,?,?,?)`, token, s.generation, view.writeSpaceID, sequence, view.labelSetDigest); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO evidence_sources(space_id,label_set_digest,source_key,receipt_id,source_json,request_digest,suppressed) VALUES(?,?,?,?,?,?,0)`, view.writeSpaceID, view.labelSetDigest, sourceKey, out.ReceiptID, string(sourceJSON), digest); err != nil {
		return out, err
	}
	table, err := readSpaceIndex(ctx, tx, view.writeSpaceID)
	if err != nil {
		return out, err
	}
	if err = indexReceiptProjection(ctx, tx, table, out.ReceiptID, r.Text, nil); err != nil {
		return out, err
	}
	for _, m := range r.Mutations {
		f, e := s.applyFactMutation(ctx, tx, view, r.Source, out.ReceiptID, m, now)
		if e != nil {
			return facts.SubmitEvidenceResponse{}, e
		}
		out.Facts = append(out.Facts, f)
	}
	if len(r.Mutations) == 0 {
		if err = s.enqueueStewardJob(ctx, tx, out.ReceiptID, view.writeSpaceID, view.labelSetEncoded, view.labelSetDigest, now); err != nil {
			return out, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE receipt_processing SET state='organized',semantic_generation=? WHERE receipt_id=?`, facts.ProtocolVersion, out.ReceiptID); err != nil {
			return out, err
		}
	}
	out.Organization, err = sourceOrganization(ctx, tx, out.ReceiptID)
	if err != nil {
		return out, err
	}
	if _, err = s.recordMemoryChange(ctx, tx, view.writeSpaceID, view.labelSetDigest, "evidence_accepted", out.ReceiptID, ""); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return facts.SubmitEvidenceResponse{}, s.serviceError(memory.ErrorCodeUnknownOutcome, "evidence commit outcome unknown; retry same source and effect", true)
	}
	out.Accepted = true
	return out, nil
}
func sourceOrganization(ctx context.Context, tx *sql.Tx, id memory.ReceiptID) (facts.OrganizationState, error) {
	var state string
	err := tx.QueryRowContext(ctx, `SELECT state FROM steward_jobs WHERE receipt_id=?`, id).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		var p string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM receipt_processing WHERE receipt_id=?`, id).Scan(&p); err != nil {
			return "", err
		}
		if p == "organized" {
			return facts.OrganizationApplied, nil
		}
		return facts.OrganizationDisabled, nil
	}
	if err != nil {
		return "", err
	}
	switch state {
	case "pending":
		return facts.OrganizationPending, nil
	case "leased":
		return facts.OrganizationProcessing, nil
	case "completed":
		return facts.OrganizationApplied, nil
	case "failed":
		return facts.OrganizationFailed, nil
	}
	return "", fmt.Errorf("unknown job state")
}
func (s *Store) factsForReceipt(ctx context.Context, tx *sql.Tx, id memory.ReceiptID) ([]facts.Fact, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.record_id,r.revision FROM semantic_revisions r JOIN semantic_evidence e ON e.record_id=r.record_id AND e.revision=r.revision WHERE e.receipt_id=? AND r.fact_json!='' ORDER BY r.record_id,r.revision`, id)
	if err != nil {
		return nil, err
	}
	type ref struct {
		id string
		n  uint64
	}
	var refs []ref
	for rows.Next() {
		var v ref
		if err = rows.Scan(&v.id, &v.n); err != nil {
			rows.Close()
			return nil, err
		}
		refs = append(refs, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var out []facts.Fact
	for _, v := range refs {
		// An independently retained confirmation source can outlive a deleted
		// supporting receipt. Its retry must respect the Record barrier even
		// before the second transaction clears the revision payload.
		forgotten, err := s.forgottenRecord(ctx, tx, steward.RecordID(v.id))
		if err != nil {
			return nil, err
		}
		if forgotten {
			continue
		}
		f, e := readFactRevision(ctx, tx, v.id, v.n)
		if e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, nil
}
