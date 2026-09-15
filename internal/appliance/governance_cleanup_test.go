package appliance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func governanceCount(t *testing.T, store *Store, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := store.db.QueryRowContext(t.Context(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return count
}

func governanceText(t *testing.T, store *Store, query string, args ...any) string {
	t.Helper()
	var value string
	if err := store.db.QueryRowContext(t.Context(), query, args...).Scan(&value); err != nil {
		t.Fatalf("read %q: %v", query, err)
	}
	return value
}

func recallBudget() v1alpha1.RecallBudget {
	return v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 8192, DeadlineMS: 2000}
}

// seedEvidenceSource writes the evidence_sources row the host ingestion path
// creates for one receipt, so source suppression can be asserted directly.
func seedEvidenceSource(t *testing.T, store *Store, receiptID v1alpha1.ReceiptID, sourceKey string) {
	t.Helper()
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO evidence_sources(space_id, label_set_digest, source_key, receipt_id, source_json, request_digest, suppressed)
		 VALUES ('space-bot-a', ?, ?, ?, '{"producer":"test"}', 'digest', 0)`,
		emptyLabelSetDigest, sourceKey, receiptID); err != nil {
		t.Fatalf("seed evidence source: %v", err)
	}
}

// stewardAdd applies one evidence-bound ADD proposal for a receipt and returns
// the resulting Record.
func stewardAdd(t *testing.T, store *Store, receiptID v1alpha1.ReceiptID, jobID, text string) stewardv1alpha1.ApplyResult {
	t.Helper()
	lease := leaseStewardReceipt(t, store, receiptID, stewardv1alpha1.JobID(jobID))
	result, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: text,
		EvidenceRefs: []v1alpha1.ReceiptID{receiptID},
	})
	if err != nil {
		t.Fatalf("apply Steward proposal: %v", err)
	}
	return result
}

func TestDeleteReceiptForgetsTransitivelyDerivedHistory(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "governance-delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	derived := stewardAdd(t, store, receipt.ReceiptID, "job-governance-delete", "Derived paraphrase of the deleted receipt.")
	seedEvidenceSource(t, store, receipt.ReceiptID, "source-governance-delete")
	_, revision, err := store.GetSemanticRecord(t.Context(), derived.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if revision.Text == "" {
		t.Fatal("baseline derived Revision has no text")
	}

	response, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: receipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-governance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Deleted || response.TombstoneID == "" || response.InvalidationVersion == 0 {
		t.Fatalf("DeleteReceipt response = %+v", response)
	}
	if response.Cleanup != managementv1alpha1.CleanupStateCompleted {
		t.Fatalf("DeleteReceipt cleanup = %q, want completed", response.Cleanup)
	}
	if !strings.Contains(response.SessionCopyBoundary, "external producers") {
		t.Fatalf("session copy boundary is not source-neutral: %q", response.SessionCopyBoundary)
	}

	// Derived contents are cleared; the content-free audit skeleton survives.
	if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); text != "" {
		t.Fatalf("cleansed Revision text = %q", text)
	}
	if factJSON := governanceText(t, store, `SELECT fact_json FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); factJSON != "" {
		t.Fatalf("cleansed Revision fact_json = %q", factJSON)
	}
	if evidence := governanceCount(t, store,
		`SELECT COUNT(*) FROM semantic_evidence WHERE record_id = ? AND receipt_id = ?`,
		derived.RecordID, receipt.ReceiptID); evidence != 1 {
		t.Fatalf("Evidence attribution count = %d, want 1", evidence)
	}
	if status := governanceText(t, store, `SELECT status FROM semantic_records WHERE record_id = ?`, derived.RecordID); status != "invalidated" {
		t.Fatalf("Record status = %q, want invalidated", status)
	}
	if indexed := governanceCount(t, store,
		`SELECT COUNT(*) FROM `+semanticSpaceIndexTable("space-bot-a")+` WHERE record_id = ?`, derived.RecordID); indexed != 0 {
		t.Fatalf("semantic projection rows = %d, want 0", indexed)
	}

	// Every read path denies the forgotten payload.
	_, fenced, err := store.GetSemanticRecord(t.Context(), derived.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if fenced.Text != "" {
		t.Fatalf("GetSemanticRecord text = %q after forget", fenced.Text)
	}
	trace, err := store.TraceRecord(t.Context(), managementv1alpha1.TraceRecordRequest{RecordID: derived.RecordID})
	if err != nil {
		t.Fatal(err)
	}
	if trace.State != managementv1alpha1.RecordStateForgotten {
		t.Fatalf("TraceRecord state = %q, want forgotten", trace.State)
	}
	if trace.Record == nil || trace.Record.Status != stewardv1alpha1.RecordStatusInvalidated {
		t.Fatalf("TraceRecord Record = %+v", trace.Record)
	}
	if len(trace.Revisions) != 1 || trace.Revisions[0].Text != "" {
		t.Fatalf("TraceRecord Revisions = %+v", trace.Revisions)
	}
	if len(trace.Revisions[0].Evidence) != 1 || trace.Revisions[0].Evidence[0].ReceiptID != receipt.ReceiptID {
		t.Fatalf("TraceRecord Evidence = %+v", trace.Revisions[0].Evidence)
	}
	if trace.Cleanup != managementv1alpha1.CleanupStateCompleted {
		t.Fatalf("TraceRecord cleanup = %q", trace.Cleanup)
	}
	recalled, err := store.Recall(t.Context(), auth, v1alpha1.RecallRequest{
		Query: "derived paraphrase service Go", Budget: recallBudget(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range recalled.Fragments {
		if strings.Contains(fragment.Text, "Derived paraphrase") {
			t.Fatalf("Recall returned forgotten derived content: %q", fragment.Text)
		}
	}

	// Barrier, source suppression, and the owner change ledger.
	if cleaned := governanceCount(t, store,
		`SELECT COUNT(*) FROM forgetting_barriers WHERE receipt_id = ? AND kind = 'receipt_deleted' AND status = 'cleaned'`,
		receipt.ReceiptID); cleaned != 1 {
		t.Fatalf("cleaned deletion barriers = %d, want 1", cleaned)
	}
	if suppressed := governanceCount(t, store,
		`SELECT COUNT(*) FROM evidence_sources WHERE receipt_id = ? AND suppressed = 1 AND source_json = ''`,
		receipt.ReceiptID); suppressed != 1 {
		t.Fatalf("suppressed evidence sources = %d, want 1", suppressed)
	}
	if changes := governanceCount(t, store,
		`SELECT COUNT(*) FROM memory_changes WHERE kind = 'receipt_deleted' AND receipt_id = ?`,
		receipt.ReceiptID); changes != 1 {
		t.Fatalf("receipt change rows = %d, want 1", changes)
	}
	if changes := governanceCount(t, store,
		`SELECT COUNT(*) FROM memory_changes WHERE kind = 'record_forgotten' AND receipt_id = ? AND record_id = ?`,
		receipt.ReceiptID, derived.RecordID); changes != 1 {
		t.Fatalf("record change rows = %d, want 1", changes)
	}

	cleanup, err := store.CleanupStatus(t.Context(), managementv1alpha1.CleanupStatusRequest{ReceiptID: receipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if cleanup.State != managementv1alpha1.CleanupStateCompleted || cleanup.InvalidationVersion != response.InvalidationVersion {
		t.Fatalf("CleanupStatus = %+v", cleanup)
	}

	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Governance.PendingCleanups != 0 || inspection.Governance.CompletedCleanups != 1 {
		t.Fatalf("governance diagnostics = %+v", inspection.Governance)
	}
	if inspection.Governance.ClearedRevisions != 1 {
		t.Fatalf("cleared revisions = %d, want 1", inspection.Governance.ClearedRevisions)
	}

	// The deletion is replayable without re-forgetting or re-cleansing.
	replay, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: receipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-governance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.DeduplicatedRetry || replay.InvalidationVersion != response.InvalidationVersion {
		t.Fatalf("replayed deletion = %+v", replay)
	}
	if replay.Cleanup != managementv1alpha1.CleanupStateCompleted {
		t.Fatalf("replayed cleanup = %q, want refreshed completed", replay.Cleanup)
	}
	if barriers := governanceCount(t, store, `SELECT COUNT(*) FROM forgetting_barriers`); barriers != 1 {
		t.Fatalf("forgetting barriers = %d, want 1", barriers)
	}
}

func TestForgettingClosureFollowsStewardReadSetAcrossRecords(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	source, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "closure-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	upstream := stewardAdd(t, store, source.ReceiptID, "job-closure-upstream", "Upstream derived claim.")

	second, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service also uses SQLite", IdempotencyKey: "closure-second",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Model one Job that read the upstream Record as context and produced a
	// downstream Record whose own Evidence never cites the forgotten receipt.
	lease := leaseStewardReceipt(t, store, second.ReceiptID, "job-closure-downstream")
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO steward_read_set(job_id, attempt, ordinal, record_id, revision, receipt_id)
		 VALUES (?, 1, 0, ?, 1, ?)`,
		"job-closure-downstream", upstream.RecordID, source.ReceiptID); err != nil {
		t.Fatal(err)
	}
	downstream, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "Downstream claim derived from upstream context.",
		EvidenceRefs: []v1alpha1.ReceiptID{second.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if governanceCount(t, store,
		`SELECT COUNT(*) FROM semantic_evidence WHERE record_id = ? AND receipt_id = ?`,
		downstream.RecordID, source.ReceiptID) != 0 {
		t.Fatal("downstream Record unexpectedly cites the forgotten receipt")
	}

	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: source.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-closure",
	}); err != nil {
		t.Fatal(err)
	}
	for _, recordID := range []stewardv1alpha1.RecordID{upstream.RecordID, downstream.RecordID} {
		if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, recordID); text != "" {
			t.Fatalf("Record %s revision text = %q, want cleared", recordID, text)
		}
		trace, err := store.TraceRecord(t.Context(), managementv1alpha1.TraceRecordRequest{RecordID: recordID})
		if err != nil {
			t.Fatal(err)
		}
		if trace.State != managementv1alpha1.RecordStateForgotten {
			t.Fatalf("Record %s state = %q, want forgotten", recordID, trace.State)
		}
	}
}

func TestForgettingClosureIsTransitiveAcrossThreeRecords(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	remember := func(key, text string) v1alpha1.ReceiptID {
		t.Helper()
		receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: text, IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		return receipt.ReceiptID
	}
	source := remember("chain-source", "the service uses Go")
	first := stewardAdd(t, store, source, "job-chain-first", "First derived claim.")

	// Each later Job read only the previous Record, so the chain is transitive
	// purely through the recorded read sets.
	secondReceipt := remember("chain-second", "the service also uses SQLite")
	secondLease := leaseStewardReceipt(t, store, secondReceipt, "job-chain-second")
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO steward_read_set(job_id, attempt, ordinal, record_id, revision, receipt_id) VALUES (?, 1, 0, ?, 1, ?)`,
		"job-chain-second", first.RecordID, source); err != nil {
		t.Fatal(err)
	}
	second, err := store.ApplyStewardProposal(t.Context(), secondLease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "Second derived claim.",
		EvidenceRefs: []v1alpha1.ReceiptID{secondReceipt},
	})
	if err != nil {
		t.Fatal(err)
	}

	thirdReceipt := remember("chain-third", "the service deploys on Fridays")
	lease := leaseStewardReceipt(t, store, thirdReceipt, "job-chain-third")
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO steward_read_set(job_id, attempt, ordinal, record_id, revision, receipt_id) VALUES (?, 1, 0, ?, 1, ?)`,
		"job-chain-third", second.RecordID, secondReceipt); err != nil {
		t.Fatal(err)
	}
	third, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "Third derived claim.",
		EvidenceRefs: []v1alpha1.ReceiptID{thirdReceipt},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []stewardv1alpha1.RecordID{second.RecordID, third.RecordID} {
		if count := governanceCount(t, store,
			`SELECT COUNT(*) FROM semantic_evidence WHERE record_id = ? AND receipt_id = ?`, record, source); count != 0 {
			t.Fatalf("Record %s unexpectedly cites the forgotten receipt directly", record)
		}
	}

	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: source, Reason: "approved erasure", IdempotencyKey: "delete-chain",
	}); err != nil {
		t.Fatal(err)
	}
	for _, record := range []stewardv1alpha1.RecordID{first.RecordID, second.RecordID, third.RecordID} {
		if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, record); text != "" {
			t.Fatalf("Record %s revision text = %q, want cleared through the read-set chain", record, text)
		}
		trace, err := store.TraceRecord(t.Context(), managementv1alpha1.TraceRecordRequest{RecordID: record})
		if err != nil {
			t.Fatal(err)
		}
		if trace.State != managementv1alpha1.RecordStateForgotten {
			t.Fatalf("Record %s state = %q, want forgotten", record, trace.State)
		}
	}
}

func TestStaleReadDependencyCannotProduceNewDerivedState(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	contextReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "stale-context",
	})
	if err != nil {
		t.Fatal(err)
	}
	contextRecord := stewardAdd(t, store, contextReceipt.ReceiptID, "job-stale-context", "Context claim.")

	workerReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service also uses SQLite", IdempotencyKey: "stale-worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseStewardReceipt(t, store, workerReceipt.ReceiptID, "job-stale-worker")
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO steward_read_set(job_id, attempt, ordinal, record_id, revision, receipt_id) VALUES (?, 1, 0, ?, 1, ?)`,
		"job-stale-worker", contextRecord.RecordID, contextReceipt.ReceiptID); err != nil {
		t.Fatal(err)
	}

	// The context Record is forgotten after the lease was issued, so the model
	// response was derived from context that no longer exists.
	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: contextReceipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-stale-context",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "Stale derived claim.",
		EvidenceRefs: []v1alpha1.ReceiptID{workerReceipt.ReceiptID},
	}); err == nil {
		t.Fatal("Apply accepted a stale read dependency")
	}
	var records int64
	for _, record := range []stewardv1alpha1.RecordID{contextRecord.RecordID} {
		records += governanceCount(t, store,
			`SELECT COUNT(*) FROM semantic_revisions WHERE record_id = ? AND text != ''`, record)
	}
	if records != 0 {
		t.Fatalf("stale context retained %d derived revisions", records)
	}
	if stale := governanceCount(t, store,
		`SELECT COUNT(*) FROM semantic_revisions WHERE text = 'Stale derived claim.'`); stale != 0 {
		t.Fatal("stale model output was persisted")
	}
}

func TestForgottenDerivedStringsNeverRemainReadable(t *testing.T) {
	const secret = "zzforgottenderivedsecret"
	dataDir := t.TempDir()
	store, auth := newGoldenStoreWithOptions(t, Options{
		DataDir: dataDir, Clock: time.Now,
		Faults: Faults{AfterForgettingBarrier: func() error { return errors.New("crash before cleansing") }},
	})
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "secret-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseStewardReceipt(t, store, receipt.ReceiptID, "job-secret-kind")
	derived, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim-" + secret, Text: "Derived claim carrying " + secret + ".",
		EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Host fact-bearing head strings are free text too.
	if _, err := store.db.ExecContext(t.Context(),
		`UPDATE semantic_records SET subject = ?, fact_key = ? WHERE record_id = ?`,
		"subject-"+secret, "key-"+secret, derived.RecordID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: receipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-secret",
	}); err == nil {
		t.Fatal("modelled crash did not interrupt cleansing")
	}

	// While the barrier is pending, every read path must blank text and kind.
	assertNoSecretInReads(t, store, secret, derived.RecordID)

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), Options{DataDir: dataDir, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	assertNoSecretInReads(t, reopened, secret, derived.RecordID)

	// Managed cleansing removes the stored strings entirely while identity,
	// Space, LabelSet, timestamps, operation, and Evidence survive.
	for _, query := range []string{
		`SELECT COUNT(*) FROM semantic_revisions WHERE kind LIKE '%' || ? || '%' OR text LIKE '%' || ? || '%' OR fact_json LIKE '%' || ? || '%'`,
		`SELECT COUNT(*) FROM semantic_records WHERE kind LIKE '%' || ? || '%' OR subject LIKE '%' || ? || '%' OR fact_key LIKE '%' || ? || '%'`,
	} {
		if count := governanceCount(t, reopened, query, secret, secret, secret); count != 0 {
			t.Fatalf("forgotten derived strings remain in storage: %d rows for %q", count, query)
		}
	}
	if kind := governanceText(t, reopened, `SELECT kind FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); kind != "" {
		t.Fatalf("Revision kind = %q, want cleared", kind)
	}
	if kind := governanceText(t, reopened, `SELECT kind FROM semantic_records WHERE record_id = ?`, derived.RecordID); kind != "" {
		t.Fatalf("Record kind = %q, want cleared", kind)
	}
	if evidence := governanceCount(t, reopened,
		`SELECT COUNT(*) FROM semantic_evidence WHERE record_id = ? AND receipt_id = ?`, derived.RecordID, receipt.ReceiptID); evidence != 1 {
		t.Fatalf("Evidence attribution = %d, want 1", evidence)
	}
	if revision := governanceCount(t, reopened,
		`SELECT COUNT(*) FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); revision != 1 {
		t.Fatalf("Revision skeleton rows = %d, want 1", revision)
	}
	if operation := governanceText(t, reopened, `SELECT operation FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); operation == "" {
		t.Fatal("Revision operation was cleared")
	}
}

// assertNoSecretInReads proves text and kind are denied through every owner and
// data-plane read while a deletion barrier is committed.
func assertNoSecretInReads(t *testing.T, store *Store, secret string, recordID stewardv1alpha1.RecordID) {
	t.Helper()
	record, revision, err := store.GetSemanticRecord(t.Context(), recordID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Kind != "" || revision.Kind != "" || revision.Text != "" {
		t.Fatalf("GetSemanticRecord exposed kind/text = %q/%q/%q", record.Kind, revision.Kind, revision.Text)
	}
	trace, err := store.TraceRecord(t.Context(), managementv1alpha1.TraceRecordRequest{RecordID: recordID})
	if err != nil {
		t.Fatal(err)
	}
	if trace.Record == nil || trace.Record.Kind != "" {
		t.Fatalf("TraceRecord head kind = %+v", trace.Record)
	}
	for _, exposed := range trace.Revisions {
		if exposed.Kind != "" || exposed.Text != "" {
			t.Fatalf("TraceRecord exposed kind/text = %q/%q", exposed.Kind, exposed.Text)
		}
	}
	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("TraceRecord exposed forgotten derived content: %s", encoded)
	}
	listing, err := store.ListRecords(t.Context(), managementv1alpha1.ListRecordsRequest{SpaceID: "space-bot-a", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range listing.Records {
		if summary.RecordID == recordID && summary.Kind != "" {
			t.Fatalf("ListRecords exposed kind = %q", summary.Kind)
		}
	}
	encoded, err = json.Marshal(listing)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("ListRecords exposed forgotten derived content: %s", encoded)
	}
}

// insertFixtureRecord writes one derived Record fixture with a single Revision,
// direct Evidence, and optional structured fact relation. It models data the
// Steward or facts surface can produce without driving a full Apply.
func insertFixtureRecord(
	t *testing.T,
	store *Store,
	recordID stewardv1alpha1.RecordID,
	evidence v1alpha1.ReceiptID,
	related stewardv1alpha1.RecordID,
	kind, text string,
	labels v1alpha1.LabelSet,
) {
	t.Helper()
	stored, err := normalizeLabelSet(labels)
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(store.now().UTC())
	factJSON := ""
	if related != "" {
		factJSON = `{"subject":"service","adoption":"confirmed","transition":"exception","related_record_id":"` + string(related) + `"}`
	}
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(t.Context(),
		`INSERT INTO semantic_records(record_id, space_id, kind, status, current_revision, created_at, updated_at, label_set, label_set_digest)
		 VALUES (?, 'space-bot-a', ?, 'active', 1, ?, ?, ?, ?)`,
		recordID, kind, now, now, stored.encoded, stored.digest); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(),
		`INSERT INTO semantic_revisions(record_id, revision, space_id, kind, text, operation, job_id, created_at, fact_json)
		 VALUES (?, 1, 'space-bot-a', ?, ?, 'ADD', NULL, ?, ?)`,
		recordID, kind, text, now, factJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(),
		`INSERT INTO semantic_evidence(record_id, revision, ordinal, receipt_id, space_id)
		 VALUES (?, 1, 0, ?, 'space-bot-a')`, recordID, evidence); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestForgettingCleansesEveryBatchOutputOfATaintedJob proves that one tainted
// bounded-batch Job contributes every Record it produced, not only its first
// Revision, and that the batch proposals never had to name the forgotten root.
func TestForgettingCleansesEveryBatchOutputOfATaintedJob(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	remember := func(key, text string) v1alpha1.ReceiptID {
		t.Helper()
		receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: text, IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		return receipt.ReceiptID
	}
	root := remember("batch-root", "the service uses Go")
	upstream := stewardAdd(t, store, root, "job-batch-upstream", "Upstream derived claim.")
	aux := remember("batch-aux", "unrelated auxiliary evidence")
	batchReceipt := remember("batch-receipt", "bounded batch evidence")

	lease := leaseStewardReceipt(t, store, batchReceipt, "job-batch")
	// The read set names the upstream Record through an unrelated receipt, so the
	// batch Job and its proposals never reference the forgotten root directly.
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO steward_read_set(job_id, attempt, ordinal, record_id, revision, receipt_id) VALUES ('job-batch', 1, 0, ?, 1, ?)`,
		upstream.RecordID, aux); err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Policy: stewardv1alpha1.PolicyBoundedBatch,
		Ops: []stewardv1alpha1.ProposalOp{
			{Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "Batch output one.", EvidenceRefs: []v1alpha1.ReceiptID{batchReceipt}},
			{Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "Batch output two.", EvidenceRefs: []v1alpha1.ReceiptID{batchReceipt}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ops) != 2 || result.Ops[0].RecordID == "" || result.Ops[0].RecordID == result.Ops[1].RecordID {
		t.Fatalf("batch Apply result = %+v", result)
	}
	outputs := []stewardv1alpha1.RecordID{result.Ops[0].RecordID, result.Ops[1].RecordID}
	for _, output := range outputs {
		if count := governanceCount(t, store,
			`SELECT COUNT(*) FROM semantic_evidence WHERE record_id = ? AND receipt_id = ?`, output, root); count != 0 {
			t.Fatalf("batch output %s cites the forgotten root directly", output)
		}
	}

	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: root, Reason: "approved erasure", IdempotencyKey: "delete-batch",
	}); err != nil {
		t.Fatal(err)
	}
	for _, recordID := range append([]stewardv1alpha1.RecordID{upstream.RecordID}, outputs...) {
		trace, err := store.TraceRecord(t.Context(), managementv1alpha1.TraceRecordRequest{RecordID: recordID})
		if err != nil {
			t.Fatal(err)
		}
		if trace.State != managementv1alpha1.RecordStateForgotten {
			t.Fatalf("Record %s state = %q, want forgotten", recordID, trace.State)
		}
		for _, revision := range trace.Revisions {
			if revision.Text != "" || revision.Kind != "" {
				t.Fatalf("Record %s exposed derived content %q/%q", recordID, revision.Text, revision.Kind)
			}
		}
	}
	if remaining := governanceCount(t, store,
		`SELECT COUNT(*) FROM semantic_revisions WHERE text != '' AND record_id IN (?, ?, ?)`,
		upstream.RecordID, outputs[0], outputs[1]); remaining != 0 {
		t.Fatalf("%d batch revisions kept derived text", remaining)
	}
}

