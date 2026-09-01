package appliance

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestRecallMergesSemanticCandidatesWithCompleteProvenance(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	first, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "golang", IdempotencyKey: "semantic-recall-first",
	})
	if err != nil {
		t.Fatal(err)
	}
	firstLease := leaseStewardReceipt(t, store, first.ReceiptID, "job-semantic-recall-first")
	added, err := store.ApplyStewardProposal(t.Context(), firstLease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "The project uses Go.",
		EvidenceRefs: []v1alpha1.ReceiptID{first.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "sqlite", IdempotencyKey: "semantic-recall-second",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondLease := leaseStewardReceipt(t, store, second.ReceiptID, "job-semantic-recall-second")
	if _, err := store.ApplyStewardProposal(t.Context(), secondLease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationMerge, TargetRecordID: added.RecordID, ExpectedRevision: 1,
		Kind: "claim", Text: "The project uses Go and SQLite.",
		EvidenceRefs: []v1alpha1.ReceiptID{second.ReceiptID, first.ReceiptID},
	}); err != nil {
		t.Fatal(err)
	}
	response, err := store.Recall(t.Context(), auth, testRecall("uses", second.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if response.Degraded || len(response.Fragments) != 1 {
		t.Fatalf("semantic Recall = %+v", response)
	}
	fragment := response.Fragments[0]
	if fragment.Text != "The project uses Go and SQLite." || len(fragment.EvidenceRefs) != 2 ||
		!containsReceipt(fragment.EvidenceRefs, first.ReceiptID) || !containsReceipt(fragment.EvidenceRefs, second.ReceiptID) ||
		!reflect.DeepEqual(fragment.RecordRefs, []string{string(added.RecordID)}) {
		t.Fatalf("semantic fragment = %+v", fragment)
	}
	repeated, err := store.Recall(t.Context(), auth, testRecall("uses", second.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response, repeated) {
		t.Fatalf("semantic Recall is nondeterministic:\nfirst=%+v\nsecond=%+v", response, repeated)
	}
}

func TestRecallDeduplicatesSemanticPresentationWithoutCollapsingReceipts(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	text := "the project uses Go"
	first, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: text, IdempotencyKey: "dedup-first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: text, IdempotencyKey: "dedup-second"})
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseStewardReceipt(t, store, first.ReceiptID, "job-semantic-dedup")
	result, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: text,
		EvidenceRefs: []v1alpha1.ReceiptID{first.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := store.Recall(t.Context(), auth, testRecall("project uses", second.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Fragments) != 2 {
		t.Fatalf("Recall fragments = %+v, want two independent receipts", response.Fragments)
	}
	var semanticMerged bool
	var evidence []v1alpha1.ReceiptID
	for _, fragment := range response.Fragments {
		evidence = append(evidence, fragment.EvidenceRefs...)
		if len(fragment.RecordRefs) == 1 && fragment.RecordRefs[0] == string(result.RecordID) {
			semanticMerged = true
			if fragment.FragmentID != "fragment:"+string(first.ReceiptID) {
				t.Fatalf("deduplicated fragment identity = %q", fragment.FragmentID)
			}
		}
	}
	if !semanticMerged || !containsReceipt(evidence, first.ReceiptID) || !containsReceipt(evidence, second.ReceiptID) {
		t.Fatalf("deduplicated provenance fragments = %+v", response.Fragments)
	}
}

func TestRecallSemanticIndexesRemainAuthorizedAndFailureIsDegraded(t *testing.T) {
	store, credentials := bootstrapFixture(t, Options{DataDir: t.TempDir(), Clock: time.Now})
	t.Cleanup(func() { _ = store.Close() })
	operations := []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus}
	authA := callAuth(mustIssue(t, store, credentials["principal:actor-bot-a"], issueRequest(
		"grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, operations,
	)), "actor-bot-a", v1alpha1.AudiencePrivate)
	authB := callAuth(mustIssue(t, store, credentials["principal:actor-bot-b"], issueRequest(
		"grant-bot-b", "actor-bot-b", v1alpha1.AudiencePrivate, operations,
	)), "actor-bot-b", v1alpha1.AudiencePrivate)
	add := func(auth v1alpha1.CallAuthorization, text, key, semanticText, jobID string) v1alpha1.RememberResponse {
		t.Helper()
		receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: text, IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		lease := leaseStewardReceipt(t, store, receipt.ReceiptID, stewardv1alpha1.JobID(jobID))
		if _, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
			Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: semanticText,
			EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
		}); err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	add(authA, "alpha", "semantic-auth-a", "A-only nebula", "job-semantic-auth-a")
	receiptB := add(authB, "beta baseline", "semantic-auth-b", "B-only quasar", "job-semantic-auth-b")
	if _, err := store.db.ExecContext(t.Context(), `DROP TABLE `+semanticSpaceIndexTable("space-bot-b")); err != nil {
		t.Fatal(err)
	}
	responseA, err := store.Recall(t.Context(), authA, testRecall("nebula", ""))
	if err != nil {
		t.Fatal(err)
	}
	if responseA.Degraded || len(responseA.Fragments) != 1 || responseA.Fragments[0].Text != "A-only nebula" {
		t.Fatalf("authorized A semantic Recall = %+v", responseA)
	}
	responseB, err := store.Recall(t.Context(), authB, testRecall("beta baseline", receiptB.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if !responseB.Degraded || len(responseB.Fragments) == 0 || responseB.Fragments[0].Text != "beta baseline" {
		t.Fatalf("degraded B baseline Recall = %+v", responseB)
	}
}

func TestSemanticProjectionDriftRebuildAndStewardDiagnostics(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "postgres", IdempotencyKey: "semantic-rebuild",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseStewardReceipt(t, store, receipt.ReceiptID, "job-semantic-rebuild")
	if _, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "The service uses PostgreSQL.",
		EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
	}); err != nil {
		t.Fatal(err)
	}
	tableName := semanticSpaceIndexTable("space-bot-a")
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM `+tableName); err != nil {
		t.Fatal(err)
	}
	degraded, err := store.Recall(t.Context(), auth, testRecall("PostgreSQL", receipt.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if !degraded.Degraded || len(degraded.Fragments) != 0 {
		t.Fatalf("drifted semantic Recall = %+v", degraded)
	}
	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Steward.CompletedJobs != 1 || inspection.Steward.ActiveRecords != 1 ||
		inspection.Steward.ProjectionStatus != "drift" || inspection.Steward.ProjectionHealthy {
		t.Fatalf("drifted Steward diagnostics = %+v", inspection.Steward)
	}
	if err := store.RebuildFTS(t.Context()); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := store.Recall(t.Context(), auth, testRecall("PostgreSQL", receipt.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Degraded || len(rebuilt.Fragments) != 1 || rebuilt.Fragments[0].Text != "The service uses PostgreSQL." {
		t.Fatalf("rebuilt semantic Recall = %+v", rebuilt)
	}
	inspection, err = store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Steward.ProjectionHealthy || inspection.Steward.ProjectionStatus != "ok" {
		t.Fatalf("rebuilt Steward diagnostics = %+v", inspection.Steward)
	}
}

func TestPendingStewardJobLeavesBaselineRecallObservablyDegraded(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "baseline survives provider outage", IdempotencyKey: "pending-degraded-recall",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := store.Recall(t.Context(), auth, testRecall("provider outage", receipt.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if !response.Degraded || len(response.Fragments) != 1 || response.Fragments[0].Text != "baseline survives provider outage" {
		t.Fatalf("pending baseline Recall = %+v", response)
	}
	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Steward.PendingJobs != 1 || inspection.Steward.ConfiguredWorkers != 0 || inspection.Steward.ConfiguredProviders != 0 {
		t.Fatalf("pending Steward diagnostics = %+v", inspection.Steward)
	}
	if _, err := store.DisableSteward(t.Context(), managementv1alpha1.DisableStewardRequest{
		SpaceIDs: []v1alpha1.SpaceID{"space-bot-a"},
	}); err != nil {
		t.Fatal(err)
	}
	disabled, err := store.Recall(t.Context(), auth, testRecall("provider outage", receipt.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Degraded || len(disabled.Fragments) != 1 {
		t.Fatalf("explicitly disabled baseline Recall = %+v", disabled)
	}
}

func TestSemanticRecallFixedCorpus80(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	type corpusCase struct {
		query     string
		text      string
		receiptID v1alpha1.ReceiptID
		recordID  stewardv1alpha1.RecordID
	}
	corpus := make([]corpusCase, 0, 80)
	for index := range 80 {
		marker := fmt.Sprintf("marker%03d", index)
		receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: fmt.Sprintf("rawseed%03d", index), IdempotencyKey: fmt.Sprintf("semantic-corpus-%03d", index),
		})
		if err != nil {
			t.Fatal(err)
		}
		lease := leaseStewardReceipt(t, store, receipt.ReceiptID, stewardv1alpha1.JobID(fmt.Sprintf("job-semantic-corpus-%03d", index)))
		text := "Derived " + marker + " assertion."
		result, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
			Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: text,
			EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
		})
		if err != nil {
			t.Fatal(err)
		}
		corpus = append(corpus, corpusCase{query: marker, text: text, receiptID: receipt.ReceiptID, recordID: result.RecordID})
	}
	for _, test := range corpus {
		response, err := store.Recall(t.Context(), auth, testRecall(test.query, ""))
		if err != nil {
			t.Fatalf("Recall(%q): %v", test.query, err)
		}
		if response.Degraded || len(response.Fragments) != 1 {
			t.Fatalf("Recall(%q) = %+v", test.query, response)
		}
		fragment := response.Fragments[0]
		if fragment.Text != test.text || !reflect.DeepEqual(fragment.EvidenceRefs, []v1alpha1.ReceiptID{test.receiptID}) ||
			!reflect.DeepEqual(fragment.RecordRefs, []string{string(test.recordID)}) {
			t.Fatalf("Recall(%q) fragment = %+v", test.query, fragment)
		}
	}
}

func containsReceipt(values []v1alpha1.ReceiptID, target v1alpha1.ReceiptID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
