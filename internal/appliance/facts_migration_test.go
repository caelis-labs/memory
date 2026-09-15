package appliance

import (
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// The image is produced by the tagged v0.5.2 source tree. The manifest records
// the producing commit and exact data identities so regeneration is reviewable.
//
//go:embed testdata/v0.5.2/memory.db
var v052MigrationFixtureDB []byte

//go:embed testdata/v0.5.2/manifest.json
var v052MigrationFixtureManifest []byte

// These files are synthetic all-'B' credentials used only to open the copied
// image. They are not production credentials.
//
//go:embed testdata/v0.5.2/management.token.txt
var v052MigrationManagementToken []byte

//go:embed testdata/v0.5.2/steward-worker.token.txt
var v052MigrationStewardToken []byte

const v052MigrationSourceCommit = "51693ff135be8c4149c15117980290aaad6d90da"

type v052MigrationManifest struct {
	SourceCommit            string                       `json:"source_commit"`
	SourceTag               string                       `json:"source_tag"`
	DatabaseSHA256          string                       `json:"database_sha256"`
	SchemaBaseline          string                       `json:"schema_baseline"`
	SchemaVersion           int                          `json:"schema_version"`
	GeneratedAt             string                       `json:"generated_at"`
	StorageGeneration       string                       `json:"storage_generation"`
	LabelSet                []string                     `json:"label_set"`
	LabelSetDigest          string                       `json:"label_set_digest"`
	TestOnlyCapabilityToken string                       `json:"test_only_capability_token"`
	Receipt                 v052MigrationReceiptFixture  `json:"receipt"`
	Semantic                v052MigrationSemanticFixture `json:"semantic"`
	GrantIDs                []string                     `json:"grant_ids"`
	CapabilityGrantID       string                       `json:"capability_grant_id"`
}

type v052MigrationReceiptFixture struct {
	ReceiptID        string               `json:"receipt_id"`
	SpaceID          string               `json:"space_id"`
	Text             string               `json:"text"`
	SourceContext    memory.SourceContext `json:"source_context"`
	OccurredAt       string               `json:"occurred_at"`
	ReceivedAt       string               `json:"received_at"`
	IdempotencyKey   string               `json:"idempotency_key"`
	RequestDigest    string               `json:"request_digest"`
	ConsistencyToken string               `json:"consistency_token"`
	CommitSequence   int64                `json:"commit_sequence"`
}

type v052MigrationSemanticFixture struct {
	RecordID           string   `json:"record_id"`
	Revision           int      `json:"revision"`
	SpaceID            string   `json:"space_id"`
	Kind               string   `json:"kind"`
	Status             string   `json:"status"`
	Text               string   `json:"text"`
	Operation          string   `json:"operation"`
	JobID              string   `json:"job_id"`
	CreatedAt          string   `json:"created_at"`
	EvidenceReceiptIDs []string `json:"evidence_receipt_ids"`
}

func v052Manifest(t *testing.T) v052MigrationManifest {
	t.Helper()
	var m v052MigrationManifest
	if err := json.Unmarshal(v052MigrationFixtureManifest, &m); err != nil {
		t.Fatalf("decode v0.5.2 manifest: %v", err)
	}
	if m.SourceCommit != v052MigrationSourceCommit || m.SourceTag != "v0.5.2" || m.SchemaVersion != 1 || m.SchemaBaseline != schemaBaselineID {
		t.Fatalf("fixture source/schema = %s/%s %s/%d", m.SourceCommit, m.SourceTag, m.SchemaBaseline, m.SchemaVersion)
	}
	sum := sha256.Sum256(v052MigrationFixtureDB)
	if got := hex.EncodeToString(sum[:]); got != m.DatabaseSHA256 {
		t.Fatalf("fixture SHA256 = %s, manifest = %s", got, m.DatabaseSHA256)
	}
	if len(m.LabelSet) == 0 || m.LabelSetDigest == emptyLabelSetDigest || m.TestOnlyCapabilityToken == "" {
		t.Fatal("fixture manifest does not exercise labeled authority")
	}
	return m
}

func copyV052Fixture(t *testing.T) (string, v052MigrationManifest) {
	t.Helper()
	m := v052Manifest(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, DatabaseFilename), v052MigrationFixtureDB, 0o600); err != nil {
		t.Fatalf("copy v0.5.2 fixture: %v", err)
	}
	// v0.5.2 stores the management digest in SQLite. Its owner-only files are
	// intentionally omitted from the repository, so materialize deterministic
	// test-only credentials only in this disposable copy.
	for _, token := range []struct {
		name string
		data []byte
	}{
		{ManagementCredentialFile, v052MigrationManagementToken},
		{StewardWorkerCredentialFile, v052MigrationStewardToken},
	} {
		if string(token.data) != m.TestOnlyCapabilityToken+"\n" {
			t.Fatalf("synthetic %s does not match manifest token", token.name)
		}
		if err := os.WriteFile(filepath.Join(dir, token.name), token.data, 0o600); err != nil {
			t.Fatalf("materialize test-only %s: %v", token.name, err)
		}
	}
	return dir, m
}