// TestForgettingClosureResolvesChainsLongerThanAnyRoundCap builds a 70-hop
// read-set chain and proves the closure reaches the end: the first Record is the
// only one that cites the forgotten receipt, and every hop is reachable only
// through the preceding Record.
func TestForgettingClosureResolvesChainsLongerThanAnyRoundCap(t *testing.T) {
	const hops = 70
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "chain-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "shared hop evidence", IdempotencyKey: "chain-shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(store.now().UTC())
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rollback := func(err error) {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(),
		`INSERT INTO steward_profiles(profile_id, version, system_prompt, max_context_records, max_input_bytes, max_output_bytes, created_at)
		 VALUES ('profile-chain', 1, 'chain', 8, 65536, 16384, ?)`, now); err != nil {
		rollback(err)
	}
	previous := stewardv1alpha1.RecordID("")
	for hop := 1; hop <= hops; hop++ {
		recordID := stewardv1alpha1.RecordID(fmt.Sprintf("record-chain-%03d", hop))
		jobID := stewardv1alpha1.JobID(fmt.Sprintf("job-chain-%03d", hop))
		evidence := shared.ReceiptID
		if hop == 1 {
			evidence = root.ReceiptID
		}
		if _, err := tx.ExecContext(t.Context(),
			`INSERT INTO steward_jobs(job_id, receipt_id, space_id, profile_id, profile_version, state, available_at, created_at, updated_at, label_set, label_set_digest)
			 VALUES (?, ?, 'space-bot-a', 'profile-chain', 1, 'completed', ?, ?, ?, '[]', ?)`,
			jobID, "receipt-chain-job-"+string(jobID), now, now, now, emptyLabelSetDigest); err != nil {
			rollback(err)
		}
		if _, err := tx.ExecContext(t.Context(),
			`INSERT INTO semantic_records(record_id, space_id, kind, status, current_revision, created_at, updated_at, label_set, label_set_digest)
			 VALUES (?, 'space-bot-a', 'chain', 'active', 1, ?, ?, '[]', ?)`,
			recordID, now, now, emptyLabelSetDigest); err != nil {
			rollback(err)
		}
		if _, err := tx.ExecContext(t.Context(),
			`INSERT INTO semantic_revisions(record_id, revision, space_id, kind, text, operation, job_id, created_at, fact_json)
			 VALUES (?, 1, 'space-bot-a', 'chain', ?, 'ADD', ?, ?, '')`,
			recordID, fmt.Sprintf("chain derived text %03d", hop), jobID, now); err != nil {
			rollback(err)
		}
		if _, err := tx.ExecContext(t.Context(),
			`INSERT INTO semantic_evidence(record_id, revision, ordinal, receipt_id, space_id) VALUES (?, 1, 0, ?, 'space-bot-a')`,
			recordID, evidence); err != nil {
			rollback(err)
		}
		if hop > 1 {
			if _, err := tx.ExecContext(t.Context(),
				`INSERT INTO steward_read_set(job_id, attempt, ordinal, record_id, revision, receipt_id) VALUES (?, 1, 0, ?, 1, ?)`,
				jobID, previous, shared.ReceiptID); err != nil {
				rollback(err)
			}
		}
		previous = recordID
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: root.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-chain-long",
	}); err != nil {
		t.Fatal(err)
	}
	for hop := 1; hop <= hops; hop++ {
		recordID := stewardv1alpha1.RecordID(fmt.Sprintf("record-chain-%03d", hop))
		if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, recordID); text != "" {
			t.Fatalf("hop %d retained derived text %q; closure stopped short", hop, text)
		}
	}
	if remaining := governanceCount(t, store,
		`SELECT COUNT(*) FROM semantic_revisions WHERE record_id LIKE 'record-chain-%' AND text != ''`); remaining != 0 {
		t.Fatalf("%d chain Revisions kept derived text", remaining)
	}
	if attributed := governanceCount(t, store,
		`SELECT COUNT(*) FROM forgetting_barrier_records WHERE record_id LIKE 'record-chain-%'`); attributed != hops {
		t.Fatalf("barrier attribution rows = %d, want %d", attributed, hops)
	}
}

