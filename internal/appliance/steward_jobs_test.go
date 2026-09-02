package appliance

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestStewardProfileVersionAndBindingAffectOnlyFutureJobs(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	before, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "receipt before Steward binding", IdempotencyKey: "before-steward-binding",
	})
	if err != nil {
		t.Fatal(err)
	}
	put1, err := store.PutStewardProfile(t.Context(), managementv1alpha1.PutStewardProfileRequest{
		Profile: testStewardProfile(1, "prompt one"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !put1.Created {
		t.Fatal("first profile version was not created")
	}
	retry, err := store.PutStewardProfile(t.Context(), managementv1alpha1.PutStewardProfileRequest{
		Profile: testStewardProfile(1, "prompt one"),
	})
	if err != nil || retry.Created {
		t.Fatalf("profile retry = %+v, error = %v", retry, err)
	}
	changed := testStewardProfile(1, "changed prompt")
	if _, err := store.PutStewardProfile(t.Context(), managementv1alpha1.PutStewardProfileRequest{Profile: changed}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeConflict) {
		t.Fatalf("mutable profile error = %v, want conflict", err)
	}
	bindStewardProfile(t, store, 1)
	first, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "receipt under profile one", IdempotencyKey: "profile-one-job",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutStewardProfile(t.Context(), managementv1alpha1.PutStewardProfileRequest{
		Profile: testStewardProfile(2, "prompt two"),
	}); err != nil {
		t.Fatal(err)
	}
	bindStewardProfile(t, store, 2)
	second, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "receipt under profile two", IdempotencyKey: "profile-two-job",
	})
	if err != nil {
		t.Fatal(err)
	}

	work1, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("first claim found=%v error=%v", found, err)
	}
	if work1.Request.Receipt.ReceiptID != first.ReceiptID || work1.Request.Profile.Version != 1 || work1.Request.Profile.SystemPrompt != "prompt one" {
		t.Fatalf("first Work = %+v", work1)
	}
	encoded, err := json.Marshal(work1.Request)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"space-bot-a", string(work1.Lease.JobID), work1.Lease.Token, "actor-bot-a"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("provider request contains hidden authority %q: %s", forbidden, encoded)
		}
	}
	if _, err := store.ApplyStewardProposal(t.Context(), work1.Lease, stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore}); err != nil {
		t.Fatal(err)
	}
	work2, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("second claim found=%v error=%v", found, err)
	}
	if work2.Request.Receipt.ReceiptID != second.ReceiptID || work2.Request.Profile.Version != 2 || work2.Request.Profile.SystemPrompt != "prompt two" {
		t.Fatalf("second Work = %+v", work2)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), work2.Lease, stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore}); err != nil {
		t.Fatal(err)
	}
	var beforeJobs int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM steward_jobs WHERE receipt_id = ?`, before.ReceiptID).Scan(&beforeJobs); err != nil {
		t.Fatal(err)
	}
	if beforeJobs != 0 {
		t.Fatalf("pre-binding receipt unexpectedly has %d Jobs", beforeJobs)
	}
	configuration, err := store.GetStewardConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(configuration.Profiles) != 2 || len(configuration.Bindings) != 1 || configuration.Bindings[0].ProfileVersion != 2 {
		t.Fatalf("Steward configuration = %+v", configuration)
	}
}

func TestStewardClaimsAreExclusiveAndExpiredLeaseIsRejected(t *testing.T) {
	now := time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC)
	store, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "one exclusively leased fact", IdempotencyKey: "exclusive-steward-job",
	})
	if err != nil {
		t.Fatal(err)
	}
	var foundCount atomic.Int64
	var first StewardWork
	var firstMu sync.Mutex
	errorsFound := make(chan error, 8)
	var claims sync.WaitGroup
	for range 8 {
		claims.Add(1)
		go func() {
			defer claims.Done()
			work, found, err := store.ClaimStewardJob(t.Context(), time.Second)
			if err != nil {
				errorsFound <- err
				return
			}
			if found {
				foundCount.Add(1)
				firstMu.Lock()
				first = work
				firstMu.Unlock()
			}
		}()
	}
	claims.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent claim error = %v", err)
	}
	if foundCount.Load() != 1 || first.Request.Receipt.ReceiptID != receipt.ReceiptID {
		t.Fatalf("exclusive claims = %d, Work = %+v", foundCount.Load(), first)
	}
	if _, found, err := store.ClaimStewardJob(t.Context(), time.Second); err != nil || found {
		t.Fatalf("claim before expiry found=%v error=%v", found, err)
	}
	now = now.Add(2 * time.Second)
	if err := store.ReportStewardFailure(t.Context(), stewardv1alpha1.FailRequest{
		Lease: first.Lease, Code: "late_worker_failure", Retryable: false,
	}); !errors.Is(err, ErrStewardLeaseLost) {
		t.Fatalf("expired lease failure error = %v", err)
	}
	second, found, err := store.ClaimStewardJob(t.Context(), time.Second)
	if err != nil || !found {
		t.Fatalf("reclaim found=%v error=%v", found, err)
	}
	if second.Attempt != 2 || second.Lease.Token == first.Lease.Token {
		t.Fatalf("reclaimed Work = %+v", second)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), first.Lease, stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore}); !errors.Is(err, ErrStewardLeaseLost) {
		t.Fatalf("stale lease Apply error = %v", err)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), second.Lease, stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore}); err != nil {
		t.Fatal(err)
	}
}

func TestStewardExpiredCrashRetriesStopAtApplianceCeiling(t *testing.T) {
	now := time.Date(2026, 9, 1, 1, 30, 0, 0, time.UTC)
	store, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "crashing Worker must stop", IdempotencyKey: "crash-retry-ceiling",
	})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= maxStewardAttempts; attempt++ {
		work, found, err := store.ClaimStewardJob(t.Context(), time.Second)
		if err != nil || !found || work.Attempt != attempt {
			t.Fatalf("crash attempt %d work=%+v found=%v err=%v", attempt, work, found, err)
		}
		now = now.Add(2 * time.Second)
	}
	if _, found, err := store.ClaimStewardJob(t.Context(), time.Second); err != nil || found {
		t.Fatalf("claim after crash ceiling found=%v err=%v", found, err)
	}
	status, err := store.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: receipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != v1alpha1.ProcessingStateFailed || status.TerminalErrorCode != "attempts_exhausted" {
		t.Fatalf("crash retry ceiling status = %+v", status)
	}
}

func TestStewardProposalOutputBudgetIsEnforcedBeforeMutation(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	profile := testStewardProfile(1, "bounded output")
	profile.MaxOutputBytes = 1024
	if _, err := store.PutStewardProfile(t.Context(), managementv1alpha1.PutStewardProfileRequest{Profile: profile}); err != nil {
		t.Fatal(err)
	}
	bindStewardProfile(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "proposal output budget", IdempotencyKey: "proposal-output-budget",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("claim found=%v err=%v", found, err)
	}
	_, err = store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation:    stewardv1alpha1.OperationAdd,
		Kind:         "fact",
		Text:         strings.Repeat("x", 1200),
		EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
	})
	if !errors.Is(err, ErrStewardProposalInvalid) {
		t.Fatalf("oversized proposal error = %v", err)
	}
	var records int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM semantic_records`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("oversized proposal created %d Records", records)
	}
}