func openV052DB(t *testing.T, dir string, immutable bool) *sql.DB {
	t.Helper()
	path := filepath.Join(dir, DatabaseFilename)
	dsn := path
	if immutable {
		dsn = (&url.URL{Scheme: "file", Path: path, RawQuery: "immutable=true"}).String()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open fixture database: %v", err)
	}
	return db
}

// legacyState intentionally selects only columns present in v0.5.2. Comparing
// this canonical row dump before/after catches changed IDs, text, source data,
// immutable timestamps, cursors, idempotency, semantic evidence, labels, and
// authority. The two mutable Steward schedule fields are canonicalized to their
// instants before comparison because schema 2 fixes their text representation.
func legacyState(t *testing.T, db *sql.DB, m v052MigrationManifest) string {
	t.Helper()
	queries := []struct {
		query string
		args  []any
	}{
		{`SELECT commit_sequence,receipt_id,space_id,text,source_context,occurred_at,received_at,idempotency_key,request_digest,consistency_token,label_set,label_set_digest FROM receipts WHERE receipt_id=?`, []any{m.Receipt.ReceiptID}},
		{`SELECT receipt_id,state,attempts,last_attempt_at,terminal_error_code,semantic_generation FROM receipt_processing WHERE receipt_id=?`, []any{m.Receipt.ReceiptID}},
		{`SELECT token,generation,space_id,commit_sequence,label_set_digest FROM consistency_cursors WHERE token=?`, []any{m.Receipt.ConsistencyToken}},
		{`SELECT job_id,receipt_id,space_id,profile_id,profile_version,state,attempts,available_at,lease_expires_at,lease_token_digest,proposal_digest,result_json,terminal_error_code,created_at,updated_at,label_set,label_set_digest FROM steward_jobs WHERE receipt_id=?`, []any{m.Receipt.ReceiptID}},
		{`SELECT record_id,space_id,label_set,label_set_digest,kind,status,current_revision,invalidated_reason,created_at,updated_at FROM semantic_records WHERE record_id=?`, []any{m.Semantic.RecordID}},
		{`SELECT record_id,revision,space_id,kind,text,operation,job_id,created_at FROM semantic_revisions WHERE record_id=? AND revision=?`, []any{m.Semantic.RecordID, m.Semantic.Revision}},
		{`SELECT record_id,revision,ordinal,receipt_id,space_id FROM semantic_evidence WHERE record_id=? AND revision=? ORDER BY ordinal`, []any{m.Semantic.RecordID, m.Semantic.Revision}},
		{`SELECT id,principal_ref,actor_ref,view_id,expires_at,revoked,version,created_at FROM grants ORDER BY id`, nil},
		{`SELECT lower(hex(token_digest)),grant_id,principal_ref,view_version,actor_ref,audience,expires_at,created_at,label_set,label_set_digest FROM capabilities ORDER BY grant_id`, nil},
		{`SELECT grant_id,operation FROM grant_operations ORDER BY grant_id,operation`, nil},
		{`SELECT grant_id,audience FROM grant_audiences ORDER BY grant_id,audience`, nil},
		{`SELECT key,value FROM metadata WHERE key IN ('storage_generation','steward_execution_mode') ORDER BY key`, nil},
	}
	var dump [][]string
	for _, item := range queries {
		rows, err := db.QueryContext(t.Context(), item.query, item.args...)
		if err != nil {
			t.Fatalf("snapshot query %q: %v", item.query, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatalf("snapshot columns %q: %v", item.query, err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(values))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				_ = rows.Close()
				t.Fatalf("snapshot scan %q: %v", item.query, err)
			}
			row := make([]string, len(values))
			for i, value := range values {
				if bytes, ok := value.([]byte); ok {
					row[i] = "blob:" + hex.EncodeToString(bytes)
					continue
				}
				text := fmt.Sprintf("%v", value)
				if text != "" && text != "<nil>" && (columns[i] == "available_at" || columns[i] == "lease_expires_at") {
					parsed, err := parseTime(text)
					if err != nil {
						_ = rows.Close()
						t.Fatalf("parse scheduler timestamp %q: %v", text, err)
					}
					text = formatScheduleTime(parsed)
				}
				row[i] = text
			}
			dump = append(dump, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("snapshot rows %q: %v", item.query, err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("snapshot close %q: %v", item.query, err)
		}
	}
	encoded, err := json.Marshal(dump)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func hasTable(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func hasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name=?)`, table, column).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func assertForeignKeys(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID, parent, fk any
		if err := rows.Scan(&table, &rowID, &parent, &fk); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("foreign key violation table=%s row=%v parent=%v fk=%v", table, rowID, parent, fk)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func assertMigratedV052(t *testing.T, db *sql.DB, m v052MigrationManifest, before string) {
	t.Helper()
	var count, version int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*),COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&count, &version); err != nil {
		t.Fatal(err)
	}
	if count != 2 || version != CurrentSchemaVersion {
		t.Fatalf("schema ledger = count:%d version:%d", count, version)
	}
	var baseline string
	if err := db.QueryRowContext(t.Context(), `SELECT value FROM metadata WHERE key='schema_baseline'`).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if baseline != factsSchemaBaselineID {
		t.Fatalf("schema baseline = %q", baseline)
	}
	if got := legacyState(t, db, m); got != before {
		t.Fatalf("legacy state changed by migration\nbefore=%s\nafter=%s", before, got)
	}
	for _, column := range []struct{ table, name string }{{"semantic_records", "subject"}, {"semantic_records", "fact_key"}, {"semantic_revisions", "fact_json"}} {
		if !hasColumn(t, db, column.table, column.name) {
			t.Fatalf("migration omitted %s.%s", column.table, column.name)
		}
	}
	var subject, key, factJSON string
	if err := db.QueryRowContext(t.Context(), `SELECT subject,fact_key FROM semantic_records WHERE record_id=?`, m.Semantic.RecordID).Scan(&subject, &key); err != nil {
		t.Fatal(err)
	}
	if subject != "" || key != "" {
		t.Fatalf("legacy semantic row acquired fact metadata subject=%q key=%q", subject, key)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT fact_json FROM semantic_revisions WHERE record_id=? AND revision=?`, m.Semantic.RecordID, m.Semantic.Revision).Scan(&factJSON); err != nil {
		t.Fatal(err)
	}
	if factJSON != "" {
		t.Fatalf("legacy semantic row acquired inferred metadata %q", factJSON)
	}
	if !hasTable(t, db, "evidence_sources") {
		t.Fatal("migration omitted evidence_sources")
	}
	var sourceRows int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM evidence_sources`).Scan(&sourceRows); err != nil {
		t.Fatal(err)
	}
	if sourceRows != 0 {
		t.Fatalf("migration fabricated %d source identities", sourceRows)
	}
	assertForeignKeys(t, db)
	var linked int
	if err := db.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM semantic_evidence e
		JOIN semantic_revisions v ON v.record_id=e.record_id AND v.revision=e.revision AND v.space_id=e.space_id
		JOIN semantic_records r ON r.record_id=v.record_id AND r.space_id=v.space_id
		JOIN receipts p ON p.receipt_id=e.receipt_id AND p.space_id=e.space_id
		WHERE e.record_id=? AND e.revision=?`, m.Semantic.RecordID, m.Semantic.Revision).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != len(m.Semantic.EvidenceReceiptIDs) {
		t.Fatalf("linked semantic evidence = %d, want %d", linked, len(m.Semantic.EvidenceReceiptIDs))
	}
}

