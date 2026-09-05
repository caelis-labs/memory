package appliance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/conformance"
)

func TestSemanticConformance(t *testing.T) {
	conformance.RunSemantic(t, newConformanceFixture)
}

func TestSQLiteFileDSNWindowsDriveLetterHasEmptyAuthority(t *testing.T) {
	query := url.Values{}
	query.Set("_txlock", "immediate")
	dsn := sqliteFileDSN(`C:\Users\15528\.caelis\memory\appliance\memory.db`, query)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "" {
		t.Fatalf("authority = %q, dsn = %q", parsed.Host, dsn)
	}
	if parsed.Path != "/C:/Users/15528/.caelis/memory/appliance/memory.db" {
		t.Fatalf("path = %q, dsn = %q", parsed.Path, dsn)
	}
	if parsed.Query().Get("_txlock") != "immediate" {
		t.Fatalf("query = %q", parsed.RawQuery)
	}
}

func TestSQLiteFileDSNUnixAbsoluteHasEmptyAuthority(t *testing.T) {
	query := url.Values{}
	query.Set("_txlock", "immediate")
	dsn := sqliteFileDSN("/home/user/.caelis/memory/appliance/memory.db", query)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "" {
		t.Fatalf("authority = %q, dsn = %q", parsed.Host, dsn)
	}
	if parsed.Path != "/home/user/.caelis/memory/appliance/memory.db" {
		t.Fatalf("path = %q, dsn = %q", parsed.Path, dsn)
	}
}