func TestStewardRefinementStaysWithinExactLabelSet(t *testing.T) {
	store, credentials := bootstrapFixture(t, Options{DataDir: t.TempDir(), Clock: time.Now})
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	operations := []v1alpha1.Operation{
		v1alpha1.OperationRemember,
		v1alpha1.OperationRecall,
		v1alpha1.OperationReceiptStatus,
	}
	issue := func(label string) v1alpha1.CallAuthorization {
		request := issueRequest("grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, operations)
		request.Labels = v1alpha1.LabelSet{v1alpha1.Label(label)}
		capability := mustIssue(t, store, credentials["principal:actor-bot-a"], request)
		return callAuth(capability, "actor-bot-a", v1alpha1.AudiencePrivate)
	}
	demoAuth := issue("workspace:demo")
	caelisAuth := issue("workspace:caelis")

	demo, err := store.Remember(t.Context(), demoAuth, v1alpha1.RememberRequest{
		Text: "demo uses React", IdempotencyKey: "labels-demo-record",
	})
	if err != nil {
		t.Fatal(err)
	}
	demoWork, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || len(demoWork.Request.Records) != 0 {
		t.Fatalf("demo Work = %+v, found=%v, error=%v", demoWork, found, err)
	}
	encodedWork, err := json.Marshal(demoWork.Request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedWork), "workspace:demo") {
		t.Fatalf("model-facing Steward Work exposed LabelSet: %s", encodedWork)
	}
	demoRecord, err := store.ApplyStewardProposal(t.Context(), demoWork.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "The demo uses React.",
		EvidenceRefs: []v1alpha1.ReceiptID{demo.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := store.GetSemanticRecord(t.Context(), demoRecord.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(record.Labels, v1alpha1.LabelSet{"workspace:demo"}) {
		t.Fatalf("promoted Record Labels = %v", record.Labels)
	}

	caelis, err := store.Remember(t.Context(), caelisAuth, v1alpha1.RememberRequest{
		Text: "Caelis uses Go", IdempotencyKey: "labels-caelis-record",
	})
	if err != nil {
		t.Fatal(err)
	}
	caelisWork, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("Caelis Work found=%v error=%v", found, err)
	}
	if len(caelisWork.Request.Records) != 0 {
		t.Fatalf("Caelis Work saw another LabelSet: %+v", caelisWork.Request.Records)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), caelisWork.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "Caelis uses Go.",
		EvidenceRefs: []v1alpha1.ReceiptID{caelis.ReceiptID, demo.ReceiptID},
	}); !errors.Is(err, ErrStewardProposalInvalid) {
		t.Fatalf("cross-LabelSet proposal error = %v", err)
	}
	caelisRecord, err := store.ApplyStewardProposal(t.Context(), caelisWork.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "fact", Text: "Caelis uses Go.",
		EvidenceRefs: []v1alpha1.ReceiptID{caelis.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	caelisNext, err := store.Remember(t.Context(), caelisAuth, v1alpha1.RememberRequest{
		Text: "Caelis ships an embedded Memory package", IdempotencyKey: "labels-caelis-refine",
	})
	if err != nil {
		t.Fatal(err)
	}
	caelisNextWork, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("next Caelis Work found=%v error=%v", found, err)
	}
	if len(caelisNextWork.Request.Records) != 1 || caelisNextWork.Request.Records[0].RecordID != caelisRecord.RecordID {
		t.Fatalf("next Caelis Work Records = %+v", caelisNextWork.Request.Records)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), caelisNextWork.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationMerge, TargetRecordID: demoRecord.RecordID, ExpectedRevision: 1,
		Kind: "fact", Text: "invalid cross-label refinement",
		EvidenceRefs: []v1alpha1.ReceiptID{caelis.ReceiptID, caelisNext.ReceiptID},
	}); !errors.Is(err, ErrStewardConflict) {
		t.Fatalf("cross-LabelSet refinement error = %v", err)
	}
	merged, err := store.ApplyStewardProposal(t.Context(), caelisNextWork.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationMerge, TargetRecordID: caelisRecord.RecordID, ExpectedRevision: 1,
		Kind: "fact", Text: "Caelis uses Go and ships an embedded Memory package.",
		EvidenceRefs: []v1alpha1.ReceiptID{caelis.ReceiptID, caelisNext.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	mergedRecord, _, err := store.GetSemanticRecord(t.Context(), merged.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if mergedRecord.CurrentRevision != 2 || !slices.Equal(mergedRecord.Labels, v1alpha1.LabelSet{"workspace:caelis"}) {
		t.Fatalf("refined Record = %+v", mergedRecord)
	}
	recalled, err := store.Recall(t.Context(), demoAuth, testRecall("uses", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range recalled.Fragments {
		if strings.Contains(fragment.Text, "Caelis") {
			t.Fatalf("demo Recall crossed LabelSet: %+v", recalled.Fragments)
		}
	}

	demoNext, err := store.Remember(t.Context(), demoAuth, v1alpha1.RememberRequest{
		Text: "demo deploys to Vercel", IdempotencyKey: "labels-demo-refine",
	})
	if err != nil {
		t.Fatal(err)
	}
	demoNextWork, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("next demo Work found=%v error=%v", found, err)
	}
	if len(demoNextWork.Request.Records) != 1 || demoNextWork.Request.Records[0].RecordID != demoRecord.RecordID {
		t.Fatalf("next demo Work Records = %+v", demoNextWork.Request.Records)
	}
	if !slices.Contains(demoNextWork.Request.Records[0].EvidenceRefs, demo.ReceiptID) || demoNext.ReceiptID == "" {
		t.Fatalf("next demo Work = %+v", demoNextWork)
	}
}

func TestStewardRetryTerminalFailureAndDisableAreDurable(t *testing.T) {
	now := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	store, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "Worker retry fact", IdempotencyKey: "worker-retry-job",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("claim found=%v error=%v", found, err)
	}
	if err := store.ReportStewardFailure(t.Context(), stewardv1alpha1.FailRequest{
		Lease: work.Lease, Code: "worker_unavailable", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ClaimStewardJob(t.Context(), time.Minute); err != nil || found {
		t.Fatalf("early retry found=%v error=%v", found, err)
	}
	now = now.Add(3 * time.Second)
	retry, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || retry.Attempt != 2 {
		t.Fatalf("retry Work=%+v found=%v error=%v", retry, found, err)
	}
	if err := store.ReportStewardFailure(t.Context(), stewardv1alpha1.FailRequest{
		Lease: retry.Lease, Code: "worker_unavailable", Retryable: false,
	}); err != nil {
		t.Fatal(err)
	}
	status, err := store.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: receipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != v1alpha1.ProcessingStateFailed || status.TerminalErrorCode != "worker_unavailable" {
		t.Fatalf("terminal ReceiptStatus = %+v", status)
	}
	pending, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "pending work disabled", IdempotencyKey: "disabled-steward-job",
	})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := store.DisableSteward(t.Context(), managementv1alpha1.DisableStewardRequest{SpaceIDs: []v1alpha1.SpaceID{"space-bot-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Disabled != 1 || disabled.CanceledJobs != 1 {
		t.Fatalf("DisableSteward = %+v", disabled)
	}
	status, err = store.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: pending.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != v1alpha1.ProcessingStateFailed || status.TerminalErrorCode != "steward_disabled" {
		t.Fatalf("disabled ReceiptStatus = %+v", status)
	}
	after, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "baseline continues after disable", IdempotencyKey: "after-steward-disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM steward_jobs WHERE receipt_id = ?`, after.ReceiptID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("disabled Steward enqueued %d Jobs", jobs)
	}
	recalled, err := store.Recall(t.Context(), auth, testRecall("baseline", after.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, recalled, "baseline continues after disable")
}

func TestStewardWorkerRetryCeilingIsApplianceOwned(t *testing.T) {
	now := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	store, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "bounded external Worker retry", IdempotencyKey: "bounded-worker-retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= maxStewardAttempts; attempt++ {
		work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
		if err != nil || !found || work.Attempt != attempt {
			t.Fatalf("attempt %d claim=%+v found=%v err=%v", attempt, work, found, err)
		}
		if err := store.ReportStewardFailure(t.Context(), stewardv1alpha1.FailRequest{
			Lease: work.Lease, Code: "generator_timeout", Retryable: true,
		}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(stewardRetryDelay(attempt) + time.Second)
	}
	if _, found, err := store.ClaimStewardJob(t.Context(), time.Minute); err != nil || found {
		t.Fatalf("claim after terminal retry ceiling found=%v err=%v", found, err)
	}
	status, err := store.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: receipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := store.db.QueryRowContext(t.Context(), `SELECT attempts FROM steward_jobs WHERE receipt_id = ?`, receipt.ReceiptID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if status.State != v1alpha1.ProcessingStateFailed || attempts != maxStewardAttempts || status.TerminalErrorCode != "generator_timeout" {
		t.Fatalf("terminal retry-ceiling status = %+v", status)
	}
}

func testStewardProfile(version uint64, prompt string) stewardv1alpha1.ProfileSpec {
	return stewardv1alpha1.ProfileSpec{
		ProfileID: "profile-managed", Version: version,
		SystemPrompt: prompt, MaxContextRecords: 8, MaxInputBytes: 128 << 10, MaxOutputBytes: 16 << 10,
	}
}

func putAndBindSteward(t testing.TB, store *Store, version uint64) {
	t.Helper()
	if _, err := store.PutStewardProfile(t.Context(), managementv1alpha1.PutStewardProfileRequest{
		Profile: testStewardProfile(version, "organize evidence"),
	}); err != nil {
		t.Fatal(err)
	}
	bindStewardProfile(t, store, version)
}

func bindStewardProfile(t testing.TB, store *Store, version uint64) {
	t.Helper()
	if _, err := store.BindStewardProfile(t.Context(), managementv1alpha1.BindStewardProfileRequest{
		ProfileID: "profile-managed", Version: version, SpaceIDs: []v1alpha1.SpaceID{"space-bot-a"},
	}); err != nil {
		t.Fatal(err)
	}
}
