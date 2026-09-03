package appliance

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestFreshSchemaUsesOneCurrentBaseline(t *testing.T) {
	store, err := Open(t.Context(), Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var count, version int
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT COUNT(*), MAX(version) FROM schema_migrations`).Scan(&count, &version); err != nil {
		t.Fatal(err)
	}
	if count != 1 || version != CurrentSchemaVersion {
		t.Fatalf("schema ledger = count:%d version:%d", count, version)
	}
	var baseline string
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT value FROM metadata WHERE key = 'schema_baseline'`).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if baseline != schemaBaselineID {
		t.Fatalf("schema baseline = %q, want %q", baseline, schemaBaselineID)
	}
	for _, table := range []string{
		"receipt_tombstones", "receipt_corrections", "management_effects",
		"steward_profiles", "steward_jobs", "semantic_records", "semantic_revisions",
		"semantic_evidence", "space_lexicons", "lexicon_terms",
	} {
		var exists bool
		if err := store.db.QueryRowContext(t.Context(),
			`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("schema baseline omitted %s", table)
		}
	}
	for table, columns := range map[string][]string{
		"capabilities":     {"label_set", "label_set_digest"},
		"receipts":         {"label_set", "label_set_digest"},
		"steward_jobs":     {"label_set", "label_set_digest"},
		"semantic_records": {"label_set", "label_set_digest"},
		"steward_profiles": {"system_prompt"},
	} {
		for _, column := range columns {
			var present bool
			if err := store.db.QueryRowContext(t.Context(),
				`SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name = ?)`, table, column).Scan(&present); err != nil {
				t.Fatal(err)
			}
			if !present {
				t.Fatalf("schema baseline omitted %s.%s", table, column)
			}
		}
	}
	for _, obsolete := range []string{"provider_ref", "model"} {
		var present bool
		if err := store.db.QueryRowContext(t.Context(),
			`SELECT EXISTS(SELECT 1 FROM pragma_table_info('steward_profiles') WHERE name = ?)`, obsolete).Scan(&present); err != nil {
			t.Fatal(err)
		}
		if present {
			t.Fatalf("unreleased compatibility column steward_profiles.%s remains", obsolete)
		}
	}
}

func TestHistoricalUnreleasedSchemaRequiresDevelopmentDatabaseRebuild(t *testing.T) {
	dataDir := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(dataDir, DatabaseFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(),
		`INSERT INTO schema_migrations(version, applied_at) VALUES (8, ?)`, formatTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Open(t.Context(), Options{DataDir: dataDir})
	if err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("Open historical development schema error = %v", err)
	}
}

func TestFinalPrereleaseBaselinePromotesWithoutDataLoss(t *testing.T) {
	dataDir := t.TempDir()
	store, auth := newGoldenStore(t, dataDir, time.Now)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the GA schema promotion preserves accepted evidence", IdempotencyKey: "ga-schema-promotion",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`UPDATE metadata SET value = ? WHERE key = 'schema_baseline'`, preGASchemaBaselineID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var baseline string
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT value FROM metadata WHERE key = 'schema_baseline'`).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if baseline != schemaBaselineID {
		t.Fatalf("promoted schema baseline = %q, want %q", baseline, schemaBaselineID)
	}
	response, err := store.Recall(t.Context(), auth, testRecall("schema promotion", receipt.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, response, "the GA schema promotion preserves accepted evidence")
}

func TestSchemaBaselineFailureRollsBackAllDomainTables(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "baseline-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(t.Context(), `CREATE TABLE metadata(conflict TEXT) STRICT`); err != nil {
		t.Fatal(err)
	}
	if err := initializeSchema(t.Context(), database, time.Now()); err == nil {
		t.Fatal("schema baseline succeeded despite a conflicting table")
	}
	for _, table := range []string{"realms", "receipts", "steward_profiles", "semantic_records"} {
		var exists bool
		if err := database.QueryRowContext(t.Context(),
			`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("failed baseline left %s behind", table)
		}
	}
	var recorded int
	if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatalf("failed baseline recorded %d schema rows", recorded)
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
	var exported bytes.Buffer
	if err := store.Export(t.Context(), managementv1alpha1.ExportRequest{IncludeCorrected: true, IncludeDeleted: true}, &exported); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(exported.Bytes(), []byte(rememberRequest.Text)) {
		t.Fatal("management export exposed deleted receipt text")
	}
	decoder := json.NewDecoder(&exported)
	var header managementv1alpha1.ExportRecord
	if err := decoder.Decode(&header); err != nil {
		t.Fatal(err)
	}
	if header.Kind != managementv1alpha1.ExportRecordHeader || header.Format != managementv1alpha1.ExportFormat {
		t.Fatalf("export header = %+v", header)
	}
	foundTombstone := false
	for {
		var record managementv1alpha1.ExportRecord
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if record.Tombstone != nil && record.Tombstone.ReceiptID == remembered.ReceiptID {
			foundTombstone = true
		}
	}
	if !foundTombstone {
		t.Fatalf("export omitted deletion tombstone: %s", exported.Bytes())
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