// TestForgettingClosureFollowsFactMetadataRelation proves the structured fact
// relation is an explicit derivation edge with no read set, and that it stays
// inside the forgotten receipt's Space and LabelSet partition.
func TestForgettingClosureFollowsFactMetadataRelation(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	baseReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "metadata-base",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := stewardAdd(t, store, baseReceipt.ReceiptID, "job-metadata-base", "Base derived claim.")
	aux, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "unrelated auxiliary evidence", IdempotencyKey: "metadata-aux",
	})
	if err != nil {
		t.Fatal(err)
	}
	samePartition := stewardv1alpha1.RecordID("record-exception-same")
	insertFixtureRecord(t, store, samePartition, aux.ReceiptID, base.RecordID, "exception",
		"Exception claim for the same partition.", nil)
	otherPartition := stewardv1alpha1.RecordID("record-exception-other")
	insertFixtureRecord(t, store, otherPartition, aux.ReceiptID, base.RecordID, "exception",
		"Exception claim for another partition.", v1alpha1.LabelSet{"tenant-other"})
	// Neither dependent Record has any read-set or Evidence link to the base
	// receipt, so only the fact relation can attribute them.
	for _, recordID := range []stewardv1alpha1.RecordID{samePartition, otherPartition} {
		if count := governanceCount(t, store,
			`SELECT COUNT(*) FROM semantic_evidence WHERE record_id = ? AND receipt_id = ?`, recordID, baseReceipt.ReceiptID); count != 0 {
			t.Fatalf("fixture %s cites the base receipt directly", recordID)
		}
	}

	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: baseReceipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-metadata",
	}); err != nil {
		t.Fatal(err)
	}
	if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, samePartition); text != "" {
		t.Fatalf("metadata-related Record retained %q", text)
	}
	if factJSON := governanceText(t, store, `SELECT fact_json FROM semantic_revisions WHERE record_id = ?`, samePartition); factJSON != "" {
		t.Fatalf("metadata-related Revision retained fact_json %q", factJSON)
	}
	if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, otherPartition); text == "" {
		t.Fatal("forgetting crossed the LabelSet partition into another tenant's Record")
	}
}

