package appliance

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// applyStewardAddDirect writes one Record through the pre-migration-style direct
// lease so a test can seed context without enqueuing a claimable Job.
func applyStewardAddDirect(t *testing.T, store *Store, receiptID v1alpha1.ReceiptID, jobID, text string) stewardv1alpha1.ApplyResult {
	t.Helper()
	lease := leaseStewardReceipt(t, store, receiptID, stewardv1alpha1.JobID(jobID))
	result, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: text,
		EvidenceRefs: []v1alpha1.ReceiptID{receiptID},
	})
	if err != nil {
		t.Fatalf("seed ADD %q: %v", text, err)
	}
	return result
}

func seedStewardHostSource(t *testing.T, store *Store, receiptID v1alpha1.ReceiptID, source facts.Source) {
	t.Helper()
	var spaceID v1alpha1.SpaceID
	var labelSetDigest string
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT space_id, label_set_digest FROM receipts WHERE receipt_id = ?`, receiptID).Scan(
		&spaceID, &labelSetDigest); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO evidence_sources(space_id, label_set_digest, source_key, receipt_id, source_json, request_digest, suppressed)
		 VALUES (?, ?, ?, ?, ?, ?, 0)`,
		spaceID, labelSetDigest, "source-"+string(receiptID), receiptID, string(encoded), "digest-"+string(receiptID)); err != nil {
		t.Fatal(err)
	}
}