func migrationAuth(m v052MigrationManifest) memory.CallAuthorization {
	return memory.CallAuthorization{Capability: memory.CapabilityToken(m.TestOnlyCapabilityToken), ActorRef: "actor:migration", Audience: memory.AudiencePrivate}
}

func migrationRemember(m v052MigrationManifest) memory.RememberRequest {
	occurred, err := time.Parse(time.RFC3339Nano, m.Receipt.OccurredAt)
	if err != nil {
		panic(err)
	}
	return memory.RememberRequest{Text: m.Receipt.Text, SourceContext: m.Receipt.SourceContext, OccurredAt: &occurred, IdempotencyKey: m.Receipt.IdempotencyKey}
}

func TestV052FixtureMigratesPreservingLegacyDataAndUnknownFacts(t *testing.T) {
	dir, m := copyV052Fixture(t)
	beforeDB := openV052DB(t, dir, true)
	before := legacyState(t, beforeDB, m)
	if err := beforeDB.Close(); err != nil {
		t.Fatal(err)
	}
	now, err := time.Parse(time.RFC3339Nano, m.GeneratedAt)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), Options{DataDir: dir, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("Open(v0.5.2 fixture): %v", err)
	}
	defer store.Close()
	assertMigratedV052(t, store.db, m, before)
	auth := migrationAuth(m)
	retry, err := store.Remember(t.Context(), auth, migrationRemember(m))
	if err != nil || !retry.Accepted || !retry.DeduplicatedRetry || string(retry.ReceiptID) != m.Receipt.ReceiptID {
		t.Fatalf("legacy Remember retry = %+v, error=%v", retry, err)
	}
	var labels, labelDigest string
	if err := store.db.QueryRowContext(t.Context(), `SELECT label_set,label_set_digest FROM capabilities WHERE grant_id=?`, m.CapabilityGrantID).Scan(&labels, &labelDigest); err != nil {
		t.Fatal(err)
	}
	if labels != `["workspace:migration"]` || labelDigest != m.LabelSetDigest {
		t.Fatalf("legacy capability labels = %q/%q", labels, labelDigest)
	}
	recalled, err := store.Recall(t.Context(), auth, memory.RecallRequest{Query: "migration", MinConsistencyToken: memory.ConsistencyToken(m.Receipt.ConsistencyToken), Budget: memory.RecallBudget{MaxFragments: 8, MaxBytes: 16 << 10, DeadlineMS: 2_000}})
	if err != nil || recalled.Degraded {
		t.Fatalf("legacy Recall = %+v, error=%v", recalled, err)
	}
	var source, semantic bool
	for _, fragment := range recalled.Fragments {
		if fragment.Text == m.Receipt.Text && fragment.FragmentID == "fragment:"+m.Receipt.ReceiptID && containsMigrationReceipt(fragment.EvidenceRefs, m.Receipt.ReceiptID) {
			source = true
		}
		if fragment.Text == m.Semantic.Text && containsString(fragment.RecordRefs, m.Semantic.RecordID) && containsMigrationReceipt(fragment.EvidenceRefs, m.Receipt.ReceiptID) {
			semantic = true
		}
	}
	if !source || !semantic {
		t.Fatalf("legacy Recall lost source/semantic provenance: %+v", recalled.Fragments)
	}
	factsResponse, err := store.ReadFacts(t.Context(), auth, facts.ReadRequest{Subject: "user:migration", Budget: facts.Budget{MaxFacts: 8, MaxBytes: 16 << 10}})
	if err != nil {
		t.Fatalf("legacy facts read: %v", err)
	}
	if len(factsResponse.Facts) != 0 || factsResponse.Background != "" {
		t.Fatalf("legacy semantic row was inferred as a confirmed fact: %+v", factsResponse)
	}
}

