package appliance

import (
	"encoding/json"
	"errors"
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

func TestStewardRetryTerminalFailureAndDisableAreDurable(t *testing.T) {
	now := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	store, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "provider retry fact", IdempotencyKey: "provider-retry-job",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("claim found=%v error=%v", found, err)
	}
	if err := store.FailStewardJob(t.Context(), work.Lease, StewardFailure{Code: "provider_unavailable", RetryAfter: 2 * time.Second}); err != nil {
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
	if err := store.FailStewardJob(t.Context(), retry.Lease, StewardFailure{Code: "provider_unavailable", Terminal: true}); err != nil {
		t.Fatal(err)
	}
	status, err := store.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: receipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != v1alpha1.ProcessingStateFailed || status.TerminalErrorCode != "provider_unavailable" {
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

func testStewardProfile(version uint64, prompt string) stewardv1alpha1.ProfileSpec {
	return stewardv1alpha1.ProfileSpec{
		ProfileID: "profile-managed", Version: version, ProviderRef: "provider-test", Model: "model-test",
		SystemPrompt: prompt, MaxContextRecords: 8, MaxInputBytes: 128 << 10, MaxOutputBytes: 16 << 10,
	}
}

func putAndBindSteward(t *testing.T, store *Store, version uint64) {
	t.Helper()
	if _, err := store.PutStewardProfile(t.Context(), managementv1alpha1.PutStewardProfileRequest{
		Profile: testStewardProfile(version, "organize evidence"),
	}); err != nil {
		t.Fatal(err)
	}
	bindStewardProfile(t, store, version)
}

func bindStewardProfile(t *testing.T, store *Store, version uint64) {
	t.Helper()
	if _, err := store.BindStewardProfile(t.Context(), managementv1alpha1.BindStewardProfileRequest{
		ProfileID: "profile-managed", Version: version, SpaceIDs: []v1alpha1.SpaceID{"space-bot-a"},
	}); err != nil {
		t.Fatal(err)
	}
}