func countSemanticRecords(t *testing.T, store *Store) int {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM semantic_records`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func insertForeignRecord(t *testing.T, store *Store, recordID stewardv1alpha1.RecordID, spaceID v1alpha1.SpaceID) {
	t.Helper()
	now := formatTime(store.now().UTC())
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO semantic_records(record_id, space_id, kind, status, current_revision, created_at, updated_at, label_set, label_set_digest)
		 VALUES (?, ?, 'fact', 'active', 1, ?, ?, '[]', ?)`,
		recordID, spaceID, now, now, emptyLabelSetDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO semantic_revisions(record_id, revision, space_id, kind, text, operation, job_id, created_at, fact_json)
		 VALUES (?, 1, ?, 'fact', 'foreign fact', 'ADD', NULL, ?, '')`,
		recordID, spaceID, now); err != nil {
		t.Fatal(err)
	}
}

// TestStewardForgottenReceiptLeaseIsRefused is the deterministic M02 property:
// leased work -> forget -> apply, with no sleeps and no wall-clock dependence.
func TestStewardForgottenReceiptLeaseIsRefused(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "forgotten-lease",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || work.Request.Receipt.ReceiptID != receipt.ReceiptID {
		t.Fatalf("claim found=%v work=%+v err=%v", found, work, err)
	}
	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: receipt.ReceiptID, Reason: "approved", IdempotencyKey: "forget-lease-delete",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "must not apply",
		EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
	}); !errors.Is(err, ErrStewardLeaseLost) {
		t.Fatalf("forgotten lease apply error = %v, want lease lost", err)
	}
	if records := countSemanticRecords(t, store); records != 0 {
		t.Fatalf("forgotten lease created %d Records", records)
	}
}

// TestStewardContextFindsLongAgoRelevantFactAfterNoise proves retrieval is not
// pure recency: an older but lexically relevant head survives more recent noise.
func TestStewardContextFindsLongAgoRelevantFactAfterNoise(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	oldReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the deployment region is eu-west-1", IdempotencyKey: "context-old",
	})
	if err != nil {
		t.Fatal(err)
	}
	old := applyStewardAddDirect(t, store, oldReceipt.ReceiptID, "job-context-old", "The deployment region is eu-west-1.")
	for index := 0; index < 12; index++ {
		noise, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: "miscellaneous chatter " + strconv.Itoa(index), IdempotencyKey: "context-noise-" + strconv.Itoa(index),
		})
		if err != nil {
			t.Fatal(err)
		}
		applyStewardAddDirect(t, store, noise.ReceiptID, "job-context-noise-"+strconv.Itoa(index),
			"Miscellaneous chatter "+strconv.Itoa(index)+".")
	}
	putAndBindSteward(t, store, 1)
	if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "what is the deployment region", IdempotencyKey: "context-target",
	}); err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("claim found=%v err=%v", found, err)
	}
	if len(work.Request.Records) > work.Request.Profile.MaxContextRecords {
		t.Fatalf("context exceeded profile bound: %d", len(work.Request.Records))
	}
	relevant := false
	for _, record := range work.Request.Records {
		if record.RecordID == old.RecordID {
			relevant = true
		}
	}
	if !relevant {
		t.Fatalf("long-ago relevant Record missing from bounded context: %+v", work.Request.Records)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationIgnore,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStewardBoundedBatchAppliesAtomically proves a multi-op Proposal either
// applies completely or not at all.
func TestStewardBoundedBatchAppliesAtomically(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "two independent durable facts", IdempotencyKey: "batch-success",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseStewardReceipt(t, store, receipt.ReceiptID, "job-batch-success")
	result, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Policy: stewardv1alpha1.PolicyBoundedBatch,
		Ops: []stewardv1alpha1.ProposalOp{
			{Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "Batch fact alpha.", EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID}},
			{Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "Batch fact beta.", EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ops) != 2 || result.Ops[0].RecordID == "" || result.Ops[1].RecordID == "" ||
		result.Ops[0].RecordID == result.Ops[1].RecordID {
		t.Fatalf("batch result = %+v", result)
	}

	seed, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "seed fact for stale batch", IdempotencyKey: "batch-seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	seedRecord := applyStewardAddDirect(t, store, seed.ReceiptID, "job-batch-seed", "Seed fact.")

	before := countSemanticRecords(t, store)
	stale, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "stale second operation", IdempotencyKey: "batch-stale",
	})
	if err != nil {
		t.Fatal(err)
	}
	staleLease := leaseStewardReceipt(t, store, stale.ReceiptID, "job-batch-stale")
	declareStewardReadSet(t, store, staleLease.JobID, 1, seedRecord.RecordID, 1, seed.ReceiptID)
	if _, err := store.ApplyStewardProposal(t.Context(), staleLease, stewardv1alpha1.Proposal{
		Policy: stewardv1alpha1.PolicyBoundedBatch,
		Ops: []stewardv1alpha1.ProposalOp{
			{Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "This must roll back.", EvidenceRefs: []v1alpha1.ReceiptID{stale.ReceiptID}},
			{Operation: stewardv1alpha1.OperationMerge, TargetRecordID: seedRecord.RecordID, ExpectedRevision: 99, Kind: "fact",
				Text: "Stale merge.", EvidenceRefs: []v1alpha1.ReceiptID{stale.ReceiptID, seed.ReceiptID}},
		},
	}); !errors.Is(err, ErrStewardConflict) {
		t.Fatalf("stale batch apply error = %v, want conflict", err)
	}
	if after := countSemanticRecords(t, store); after != before {
		t.Fatalf("stale batch changed Record count %d -> %d", before, after)
	}
	var revision uint64
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT current_revision FROM semantic_records WHERE record_id = ?`, seedRecord.RecordID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != seedRecord.Revision {
		t.Fatalf("stale batch advanced seed Record to revision %d", revision)
	}

	// A batch whose second op reaches a different Space must not partially apply.
	insertForeignRecord(t, store, "record-foreign-space", "space-bot-b")
	cross, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "cross-space batch operation", IdempotencyKey: "batch-cross-space",
	})
	if err != nil {
		t.Fatal(err)
	}
	crossLease := leaseStewardReceipt(t, store, cross.ReceiptID, "job-batch-cross-space")
	before = countSemanticRecords(t, store)
	if _, err := store.ApplyStewardProposal(t.Context(), crossLease, stewardv1alpha1.Proposal{
		Policy: stewardv1alpha1.PolicyBoundedBatch,
		Ops: []stewardv1alpha1.ProposalOp{
			{Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "Cross-Space batch must roll back.", EvidenceRefs: []v1alpha1.ReceiptID{cross.ReceiptID}},
			{Operation: stewardv1alpha1.OperationMerge, TargetRecordID: "record-foreign-space", ExpectedRevision: 1, Kind: "fact",
				Text: "Cross-Space merge.", EvidenceRefs: []v1alpha1.ReceiptID{cross.ReceiptID}},
		},
	}); !errors.Is(err, ErrStewardConflict) {
		t.Fatalf("cross-Space batch apply error = %v, want conflict", err)
	}
	if after := countSemanticRecords(t, store); after != before {
		t.Fatalf("cross-Space batch changed Record count %d -> %d", before, after)
	}
}