func TestForgettingClosureWidensToLegacyPartitionRecords(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	source, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "legacy-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	attributed := stewardAdd(t, store, source.ReceiptID, "job-legacy-source", "Attributed derived claim.")

	other, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "unrelated legacy material", IdempotencyKey: "legacy-other",
	})
	if err != nil {
		t.Fatal(err)
	}
	legacy := stewardAdd(t, store, other.ReceiptID, "job-legacy-other", "Legacy claim with no recorded read set.")
	// Model a pre-M02 Revision: its derivation can never be reconstructed.
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO governance_legacy_revisions(record_id, revision) VALUES (?, 1)`, legacy.RecordID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: source.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-legacy",
	}); err != nil {
		t.Fatal(err)
	}
	for _, recordID := range []stewardv1alpha1.RecordID{attributed.RecordID, legacy.RecordID} {
		if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, recordID); text != "" {
			t.Fatalf("Record %s revision text = %q, want cleared by legacy widening", recordID, text)
		}
	}
}

func TestForgettingCleanupResumesAfterRestart(t *testing.T) {
	dataDir := t.TempDir()
	fault := func() error { return errors.New("simulated crash between barrier and cleansing") }
	store, auth := newGoldenStoreWithOptions(t, Options{
		DataDir: dataDir, Clock: time.Now, Faults: Faults{AfterForgettingBarrier: fault},
	})
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "restart-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	derived := stewardAdd(t, store, receipt.ReceiptID, "job-restart", "Derived paraphrase before the crash.")

	_, err = store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: receipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-restart",
	})
	if !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnknownOutcome) {
		t.Fatalf("delete error = %v, want unknown outcome", err)
	}
	if status := governanceText(t, store,
		`SELECT status FROM forgetting_barriers WHERE receipt_id = ?`, receipt.ReceiptID); status != "pending" {
		t.Fatalf("barrier status = %q, want pending", status)
	}
	// Content is still physically present but already denied.
	if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); text == "" {
		t.Fatal("cleansing ran before the modelled crash")
	}
	_, fenced, err := store.GetSemanticRecord(t.Context(), derived.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if fenced.Text != "" {
		t.Fatalf("pending barrier leaked derived text %q", fenced.Text)
	}
	pending, err := store.CleanupStatus(t.Context(), managementv1alpha1.CleanupStatusRequest{ReceiptID: receipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != managementv1alpha1.CleanupStatePending || pending.PendingBarriers != 1 {
		t.Fatalf("CleanupStatus = %+v", pending)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), Options{DataDir: dataDir, Clock: time.Now})
	if err != nil {
		t.Fatalf("reopen after pending barrier: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if text := governanceText(t, reopened, `SELECT text FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); text != "" {
		t.Fatalf("recovered revision text = %q, want cleared", text)
	}
	if status := governanceText(t, reopened,
		`SELECT status FROM forgetting_barriers WHERE receipt_id = ?`, receipt.ReceiptID); status != "cleaned" {
		t.Fatalf("recovered barrier status = %q, want cleaned", status)
	}
	inspection, err := reopened.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Governance.PendingCleanups != 0 {
		t.Fatalf("pending cleanups after recovery = %d", inspection.Governance.PendingCleanups)
	}
	// The forgotten receipt can never be reprocessed after recovery.
	forgotten, err := reopened.forgottenReceipt(t.Context(), reopened.db, "space-bot-a", receipt.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	if !forgotten {
		t.Fatal("forgotten receipt is not fenced after recovery")
	}
}