func containsMigrationReceipt(values []memory.ReceiptID, expected string) bool {
	for _, value := range values {
		if string(value) == expected {
			return true
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestFactsMigrationFailureRollsBackAndResumes(t *testing.T) {
	dir, m := copyV052Fixture(t)
	beforeDB := openV052DB(t, dir, true)
	before := legacyState(t, beforeDB, m)
	_ = beforeDB.Close()
	conflict := openV052DB(t, dir, false)
	if _, err := conflict.ExecContext(t.Context(), `CREATE TABLE migration_evidence(marker TEXT) STRICT`); err != nil {
		t.Fatal(err)
	}
	_ = conflict.Close()
	now, err := time.Parse(time.RFC3339Nano, m.GeneratedAt)
	if err != nil {
		t.Fatal(err)
	}
	if store, err := Open(t.Context(), Options{DataDir: dir, Clock: func() time.Time { return now }}); err == nil {
		_ = store.Close()
		t.Fatal("Open accepted conflicting migration")
	} else if !strings.Contains(err.Error(), "migrat") && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("conflicting migration error = %v", err)
	}
	rolledBack := openV052DB(t, dir, true)
	if got := legacyState(t, rolledBack, m); got != before {
		t.Fatalf("failed migration changed legacy state\nbefore=%s\nafter=%s", before, got)
	}
	var count, version int
	if err := rolledBack.QueryRowContext(t.Context(), `SELECT COUNT(*),COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&count, &version); err != nil {
		t.Fatal(err)
	}
	if count != 1 || version != 1 || hasTable(t, rolledBack, "evidence_sources") || hasColumn(t, rolledBack, "semantic_records", "subject") {
		t.Fatalf("failed migration left partial schema count=%d version=%d", count, version)
	}
	if !hasTable(t, rolledBack, "migration_evidence") {
		t.Fatal("injected migration conflict was not preserved by rollback")
	}
	_ = rolledBack.Close()
	cleanup := openV052DB(t, dir, false)
	if _, err := cleanup.ExecContext(t.Context(), `DROP TABLE migration_evidence`); err != nil {
		t.Fatal(err)
	}
	_ = cleanup.Close()
	resumed, err := Open(t.Context(), Options{DataDir: dir, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("resume migration: %v", err)
	}
	defer resumed.Close()
	assertMigratedV052(t, resumed.db, m, before)
}

func TestUnsupportedSchemaMarkerIsRejectedWithoutMigration(t *testing.T) {
	dir, _ := copyV052Fixture(t)
	db := openV052DB(t, dir, false)
	if _, err := db.ExecContext(t.Context(), `UPDATE metadata SET value='memory-v0.6.99' WHERE key='schema_baseline'`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := Open(t.Context(), Options{DataDir: dir}); err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("unsupported schema Open error = %v", err)
	}
}

// frozenV052SchemaGuard mirrors the v0.5.2 ledger check: it accepts exactly one
// current-version row. An upgraded image therefore cannot be opened by v0.5.2.
func frozenV052SchemaGuard(db *sql.DB) bool {
	var count, version int
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&count, &version); err != nil || count != 1 || version != 1 {
		return false
	}
	var baseline string
	if err := db.QueryRow(`SELECT value FROM metadata WHERE key='schema_baseline'`).Scan(&baseline); err != nil {
		return false
	}
	return baseline == schemaBaselineID || baseline == preGASchemaBaselineID
}

func TestUpgradedDatabaseIsRejectedByFrozenV052SchemaGuard(t *testing.T) {
	dir, m := copyV052Fixture(t)
	legacy := openV052DB(t, dir, true)
	if !frozenV052SchemaGuard(legacy) {
		t.Fatal("frozen v0.5.2 guard rejected its own fixture")
	}
	_ = legacy.Close()
	now, err := time.Parse(time.RFC3339Nano, m.GeneratedAt)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), Options{DataDir: dir, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	upgraded := openV052DB(t, dir, true)
	defer upgraded.Close()
	if frozenV052SchemaGuard(upgraded) {
		t.Fatal("frozen v0.5.2 guard accepted upgraded schema-2 database")
	}
}