// TestStewardApplyRevalidatesReadSetIncludingIgnore proves Apply revalidates
// persisted context even for IGNORE, and that a retried claim re-reads context.
func TestStewardApplyRevalidatesReadSetIncludingIgnore(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	t.Cleanup(func() { _ = store.Close() })
	original, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "service fact alpha", IdempotencyKey: "revalidate-original",
	})
	if err != nil {
		t.Fatal(err)
	}
	originalRecord := applyStewardAddDirect(t, store, original.ReceiptID, "job-revalidate-original", "Service fact alpha.")
	mutation, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "service fact beta", IdempotencyKey: "revalidate-mutation",
	})
	if err != nil {
		t.Fatal(err)
	}
	putAndBindSteward(t, store, 1)
	if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "service fact", IdempotencyKey: "revalidate-target",
	}); err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || len(work.Request.Records) != 1 || work.Request.Records[0].RecordID != originalRecord.RecordID {
		t.Fatalf("claim work=%+v found=%v err=%v", work, found, err)
	}
	// Mutate the context Record after the lease was issued.
	mutationLease := leaseStewardReceipt(t, store, mutation.ReceiptID, "job-revalidate-mutation")
	declareStewardReadSet(t, store, mutationLease.JobID, 1, originalRecord.RecordID, 1, original.ReceiptID)
	if _, err := store.ApplyStewardProposal(t.Context(), mutationLease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationMerge, TargetRecordID: originalRecord.RecordID, ExpectedRevision: 1,
		Kind: "fact", Text: "Service fact alpha and beta.",
		EvidenceRefs: []v1alpha1.ReceiptID{mutation.ReceiptID, original.ReceiptID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationIgnore,
	}); !errors.Is(err, ErrStewardConflict) {
		t.Fatalf("IGNORE against drifted context error = %v, want conflict", err)
	}
	if err := store.ReportStewardFailure(t.Context(), stewardv1alpha1.FailRequest{
		Lease: work.Lease, Code: "context_conflict", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	retry, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || retry.Attempt != 2 || len(retry.Request.Records) != 1 {
		t.Fatalf("retry claim=%+v found=%v err=%v", retry, found, err)
	}
	if retry.Request.Records[0].Revision != 2 {
		t.Fatalf("retry re-read stale context: %+v", retry.Request.Records)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), retry.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationIgnore,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStewardHostSourceAddWritesPendingFactMetadata proves a trusted host Source
// seeds pending structured metadata and that a model update can never overwrite
// the structured head.
func TestStewardHostSourceAddWritesPendingFactMetadata(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "host-source-add",
	})
	if err != nil {
		t.Fatal(err)
	}
	seedStewardHostSource(t, store, receipt.ReceiptID, facts.Source{
		Producer: "host", EventID: "event-1", Revision: "1", Fragment: "fragment-1",
		Subject: "service", FactKey: "language", Role: facts.RoleUserQuote,
	})
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || work.Request.Receipt.ReceiptID != receipt.ReceiptID {
		t.Fatalf("claim found=%v work=%+v err=%v", found, work, err)
	}
	if work.Request.Receipt.Subject != "service" || work.Request.Receipt.FactKey != "language" ||
		len(work.Request.Receipt.Sources) != 1 || work.Request.Receipt.Sources[0].Role != stewardv1alpha1.SourceRoleUserQuote {
		t.Fatalf("work input source = %+v", work.Request.Receipt)
	}
	added, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "The service uses Go.",
		EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	var subject, factKey string
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT subject, fact_key FROM semantic_records WHERE record_id = ?`, added.RecordID).Scan(&subject, &factKey); err != nil {
		t.Fatal(err)
	}
	if subject != "service" || factKey != "language" {
		t.Fatalf("record attribution = %q/%q", subject, factKey)
	}
	var encodedFact string
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT fact_json FROM semantic_revisions WHERE record_id = ? AND revision = 1`, added.RecordID).Scan(&encodedFact); err != nil {
		t.Fatal(err)
	}
	var metadata facts.Metadata
	if err := json.Unmarshal([]byte(encodedFact), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Subject != "service" || metadata.Key != "language" ||
		metadata.Adoption != facts.AdoptionPending || metadata.Transition != facts.TransitionEstablish {
		t.Fatalf("pending fact metadata = %+v", metadata)
	}
	// A later model proposal must not overwrite the structured head.
	other, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "a later model claim", IdempotencyKey: "host-source-guard",
	})
	if err != nil {
		t.Fatal(err)
	}
	next, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || next.Request.Receipt.ReceiptID != other.ReceiptID {
		t.Fatalf("guard claim found=%v work=%+v err=%v", found, next, err)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), next.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationMerge, TargetRecordID: added.RecordID, ExpectedRevision: 1,
		Kind: "fact", Text: "must not overwrite", EvidenceRefs: []v1alpha1.ReceiptID{other.ReceiptID},
	}); !errors.Is(err, ErrStewardConflict) {
		t.Fatalf("structured head overwrite error = %v, want conflict", err)
	}
}

