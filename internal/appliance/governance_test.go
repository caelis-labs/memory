package appliance

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestSchemaOneMigratesTransactionallyToGovernanceSchema(t *testing.T) {
	dataDir := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(dataDir, DatabaseFilename))
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateTo(t.Context(), database, time.Now(), 1); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRowContext(t.Context(), `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d, want 2", version)
	}
	for _, table := range []string{"receipt_tombstones", "receipt_corrections", "management_effects"} {
		var exists bool
		if err := store.db.QueryRowContext(t.Context(),
			`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("migration did not create %s", table)
		}
	}
}

func TestGovernanceMigrationFailureRollsBackPartialDDL(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := migrateTo(t.Context(), database, time.Now(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `CREATE TABLE management_effects(conflict TEXT) STRICT`); err != nil {
		t.Fatal(err)
	}
	if err := migrateTo(t.Context(), database, time.Now(), 2); err == nil {
		t.Fatal("governance migration succeeded despite a conflicting later table")
	}
	for _, table := range []string{"receipt_tombstones", "receipt_corrections"} {
		var exists bool
		if err := database.QueryRowContext(t.Context(),
			`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("failed governance migration left %s behind", table)
		}
	}
	var version int
	if err := database.QueryRowContext(t.Context(), `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("failed migration recorded schema version %d, want 1", version)
	}
	var immutableDeleteExists bool
	if err := database.QueryRowContext(t.Context(),
		`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'trigger' AND name = 'receipts_immutable_delete')`).Scan(&immutableDeleteExists); err != nil {
		t.Fatal(err)
	}
	if !immutableDeleteExists {
		t.Fatal("failed governance migration removed the v1 delete guard")
	}
}

func TestReceiptCorrectionIsAppendOnlyIdempotentAndRebuildSafe(t *testing.T) {
	dataDir := t.TempDir()
	store, auth := newGoldenStore(t, dataDir, time.Now)
	original, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "service listens on obsolete port 80", IdempotencyKey: "port-fact",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := managementv1alpha1.CorrectReceiptRequest{
		ReceiptID: original.ReceiptID, ReplacementText: "service listens on corrected port 443",
		Reason: "operator verified configuration", IdempotencyKey: "correct-port-1",
	}
	corrected, err := store.CorrectReceipt(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := store.CorrectReceipt(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !retry.DeduplicatedRetry || retry.ReplacementReceiptID != corrected.ReplacementReceiptID {
		t.Fatalf("correction retry = %+v, first = %+v", retry, corrected)
	}
	conflict := request
	conflict.ReplacementText = "conflicting replacement"
	if _, err := store.CorrectReceipt(t.Context(), conflict); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeConflict) {
		t.Fatalf("changed correction retry error = %v, want conflict", err)
	}

	oldRecall, err := store.Recall(t.Context(), auth, testRecall("obsolete", corrected.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if len(oldRecall.Fragments) != 0 {
		t.Fatalf("corrected original remained Recallable: %+v", oldRecall.Fragments)
	}
	newRecall, err := store.Recall(t.Context(), auth, testRecall("corrected", corrected.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, newRecall, request.ReplacementText)
	if len(newRecall.Fragments) != 1 || newRecall.Fragments[0].EvidenceRefs[0] != corrected.ReplacementReceiptID {
		t.Fatalf("replacement provenance = %+v", newRecall.Fragments)
	}

	originalTrace, err := store.TraceReceipt(t.Context(), managementv1alpha1.TraceReceiptRequest{ReceiptID: original.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if originalTrace.State != managementv1alpha1.ReceiptStateCorrected || originalTrace.Receipt == nil || originalTrace.Receipt.CorrectedBy != corrected.ReplacementReceiptID {
		t.Fatalf("original trace = %+v", originalTrace)
	}
	replacementTrace, err := store.TraceReceipt(t.Context(), managementv1alpha1.TraceReceiptRequest{ReceiptID: corrected.ReplacementReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if replacementTrace.Receipt == nil || replacementTrace.Receipt.CorrectionOf != original.ReceiptID {
		t.Fatalf("replacement trace = %+v", replacementTrace)
	}
	activeSearch, err := store.SearchReceipts(t.Context(), managementv1alpha1.SearchReceiptsRequest{Query: "obsolete", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(activeSearch.Receipts) != 0 {
		t.Fatalf("default search exposed corrected original: %+v", activeSearch.Receipts)
	}
	auditSearch, err := store.SearchReceipts(t.Context(), managementv1alpha1.SearchReceiptsRequest{Query: "obsolete", Limit: 10, IncludeCorrected: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(auditSearch.Receipts) != 1 || auditSearch.Receipts[0].ReceiptID != original.ReceiptID {
		t.Fatalf("audit search = %+v", auditSearch.Receipts)
	}

	if err := store.RebuildFTS(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	oldRecall, err = restarted.Recall(t.Context(), auth, testRecall("obsolete", corrected.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if len(oldRecall.Fragments) != 0 {
		t.Fatalf("corrected original reappeared after rebuild/restart: %+v", oldRecall.Fragments)
	}
	newRecall, err = restarted.Recall(t.Context(), auth, testRecall("corrected", corrected.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, newRecall, request.ReplacementText)
}

func TestReceiptDeletionCannotReappearOrResurrect(t *testing.T) {
	dataDir := t.TempDir()
	store, auth := newGoldenStore(t, dataDir, time.Now)
	rememberRequest := v1alpha1.RememberRequest{
		Text: "governance erasure sentinel", IdempotencyKey: "erasure-effect",
	}
	remembered, err := store.Remember(t.Context(), auth, rememberRequest)
	if err != nil {
		t.Fatal(err)
	}
	deleteRequest := managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: remembered.ReceiptID, Reason: "verified erasure request", IdempotencyKey: "delete-erasure-1",
	}
	deleted, err := store.DeleteReceipt(t.Context(), deleteRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Deleted || deleted.TombstoneID == "" || deleted.SessionCopyBoundary != sessionCopyBoundary {
		t.Fatalf("delete response = %+v", deleted)
	}
	retry, err := store.DeleteReceipt(t.Context(), deleteRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !retry.DeduplicatedRetry || retry.TombstoneID != deleted.TombstoneID {
		t.Fatalf("delete retry = %+v, first = %+v", retry, deleted)
	}
	if _, err := store.Remember(t.Context(), auth, rememberRequest); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeConflict) {
		t.Fatalf("deleted Remember retry error = %v, want conflict", err)
	}
	trace, err := store.TraceReceipt(t.Context(), managementv1alpha1.TraceReceiptRequest{ReceiptID: remembered.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if trace.State != managementv1alpha1.ReceiptStateDeleted || trace.Receipt != nil || trace.Tombstone == nil || trace.Tombstone.Reason != deleteRequest.Reason {
		t.Fatalf("deleted trace = %+v", trace)
	}
	var rawCount int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM receipts WHERE text = ?`, rememberRequest.Text).Scan(&rawCount); err != nil {
		t.Fatal(err)
	}
	if rawCount != 0 {
		t.Fatalf("deleted raw receipt count = %d, want 0", rawCount)
	}
	search, err := store.SearchReceipts(t.Context(), managementv1alpha1.SearchReceiptsRequest{Query: "erasure", Limit: 10, IncludeCorrected: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Receipts) != 0 {
		t.Fatalf("management search returned deleted text: %+v", search.Receipts)
	}
	if err := store.RebuildFTS(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	recalled, err := restarted.Recall(t.Context(), auth, testRecall("erasure", remembered.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled.Fragments) != 0 {
		t.Fatalf("deleted receipt reappeared after rebuild/restart: %+v", recalled.Fragments)
	}
	if _, err := restarted.Remember(t.Context(), auth, rememberRequest); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeConflict) {
		t.Fatalf("deleted Remember retry after restart error = %v, want conflict", err)
	}
}