func TestCorrectionBarrierInvalidatesWithoutClearingHistory(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Java", IdempotencyKey: "correction-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	derived := stewardAdd(t, store, receipt.ReceiptID, "job-correction", "The service uses Java.")
	seedEvidenceSource(t, store, receipt.ReceiptID, "source-correction")
	derivedText := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, derived.RecordID)

	response, err := store.CorrectReceipt(t.Context(), managementv1alpha1.CorrectReceiptRequest{
		ReceiptID: receipt.ReceiptID, ReplacementText: "the service uses Go",
		Reason: "producer corrected the source", IdempotencyKey: "correct-governance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.ReplacementReceiptID == "" || response.ConsistencyToken == "" {
		t.Fatalf("CorrectReceipt response = %+v", response)
	}
	// Correction preserves the wrong-source audit and only invalidates use.
	if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); text != derivedText {
		t.Fatalf("correction cleared revision text: %q", text)
	}
	if status := governanceText(t, store, `SELECT status FROM semantic_records WHERE record_id = ?`, derived.RecordID); status != "invalidated" {
		t.Fatalf("corrected Record status = %q, want invalidated", status)
	}
	if barriers := governanceCount(t, store,
		`SELECT COUNT(*) FROM forgetting_barriers WHERE receipt_id = ? AND kind = 'receipt_deleted'`, receipt.ReceiptID); barriers != 0 {
		t.Fatalf("correction created %d deletion barriers, want 0", barriers)
	}
	if barriers := governanceCount(t, store,
		`SELECT COUNT(*) FROM forgetting_barriers WHERE receipt_id = ? AND kind = 'receipt_corrected' AND status = 'cleaned'`,
		receipt.ReceiptID); barriers != 1 {
		t.Fatalf("cleaned correction barriers = %d, want 1", barriers)
	}
	if suppressed := governanceCount(t, store,
		`SELECT COUNT(*) FROM evidence_sources WHERE receipt_id = ? AND suppressed = 1 AND source_json != ''`,
		receipt.ReceiptID); suppressed != 1 {
		t.Fatalf("audited suppressed sources after correction = %d, want 1 with payload retained", suppressed)
	}
	// A suppressed source is never offered to a model as EvidenceRefs.
	source, err := readStewardHostSource(t.Context(), store.db, receipt.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	if source != nil {
		t.Fatalf("corrected source was still offered to a model: %+v", source)
	}
	if changes := governanceCount(t, store,
		`SELECT COUNT(*) FROM memory_changes WHERE kind = 'receipt_corrected' AND receipt_id = ?`,
		receipt.ReceiptID); changes != 1 {
		t.Fatalf("correction change rows = %d, want 1", changes)
	}
	trace, err := store.TraceRecord(t.Context(), managementv1alpha1.TraceRecordRequest{RecordID: derived.RecordID})
	if err != nil {
		t.Fatal(err)
	}
	if trace.State != managementv1alpha1.RecordStateInvalidated || trace.Revisions[0].Text != derivedText {
		t.Fatalf("corrected TraceRecord = %+v", trace)
	}

	// A later deletion always clears the correction history too.
	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: receipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-after-correction",
	}); err != nil {
		t.Fatal(err)
	}
	if text := governanceText(t, store, `SELECT text FROM semantic_revisions WHERE record_id = ?`, derived.RecordID); text != "" {
		t.Fatalf("deletion after correction left revision text %q", text)
	}
	if cleared := governanceCount(t, store,
		`SELECT COUNT(*) FROM evidence_sources WHERE receipt_id = ? AND suppressed = 1 AND source_json = ''`,
		receipt.ReceiptID); cleared != 1 {
		t.Fatalf("deletion did not clear %d suppressed source payloads", cleared)
	}
}