// TestStewardApplyRejectsUnreadEvidence proves a proposal can only cite the job
// receipt or evidence the Worker actually read, never a model-supplied reference.
func TestStewardApplyRejectsUnreadEvidence(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	contextReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "service fact alpha", IdempotencyKey: "unread-context",
	})
	if err != nil {
		t.Fatal(err)
	}
	applyStewardAddDirect(t, store, contextReceipt.ReceiptID, "job-unread-context", "Service fact alpha.")
	foreign, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "unrelated note", IdempotencyKey: "unread-foreign",
	})
	if err != nil {
		t.Fatal(err)
	}
	putAndBindSteward(t, store, 1)
	target, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "service fact", IdempotencyKey: "unread-target",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || work.Request.Receipt.ReceiptID != target.ReceiptID {
		t.Fatalf("claim found=%v work=%+v err=%v", found, work, err)
	}
	if len(work.Request.Records) == 1 && !containsReceipt(work.Request.Records[0].EvidenceRefs, contextReceipt.ReceiptID) {
		t.Fatalf("read set omitted context evidence: %+v", work.Request.Records)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "cites unread evidence",
		EvidenceRefs: []v1alpha1.ReceiptID{target.ReceiptID, foreign.ReceiptID},
	}); !errors.Is(err, ErrStewardProposalInvalid) {
		t.Fatalf("unread evidence error = %v, want invalid proposal", err)
	}
}