func TestOpenPingsSQLiteOnNativeTempDir(t *testing.T) {
	store, err := Open(t.Context(), Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
}

func TestSyncDirectorySucceedsOnNativeTempDir(t *testing.T) {
	if err := syncDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestDurableRestartAndIdempotency(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()
	store, auth := newGoldenStore(t, dataDir, func() time.Time { return now })
	request := v1alpha1.RememberRequest{Text: "durable restart fact", IdempotencyKey: "durable-key"}
	first, err := store.Remember(t.Context(), auth, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := Open(t.Context(), Options{DataDir: dataDir, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	recalled, err := restarted.Recall(t.Context(), auth, testRecall("restart", first.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, recalled, request.Text)
	second, err := restarted.Remember(t.Context(), auth, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.ReceiptID != first.ReceiptID || !second.DeduplicatedRetry {
		t.Fatalf("retry = %+v, first = %+v", second, first)
	}
}

func TestLabelSetPartitionSurvivesRestart(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()
	store, credentials := bootstrapFixture(t, Options{DataDir: dataDir, Clock: func() time.Time { return now }})
	operations := []v1alpha1.Operation{
		v1alpha1.OperationRemember,
		v1alpha1.OperationRecall,
		v1alpha1.OperationReceiptStatus,
	}
	issueLabeled := func(label string) v1alpha1.CallAuthorization {
		request := issueRequest("grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, operations)
		request.Labels = v1alpha1.LabelSet{v1alpha1.Label(label)}
		return callAuth(mustIssue(t, store, credentials["principal:actor-bot-a"], request), "actor-bot-a", v1alpha1.AudiencePrivate)
	}
	demoAuth := issueLabeled("workspace:demo")
	caelisAuth := issueLabeled("workspace:caelis")
	demo, err := store.Remember(t.Context(), demoAuth, v1alpha1.RememberRequest{
		Text: "shared keyword belongs to demo", IdempotencyKey: "restart-label-demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Remember(t.Context(), caelisAuth, v1alpha1.RememberRequest{
		Text: "shared keyword belongs to caelis", IdempotencyKey: "restart-label-caelis",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := Open(t.Context(), Options{DataDir: dataDir, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	recalled, err := restarted.Recall(t.Context(), demoAuth, testRecall("shared keyword", demo.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, recalled, "shared keyword belongs to demo")
	for _, fragment := range recalled.Fragments {
		if strings.Contains(fragment.Text, "belongs to caelis") {
			t.Fatalf("Recall crossed a persisted LabelSet: %+v", recalled.Fragments)
		}
	}
	if _, err := restarted.GetReceiptStatus(t.Context(), caelisAuth, v1alpha1.GetReceiptStatusRequest{ReceiptID: demo.ReceiptID}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeNotFound) {
		t.Fatalf("cross-LabelSet ReceiptStatus error = %v, want not_found", err)
	}
}

func TestOwnerLockRejectsSecondProcessOwner(t *testing.T) {
	store, err := Open(t.Context(), Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = Open(t.Context(), Options{DataDir: store.dataDir})
	if !errors.Is(err, ErrOwnerLocked) {
		t.Fatalf("second Open() error = %v, want ErrOwnerLocked", err)
	}
}

func TestCredentialAndDatabasePathsRejectSymlinks(t *testing.T) {
	for _, filename := range []string{DatabaseFilename, ManagementCredentialFile, StewardWorkerCredentialFile, OwnerLockFilename} {
		t.Run(filename, func(t *testing.T) {
			dataDir := t.TempDir()
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("do not follow"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dataDir, filename)); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if store, err := Open(t.Context(), Options{DataDir: dataDir}); err == nil {
				_ = store.Close()
				t.Fatalf("Open() followed %s symlink", filename)
			}
			content, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "do not follow" {
				t.Fatalf("symlink target changed to %q", content)
			}
		})
	}
}

func TestSQLiteLockReturnsUnavailableWithoutAcceptance(t *testing.T) {
	dataDir := t.TempDir()
	store, auth := newGoldenStoreWithOptions(t, Options{DataDir: dataDir, BusyTimeoutMS: 10})
	t.Cleanup(func() { _ = store.Close() })
	external, err := sql.Open("sqlite", filepath.Join(dataDir, DatabaseFilename))
	if err != nil {
		t.Fatal(err)
	}
	defer external.Close()
	if _, err := external.ExecContext(t.Context(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	_, rememberErr := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "locked write", IdempotencyKey: "locked-write",
	})
	if !v1alpha1.IsCode(rememberErr, v1alpha1.ErrorCodeUnavailable) {
		t.Fatalf("Remember() error = %v, want unavailable", rememberErr)
	}
	if _, err := external.ExecContext(t.Context(), `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Counts["receipts"] != 0 {
		t.Fatalf("receipt count = %d, want 0", inspection.Counts["receipts"])
	}
}

func TestConcurrentWritersWaitBeforeReadingCanonicalState(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var blocked atomic.Bool
	store, auth := newGoldenStoreWithOptions(t, Options{
		DataDir: t.TempDir(), BusyTimeoutMS: 1_000,
		Faults: Faults{BeforeRememberCommit: func() error {
			if blocked.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return nil
		}},
	})
	t.Cleanup(func() { _ = store.Close() })

	type result struct {
		response v1alpha1.RememberResponse
		err      error
	}
	first := make(chan result, 1)
	second := make(chan result, 1)
	go func() {
		response, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: "first concurrent writer", IdempotencyKey: "writer-first",
		})
		first <- result{response: response, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first writer did not reach the commit boundary")
	}
	go func() {
		response, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: "second concurrent writer", IdempotencyKey: "writer-second",
		})
		second <- result{response: response, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)

	for name, resultChannel := range map[string]<-chan result{"first": first, "second": second} {
		select {
		case outcome := <-resultChannel:
			if outcome.err != nil || !outcome.response.Accepted {
				t.Fatalf("%s concurrent writer = %+v, %v", name, outcome.response, outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s concurrent writer did not finish", name)
		}
	}
	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Receipts.Stored != 2 {
		t.Fatalf("stored receipts = %d, want 2", inspection.Receipts.Stored)
	}
}

func TestSchemaBaselineFailureIsExplicit(t *testing.T) {
	dataDir := t.TempDir()
	store, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	external, err := sql.Open("sqlite", filepath.Join(dataDir, DatabaseFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := external.ExecContext(t.Context(), `DELETE FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if err := external.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Open(t.Context(), Options{DataDir: dataDir})
	if err == nil || !strings.Contains(err.Error(), "apply schema baseline") {
		t.Fatalf("Open() error = %v, want explicit schema baseline failure", err)
	}
}

func TestFTSProjectionRebuildsFromReceipts(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "rebuildable lexical evidence", IdempotencyKey: "rebuild-key",
	}); err != nil {
		t.Fatal(err)
	}
	tableName := spaceIndexTable("space-bot-a")
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM `+tableName); err != nil {
		t.Fatal(err)
	}
	empty, err := store.Recall(t.Context(), auth, testRecall("rebuildable", ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Fragments) != 0 {
		t.Fatalf("Recall before rebuild = %+v, want empty", empty.Fragments)
	}
	if err := store.RebuildFTS(t.Context()); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := store.Recall(t.Context(), auth, testRecall("rebuildable", ""))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, rebuilt, "rebuildable lexical evidence")
}

func TestReceiptPayloadIsImmutable(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	remembered, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "immutable evidence", IdempotencyKey: "immutable-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`UPDATE receipts SET text = 'mutated' WHERE receipt_id = ?`, remembered.ReceiptID); err == nil {
		t.Fatal("receipt payload update succeeded")
	}
	if _, err := store.db.ExecContext(t.Context(),
		`DELETE FROM receipts WHERE receipt_id = ?`, remembered.ReceiptID); err == nil {
		t.Fatal("receipt payload delete succeeded without a governance tombstone")
	}
	recalled, err := store.Recall(t.Context(), auth, testRecall("immutable", remembered.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, recalled, "immutable evidence")
}

func TestRecallTreatsFTSSyntaxAsText(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "literal operator evidence", IdempotencyKey: "fts-syntax",
	}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`" OR *`, `NEAR(operator`, `***`, "   "} {
		if _, err := store.Recall(t.Context(), auth, testRecall(query, "")); err != nil {
			t.Fatalf("Recall(%q) error = %v", query, err)
		}
	}
}

func TestRecallDeadlineCoversCandidateQuery(t *testing.T) {
	store, auth := newGoldenStoreWithOptions(t, Options{
		DataDir: t.TempDir(),
		CandidateRead: func(v1alpha1.SpaceID) {
			time.Sleep(5 * time.Millisecond)
		},
	})
	t.Cleanup(func() { _ = store.Close() })
	request := testRecall("deadline", "")
	request.Budget.DeadlineMS = 1
	_, err := store.Recall(t.Context(), auth, request)
	if !v1alpha1.IsCode(err, v1alpha1.ErrorCodeDeadline) {
		t.Fatalf("Recall() error = %v, want deadline", err)
	}
}

func TestCorruptedSharedProjectionCannotExposePrivateReceipt(t *testing.T) {
	fixture := newConformanceFixture(t)
	store := fixture.Service.(*Store)
	private, err := store.Remember(t.Context(), fixture.BotAPrivate, v1alpha1.RememberRequest{
		Text: "private projection contamination sentinel", IdempotencyKey: "private-contamination",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO `+spaceIndexTable(fixture.SharedSpace)+`(receipt_id, text) VALUES (?, ?)`,
		private.ReceiptID, "private projection contamination sentinel"); err != nil {
		t.Fatal(err)
	}
	response, err := store.Recall(t.Context(), fixture.SharedB, testRecall("contamination sentinel", ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Fragments) != 0 {
		t.Fatalf("shared Recall exposed corrupted private projection: %+v", response.Fragments)
	}
}

func TestRememberFailureBoundaries(t *testing.T) {
	t.Run("before commit is unavailable and not accepted", func(t *testing.T) {
		store, auth := newGoldenStoreWithOptions(t, Options{
			DataDir: t.TempDir(),
			Faults:  Faults{BeforeRememberCommit: func() error { return errors.New("disk full") }},
		})
		t.Cleanup(func() { _ = store.Close() })
		_, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: "not committed", IdempotencyKey: "disk-full"})
		if !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnavailable) {
			t.Fatalf("Remember() error = %v, want unavailable", err)
		}
		inspection, inspectErr := store.Inspect(t.Context())
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		if inspection.Counts["receipts"] != 0 {
			t.Fatalf("receipt count = %d, want 0", inspection.Counts["receipts"])
		}
	})

	t.Run("after commit is unknown and retry resolves original", func(t *testing.T) {
		store, auth := newGoldenStoreWithOptions(t, Options{
			DataDir: t.TempDir(),
			Faults:  Faults{AfterRememberCommit: func() error { return errors.New("connection lost") }},
		})
		t.Cleanup(func() { _ = store.Close() })
		request := v1alpha1.RememberRequest{Text: "committed unknown effect", IdempotencyKey: "unknown-outcome"}
		_, err := store.Remember(t.Context(), auth, request)
		if !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnknownOutcome) {
			t.Fatalf("Remember() error = %v, want unknown_outcome", err)
		}
		store.faults.AfterRememberCommit = nil
		retry, err := store.Remember(t.Context(), auth, request)
		if err != nil {
			t.Fatal(err)
		}
		if !retry.DeduplicatedRetry {
			t.Fatalf("retry = %+v, want deduplicated original", retry)
		}
		inspection, inspectErr := store.Inspect(t.Context())
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		if inspection.Counts["receipts"] != 1 {
			t.Fatalf("receipt count = %d, want 1", inspection.Counts["receipts"])
		}
	})
}

func TestIssuerRotationRecoversLostBootstrapResponse(t *testing.T) {
	store, credentials := bootstrapFixture(t, Options{DataDir: t.TempDir()})
	t.Cleanup(func() { _ = store.Close() })
	request := issueRequest(
		"grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate,
		[]v1alpha1.Operation{v1alpha1.OperationRecall},
	)
	request.Authorization = IssuerAuthorization{
		PrincipalRef: "principal:actor-bot-a",
		Credential:   credentials["principal:actor-bot-a"],
	}
	rotated, err := store.RotateIssuerCredential(t.Context(), "principal:actor-bot-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueCapability(t.Context(), request); err == nil {
		t.Fatal("pre-rotation issuer credential remained valid")
	}
	request.Authorization = rotated
	if _, err := store.IssueCapability(t.Context(), request); err != nil {
		t.Fatalf("rotated issuer credential did not recover issuance: %v", err)
	}
	if _, err := store.RotateIssuerCredential(t.Context(), "principal:actor-bot-a"); err != nil {
		t.Fatalf("repeated recovery rotation failed: %v", err)
	}
}

func TestValidateCapabilityAuthorityIsSideEffectFree(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	store, credentials := bootstrapFixture(t, Options{DataDir: t.TempDir(), Clock: func() time.Time { return now }})
	t.Cleanup(func() { _ = store.Close() })
	now = now.Add(2 * time.Hour)
	if err := store.RevokeGrant(t.Context(), "grant-revoked"); err != nil {
		t.Fatal(err)
	}
	valid := CapabilityAuthorityRequest{
		Authorization: IssuerAuthorization{
			PrincipalRef: "principal:actor-bot-a",
			Credential:   credentials["principal:actor-bot-a"],
		},
		GrantRef: "grant-bot-a", ViewRef: "view-bot-a-private", ActorRef: "actor-bot-a",
		Audience: v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{
			v1alpha1.OperationRemember,
			v1alpha1.OperationRecall,
			v1alpha1.OperationReceiptStatus,
		},
	}
	tests := []struct {
		name string
		edit func(*CapabilityAuthorityRequest)
		want error
	}{
		{name: "valid"},
		{name: "credential", edit: func(request *CapabilityAuthorityRequest) { request.Authorization.Credential = "wrong" }, want: ErrCapabilityIssueUnauthorized},
		{name: "principal", edit: func(request *CapabilityAuthorityRequest) { request.Authorization.PrincipalRef = "principal:other" }, want: ErrCapabilityIssueUnauthorized},
		{name: "Grant", edit: func(request *CapabilityAuthorityRequest) { request.GrantRef = "grant-missing" }, want: ErrCapabilityIssueUnauthorized},
		{name: "View", edit: func(request *CapabilityAuthorityRequest) { request.ViewRef = "view-shared" }, want: ErrCapabilityIssueUnauthorized},
		{name: "actor", edit: func(request *CapabilityAuthorityRequest) { request.ActorRef = "actor-other" }, want: ErrCapabilityIssueUnauthorized},
		{name: "audience", edit: func(request *CapabilityAuthorityRequest) { request.Audience = v1alpha1.AudienceShared }, want: ErrCapabilityIssueUnauthorized},
		{name: "operation", edit: func(request *CapabilityAuthorityRequest) { request.Operations = []v1alpha1.Operation{"unsupported"} }, want: ErrCapabilityIssueUnauthorized},
		{name: "empty operations", edit: func(request *CapabilityAuthorityRequest) { request.Operations = nil }, want: ErrCapabilityIssueInvalid},
		{name: "expired Grant", edit: func(request *CapabilityAuthorityRequest) { request.GrantRef = "grant-expired" }, want: ErrCapabilityIssueUnauthorized},
		{name: "revoked Grant", edit: func(request *CapabilityAuthorityRequest) { request.GrantRef = "grant-revoked" }, want: ErrCapabilityIssueUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			request.Operations = slices.Clone(valid.Operations)
			if test.edit != nil {
				test.edit(&request)
			}
			beforeCapabilities, beforeOperations := capabilityRowCounts(t, store)
			err := store.ValidateCapabilityAuthority(t.Context(), request)
			if test.want == nil && err != nil {
				t.Fatalf("ValidateCapabilityAuthority() = %v", err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("ValidateCapabilityAuthority() error = %v, want %v", err, test.want)
			}
			afterCapabilities, afterOperations := capabilityRowCounts(t, store)
			if afterCapabilities != beforeCapabilities || afterOperations != beforeOperations {
				t.Fatalf("authority validation persisted rows: capabilities %d -> %d, operations %d -> %d",
					beforeCapabilities, afterCapabilities, beforeOperations, afterOperations)
			}
		})
	}
}

func capabilityRowCounts(t testing.TB, store *Store) (int64, int64) {
	t.Helper()
	var capabilities, operations int64
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM capabilities`).Scan(&capabilities); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM capability_operations`).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	return capabilities, operations
}

func TestConcurrentRememberRecallRevokeAndShutdown(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	var start sync.WaitGroup
	start.Add(3)
	var operations sync.WaitGroup
	operations.Add(3)
	for worker := range 3 {
		go func() {
			defer operations.Done()
			start.Done()
			start.Wait()
			for index := range 20 {
				_, _ = store.Remember(context.Background(), auth, v1alpha1.RememberRequest{
					Text:           fmt.Sprintf("concurrent fact %d %d", worker, index),
					IdempotencyKey: fmt.Sprintf("concurrent-%d-%d", worker, index),
				})
				_, _ = store.Recall(context.Background(), auth, testRecall("concurrent", ""))
			}
		}()
	}
	start.Wait()
	_ = store.RevokeGrant(context.Background(), "grant-bot-a")
	_ = store.Close()
	operations.Wait()
}

func newConformanceFixture(t *testing.T) conformance.Fixture {
	t.Helper()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var privateReads atomic.Uint64
	var sharedReads atomic.Uint64
	store, credentials := bootstrapFixture(t, Options{
		DataDir: t.TempDir(),
		Clock:   func() time.Time { return now },
		CandidateRead: func(spaceID v1alpha1.SpaceID) {
			switch spaceID {
			case "space-bot-a":
				privateReads.Add(1)
			case "space-shared":
				sharedReads.Add(1)
			}
		},
	})
	t.Cleanup(func() { _ = store.Close() })
	allOperations := []v1alpha1.Operation{
		v1alpha1.OperationRemember,
		v1alpha1.OperationRecall,
		v1alpha1.OperationReceiptStatus,
	}
	expired := mustIssue(t, store, credentials["principal:actor-bot-a"], IssueCapabilityRequest{
		GrantRef: "grant-expired", ActorRef: "actor-bot-a", Audience: v1alpha1.AudiencePrivate,
		Operations: allOperations, TTL: time.Hour,
	})
	now = now.Add(2 * time.Hour)
	botA := mustIssue(t, store, credentials["principal:actor-bot-a"], issueRequest("grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations))
	botARenewed := mustIssue(t, store, credentials["principal:actor-bot-a"], issueRequest("grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations))
	labelRequest := issueRequest("grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations)
	labelRequest.Labels = v1alpha1.LabelSet{"workspace:demo"}
	botALabeled := mustIssue(t, store, credentials["principal:actor-bot-a"], labelRequest)
	otherRequest := issueRequest("grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations)
	otherRequest.Labels = v1alpha1.LabelSet{"workspace:caelis"}
	botAOther := mustIssue(t, store, credentials["principal:actor-bot-a"], otherRequest)
	botB := mustIssue(t, store, credentials["principal:actor-bot-b"], issueRequest("grant-bot-b", "actor-bot-b", v1alpha1.AudiencePrivate, allOperations))
	sharedA := mustIssue(t, store, credentials["principal:actor-shared-a"], issueRequest("grant-shared-a", "actor-shared-a", v1alpha1.AudienceShared, allOperations))
	sharedB := mustIssue(t, store, credentials["principal:actor-shared-b"], issueRequest("grant-shared-b", "actor-shared-b", v1alpha1.AudienceShared, allOperations))
	recallOnly := mustIssue(t, store, credentials["principal:actor-bot-a"], issueRequest(
		"grant-recall-only", "actor-bot-a", v1alpha1.AudiencePrivate, []v1alpha1.Operation{v1alpha1.OperationRecall},
	))
	revoked := mustIssue(t, store, credentials["principal:actor-bot-a"], issueRequest("grant-revoked", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations))
	if err := store.RevokeGrant(t.Context(), "grant-revoked"); err != nil {
		t.Fatal(err)
	}
	injectedToken := "cap-private-shared-injected"
	injectImpossiblePrivateSharedCapability(t, store, injectedToken, now)
	reads := func(spaceID v1alpha1.SpaceID) uint64 {
		switch spaceID {
		case "space-bot-a":
			return privateReads.Load()
		case "space-shared":
			return sharedReads.Load()
		default:
			return 0
		}
	}
	return conformance.Fixture{
		Service:            store,
		BotAPrivate:        callAuth(botA, "actor-bot-a", v1alpha1.AudiencePrivate),
		BotAPrivateRenewed: callAuth(botARenewed, "actor-bot-a", v1alpha1.AudiencePrivate),
		BotAPrivateLabeled: callAuth(botALabeled, "actor-bot-a", v1alpha1.AudiencePrivate),
		BotAPrivateOther:   callAuth(botAOther, "actor-bot-a", v1alpha1.AudiencePrivate),
		BotBPrivate:        callAuth(botB, "actor-bot-b", v1alpha1.AudiencePrivate),
		SharedA:            callAuth(sharedA, "actor-shared-a", v1alpha1.AudienceShared),
		SharedB:            callAuth(sharedB, "actor-shared-b", v1alpha1.AudienceShared),
		RecallOnly:         callAuth(recallOnly, "actor-bot-a", v1alpha1.AudiencePrivate),
		Expired:            callAuth(expired, "actor-bot-a", v1alpha1.AudiencePrivate),
		Revoked:            callAuth(revoked, "actor-bot-a", v1alpha1.AudiencePrivate),
		PrivateSharedWrite: v1alpha1.CallAuthorization{Capability: v1alpha1.CapabilityToken(injectedToken), ActorRef: "actor-bot-a", Audience: v1alpha1.AudiencePrivate},
		BotAPrivateSpace:   "space-bot-a",
		BotBPrivateSpace:   "space-bot-b",
		SharedSpace:        "space-shared",
		CandidateReads:     reads,
		SetAvailable: func(available bool) {
			if !available {
				_ = store.Close()
			}
		},
	}
}

func newGoldenStore(t testing.TB, dataDir string, clock func() time.Time) (*Store, v1alpha1.CallAuthorization) {
	t.Helper()
	return newGoldenStoreWithOptions(t, Options{DataDir: dataDir, Clock: clock})
}

func newGoldenStoreWithOptions(t testing.TB, options Options) (*Store, v1alpha1.CallAuthorization) {
	t.Helper()
	store, credentials := bootstrapFixture(t, options)
	capability := mustIssue(t, store, credentials["principal:actor-bot-a"], issueRequest(
		"grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate,
		[]v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus},
	))
	return store, callAuth(capability, "actor-bot-a", v1alpha1.AudiencePrivate)
}

func bootstrapFixture(t testing.TB, options Options) (*Store, map[string]string) {
	t.Helper()
	if options.Clock == nil {
		options.Clock = time.Now
	}
	store, err := Open(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	now := options.Clock()
	operations := []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus}
	grant := func(id, principal, actor string, view v1alpha1.ViewID, audience v1alpha1.Audience, allowed []v1alpha1.Operation, expires time.Time) Grant {
		return Grant{ID: v1alpha1.GrantID(id), PrincipalRef: principal, ActorRef: actor, ViewRef: view,
			AllowedOperations: allowed, AllowedAudiences: []v1alpha1.Audience{audience}, ExpiresAt: expires, Version: 1}
	}
	response, err := store.Bootstrap(t.Context(), BootstrapRequest{
		Realms: []Realm{{ID: "realm-default"}},
		Identities: []Identity{
			{ID: "identity-bot-a", RealmID: "realm-default"},
			{ID: "identity-bot-b", RealmID: "realm-default"},
		},
		Spaces: []Space{
			{ID: "space-shared", RealmID: "realm-default", Class: v1alpha1.SpaceClassShared},
			{ID: "space-bot-a", RealmID: "realm-default", IdentityID: "identity-bot-a", Class: v1alpha1.SpaceClassPrivate},
			{ID: "space-bot-b", RealmID: "realm-default", IdentityID: "identity-bot-b", Class: v1alpha1.SpaceClassPrivate},
		},
		Views: []ViewDefinition{
			{ID: "view-bot-a-private", RealmID: "realm-default", ReadSpaceIDs: []v1alpha1.SpaceID{"space-shared", "space-bot-a"}, WriteSpaceID: "space-bot-a", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1},
			{ID: "view-bot-b-private", RealmID: "realm-default", ReadSpaceIDs: []v1alpha1.SpaceID{"space-shared", "space-bot-b"}, WriteSpaceID: "space-bot-b", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1},
			{ID: "view-shared", RealmID: "realm-default", ReadSpaceIDs: []v1alpha1.SpaceID{"space-shared"}, WriteSpaceID: "space-shared", MaxDisclosureClass: v1alpha1.SpaceClassShared, Version: 1},
		},
		Grants: []Grant{
			grant("grant-expired", "principal:actor-bot-a", "actor-bot-a", "view-bot-a-private", v1alpha1.AudiencePrivate, operations, now.Add(time.Hour)),
			grant("grant-bot-a", "principal:actor-bot-a", "actor-bot-a", "view-bot-a-private", v1alpha1.AudiencePrivate, operations, now.Add(26*time.Hour)),
			grant("grant-bot-b", "principal:actor-bot-b", "actor-bot-b", "view-bot-b-private", v1alpha1.AudiencePrivate, operations, now.Add(26*time.Hour)),
			grant("grant-shared-a", "principal:actor-shared-a", "actor-shared-a", "view-shared", v1alpha1.AudienceShared, operations, now.Add(26*time.Hour)),
			grant("grant-shared-b", "principal:actor-shared-b", "actor-shared-b", "view-shared", v1alpha1.AudienceShared, operations, now.Add(26*time.Hour)),
			grant("grant-recall-only", "principal:actor-bot-a", "actor-bot-a", "view-bot-a-private", v1alpha1.AudiencePrivate, []v1alpha1.Operation{v1alpha1.OperationRecall}, now.Add(26*time.Hour)),
			grant("grant-revoked", "principal:actor-bot-a", "actor-bot-a", "view-bot-a-private", v1alpha1.AudiencePrivate, operations, now.Add(26*time.Hour)),
		},
		IssuerPrincipals: []string{"principal:actor-bot-a", "principal:actor-bot-b", "principal:actor-shared-a", "principal:actor-shared-b"},
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return store, response.IssuerCredentials
}

func issueRequest(grantID v1alpha1.GrantID, actor string, audience v1alpha1.Audience, operations []v1alpha1.Operation) IssueCapabilityRequest {
	return IssueCapabilityRequest{GrantRef: grantID, ActorRef: actor, Audience: audience, Operations: operations, TTL: time.Hour}
}

func mustIssue(t testing.TB, store *Store, credential string, request IssueCapabilityRequest) RuntimeCapability {
	t.Helper()
	request.Authorization = IssuerAuthorization{PrincipalRef: "principal:" + request.ActorRef, Credential: credential}
	capability, err := store.IssueCapability(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return capability
}

func callAuth(capability RuntimeCapability, actor string, audience v1alpha1.Audience) v1alpha1.CallAuthorization {
	return v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: actor, Audience: audience}
}

func injectImpossiblePrivateSharedCapability(t *testing.T, store *Store, token string, now time.Time) {
	t.Helper()
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO grants(id, principal_ref, actor_ref, view_id, expires_at, revoked, version, created_at) VALUES (?, ?, ?, ?, ?, 0, 1, ?)`, []any{"grant-private-shared-injected", "principal:actor-bot-a", "actor-bot-a", "view-shared", formatTime(now.Add(time.Hour)), formatTime(now)}},
		{`INSERT INTO grant_operations(grant_id, operation) VALUES (?, ?)`, []any{"grant-private-shared-injected", v1alpha1.OperationRemember}},
		{`INSERT INTO grant_audiences(grant_id, audience) VALUES (?, ?)`, []any{"grant-private-shared-injected", v1alpha1.AudiencePrivate}},
		{`INSERT INTO capabilities(token_digest, grant_id, principal_ref, view_version, actor_ref, audience, expires_at, created_at) VALUES (?, ?, ?, 1, ?, ?, ?, ?)`, []any{digestBytes(token), "grant-private-shared-injected", "principal:actor-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, formatTime(now.Add(time.Hour)), formatTime(now)}},
		{`INSERT INTO capability_operations(token_digest, operation) VALUES (?, ?)`, []any{digestBytes(token), v1alpha1.OperationRemember}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func testRecall(query string, token v1alpha1.ConsistencyToken) v1alpha1.RecallRequest {
	return v1alpha1.RecallRequest{Query: query, MinConsistencyToken: token, Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 2000}}
}

func assertHasText(t *testing.T, response v1alpha1.RecallResponse, text string) {
	t.Helper()
	for _, fragment := range response.Fragments {
		if fragment.Text == text {
			return
		}
	}
	t.Fatalf("Recall fragments %+v do not contain %q", response.Fragments, text)
}