func TestListRecordsPagesScopedRecordsWithGovernanceState(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	firstReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "list-source-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service also uses SQLite", IdempotencyKey: "list-source-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	stewardAdd(t, store, firstReceipt.ReceiptID, "job-list-a", "First claim.")
	stewardAdd(t, store, secondReceipt.ReceiptID, "job-list-b", "Second claim.")
	page, err := store.ListRecords(t.Context(), managementv1alpha1.ListRecordsRequest{
		SpaceID: "space-bot-a", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || !page.Truncated || page.NextCursor == "" {
		t.Fatalf("first page = %+v", page)
	}
	if page.Records[0].State != managementv1alpha1.RecordStateActive {
		t.Fatalf("first page state = %q", page.Records[0].State)
	}
	next, err := store.ListRecords(t.Context(), managementv1alpha1.ListRecordsRequest{
		SpaceID: "space-bot-a", Limit: 5, Cursor: page.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Records) != 1 || next.Truncated || next.NextCursor != "" {
		t.Fatalf("second page = %+v", next)
	}
	seen := map[stewardv1alpha1.RecordID]bool{page.Records[0].RecordID: true, next.Records[0].RecordID: true}
	if _, err := store.ListRecords(t.Context(), managementv1alpha1.ListRecordsRequest{SpaceID: "space-absent", Limit: 1}); err == nil {
		t.Fatal("ListRecords accepted an unknown Space")
	}
	if _, err := store.ListRecords(t.Context(), managementv1alpha1.ListRecordsRequest{SpaceID: "space-bot-a", Limit: 0}); err == nil {
		t.Fatal("ListRecords accepted an unbounded page")
	}
	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: firstReceipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-list",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: secondReceipt.ReceiptID, Reason: "approved erasure", IdempotencyKey: "delete-list-b",
	}); err != nil {
		t.Fatal(err)
	}
	after, err := store.ListRecords(t.Context(), managementv1alpha1.ListRecordsRequest{SpaceID: "space-bot-a", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Records) != len(seen) {
		t.Fatalf("forgotten listing = %+v", after)
	}
	for _, record := range after.Records {
		if record.State != managementv1alpha1.RecordStateForgotten {
			t.Fatalf("forgotten listing state = %q for %s", record.State, record.RecordID)
		}
	}
	if _, err := store.TraceRecord(t.Context(), managementv1alpha1.TraceRecordRequest{RecordID: "record-absent"}); err == nil {
		t.Fatal("TraceRecord accepted an unknown Record")
	}
}