// TestStewardClaimPersistsExactReadSet is the zero-model path: context and the
// read set are deterministic and need no model to be durable.
func TestStewardClaimPersistsExactReadSet(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	first, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the project uses Go", IdempotencyKey: "readset-first",
	})
	if err != nil {
		t.Fatal(err)
	}
	added := applyStewardAddDirect(t, store, first.ReceiptID, "job-readset-first", "The project uses Go.")
	second, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the project targets Go 1.25", IdempotencyKey: "readset-second",
	})
	if err != nil {
		t.Fatal(err)
	}
	mergeLease := leaseStewardReceipt(t, store, second.ReceiptID, "job-readset-merge")
	declareStewardReadSet(t, store, mergeLease.JobID, 1, added.RecordID, 1, first.ReceiptID)
	if _, err := store.ApplyStewardProposal(t.Context(), mergeLease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationMerge, TargetRecordID: added.RecordID, ExpectedRevision: 1,
		Kind: "fact", Text: "The project uses Go and targets Go 1.25.",
		EvidenceRefs: []v1alpha1.ReceiptID{second.ReceiptID, first.ReceiptID},
	}); err != nil {
		t.Fatal(err)
	}
	putAndBindSteward(t, store, 1)
	if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the project uses", IdempotencyKey: "readset-target",
	}); err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || len(work.Request.Records) != 1 || work.Request.Records[0].Revision != 2 {
		t.Fatalf("claim work=%+v found=%v err=%v", work, found, err)
	}
	rows, err := store.db.QueryContext(t.Context(),
		`SELECT ordinal, record_id, revision, receipt_id FROM steward_read_set WHERE job_id = ? AND attempt = ? ORDER BY ordinal`,
		work.Lease.JobID, work.Attempt)
	if err != nil {
		t.Fatal(err)
	}
	type readRow struct {
		ordinal  int
		recordID string
		revision uint64
		receipt  string
	}
	var read []readRow
	for rows.Next() {
		var row readRow
		if err := rows.Scan(&row.ordinal, &row.recordID, &row.revision, &row.receipt); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		read = append(read, row)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(read) != 2 {
		t.Fatalf("persisted read set = %+v", read)
	}
	readReceipts := map[string]bool{read[0].receipt: true, read[1].receipt: true}
	if !readReceipts[string(second.ReceiptID)] || !readReceipts[string(first.ReceiptID)] {
		t.Fatalf("persisted read set receipts = %+v", read)
	}
	for index, row := range read {
		if row.ordinal != index || row.recordID != string(added.RecordID) || row.revision != 2 {
			t.Fatalf("read set row %d = %+v", index, row)
		}
	}
	if _, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationIgnore,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStewardClaimWithoutContextRejectsUnreadEvidence proves the actual-read
// policy holds even when a Claim legitimately produced an empty context: the
// only acceptable evidence is the job receipt, and a rejected proposal writes
// nothing.
func TestStewardClaimWithoutContextRejectsUnreadEvidence(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	target, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "empty-context-target",
	})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "unrelated note", IdempotencyKey: "empty-context-unrelated",
	})
	if err != nil {
		t.Fatal(err)
	}
	corrected, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "a fact later corrected", IdempotencyKey: "empty-context-corrected",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CorrectReceipt(t.Context(), managementv1alpha1.CorrectReceiptRequest{
		ReceiptID: corrected.ReceiptID, ReplacementText: "the corrected replacement", Reason: "verified",
		IdempotencyKey: "empty-context-correct",
	}); err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || work.Request.Receipt.ReceiptID != target.ReceiptID {
		t.Fatalf("claim found=%v work=%+v err=%v", found, work, err)
	}
	if len(work.Request.Records) != 0 {
		t.Fatalf("expected empty context, got %+v", work.Request.Records)
	}
	for name, evidence := range map[string][]v1alpha1.ReceiptID{
		"unrelated receipt": {target.ReceiptID, unrelated.ReceiptID},
		"corrected receipt": {target.ReceiptID, corrected.ReceiptID},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
				Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "must not persist", EvidenceRefs: evidence,
			}); !errors.Is(err, ErrStewardProposalInvalid) {
				t.Fatalf("evidence error = %v, want invalid proposal", err)
			}
		})
	}
	if records := countSemanticRecords(t, store); records != 0 {
		t.Fatalf("rejected proposals created %d Records", records)
	}
}
