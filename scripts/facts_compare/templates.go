package main

// The comparison runner injects one shared test plus one side adapter into a
// temporary copy of each tree. The shared test uses only API surface that is
// byte-identical between the exact v0.5.2 baseline and the current tree, so the
// same fixture and the same scripted deterministic proposal bytes run on both
// sides with no model, no credentials, and no network.
//
// The side adapter is the only place that may reference side-specific fields:
// the current tree adds trusted host sources and subject/fact-key hints to
// ReceiptInput, which the baseline ReceiptInput does not have.

const factsCompareSharedTestFile = "facts_compare_generated_test.go"

const factsCompareSideTestFile = "facts_compare_side_generated_test.go"

const factsCompareSharedTest = `package appliance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

type factsCompareFixture struct {
	UnrelatedRecords int                    ` + "`" + `json:"unrelated_records"` + "`" + `
	OldReceiptText   string                 ` + "`" + `json:"old_receipt_text"` + "`" + `
	OldRecordKind    string                 ` + "`" + `json:"old_record_kind"` + "`" + `
	OldRecordText    string                 ` + "`" + `json:"old_record_text"` + "`" + `
	UnrelatedKind    string                 ` + "`" + `json:"unrelated_kind"` + "`" + `
	UnrelatedText    string                 ` + "`" + `json:"unrelated_text"` + "`" + `
	ProbeReceiptText string                 ` + "`" + `json:"probe_receipt_text"` + "`" + `
	Profile          factsCompareProfile    ` + "`" + `json:"profile"` + "`" + `
	RecallQueries    []factsCompareQuery    ` + "`" + `json:"recall_queries"` + "`" + `
	Supplemental     factsCompareSupplement ` + "`" + `json:"supplemental"` + "`" + `
}

type factsCompareProfile struct {
	ProfileID         string ` + "`" + `json:"profile_id"` + "`" + `
	Version           uint64 ` + "`" + `json:"version"` + "`" + `
	SystemPrompt      string ` + "`" + `json:"system_prompt"` + "`" + `
	MaxContextRecords int    ` + "`" + `json:"max_context_records"` + "`" + `
	MaxInputBytes     int    ` + "`" + `json:"max_input_bytes"` + "`" + `
	MaxOutputBytes    int    ` + "`" + `json:"max_output_bytes"` + "`" + `
}

type factsCompareQuery struct {
	Query         string ` + "`" + `json:"query"` + "`" + `
	ExpectedText  string ` + "`" + `json:"expected_text"` + "`" + `
	SecondaryText string ` + "`" + `json:"secondary_text"` + "`" + `
}

type factsCompareSupplement struct {
	Subject           string ` + "`" + `json:"subject"` + "`" + `
	FactKey           string ` + "`" + `json:"fact_key"` + "`" + `
	FactText          string ` + "`" + `json:"fact_text"` + "`" + `
	ProbeReceiptText  string ` + "`" + `json:"probe_receipt_text"` + "`" + `
	UnrelatedRecords  int    ` + "`" + `json:"unrelated_records"` + "`" + `
	UnrelatedTemplate string ` + "`" + `json:"unrelated_template"` + "`" + `
}

type factsCompareRecallObservation struct {
	Query             string   ` + "`" + `json:"query"` + "`" + `
	ExpectedPresent   bool     ` + "`" + `json:"expected_present"` + "`" + `
	ExpectedRank      int      ` + "`" + `json:"expected_rank"` + "`" + `
	SecondaryPresent  bool     ` + "`" + `json:"secondary_present"` + "`" + `
	SecondaryRank     int      ` + "`" + `json:"secondary_rank"` + "`" + `
	Fragments         []string ` + "`" + `json:"fragments"` + "`" + `
	ConsistencyToken  string   ` + "`" + `json:"consistency_token,omitempty"` + "`" + `
	RetrievalOnly     bool     ` + "`" + `json:"retrieval_only"` + "`" + `
	AnswersPreference bool     ` + "`" + `json:"answers_preference"` + "`" + `
}

type factsCompareSideResult struct {
	Side                  string                            ` + "`" + `json:"side"` + "`" + `
	ProfileID             string                            ` + "`" + `json:"profile_id"` + "`" + `
	ProfileVersion        uint64                            ` + "`" + `json:"profile_version"` + "`" + `
	MaxContextRecords     int                               ` + "`" + `json:"max_context_records"` + "`" + `
	OldRecordIncluded     bool                              ` + "`" + `json:"old_record_included"` + "`" + `
	OldRecordRank         int                               ` + "`" + `json:"old_record_rank"` + "`" + `
	RecordsCount          int                               ` + "`" + `json:"records_count"` + "`" + `
	RecordsSHA256         string                            ` + "`" + `json:"records_sha256"` + "`" + `
	OldRecordIDHash       string                            ` + "`" + `json:"old_record_id_hash"` + "`" + `
	ReceiptAttribution    map[string]any                    ` + "`" + `json:"receipt_attribution"` + "`" + `
	ScriptedOutputsSHA256 string                            ` + "`" + `json:"scripted_outputs_sha256"` + "`" + `
	NoStewardRecall       []factsCompareRecallObservation   ` + "`" + `json:"no_steward_recall"` + "`" + `
	NoStewardJobs         int                               ` + "`" + `json:"no_steward_jobs"` + "`" + `
	NoStewardRecords      int                               ` + "`" + `json:"no_steward_records"` + "`" + `
	StewardRecall         []factsCompareRecallObservation   ` + "`" + `json:"steward_recall"` + "`" + `
	Supplemental          map[string]any                    ` + "`" + `json:"supplemental"` + "`" + `
}

func TestFactsCompareStruct(t *testing.T) {
	fixtureRaw, err := os.ReadFile(os.Getenv("FACTS_COMPARE_FIXTURE"))
	if err != nil {
		t.Fatalf("read comparison fixture: %v", err)
	}
	var fixture factsCompareFixture
	if err := json.Unmarshal(fixtureRaw, &fixture); err != nil {
		t.Fatalf("parse comparison fixture: %v", err)
	}
	result := factsCompareSideResult{
		Side:           os.Getenv("FACTS_COMPARE_SIDE"),
		ProfileID:      fixture.Profile.ProfileID,
		ProfileVersion: fixture.Profile.Version,
		MaxContextRecords: fixture.Profile.MaxContextRecords,
	}

	store, auth := newGoldenStore(t, t.TempDir(), factsCompareClock)
	t.Cleanup(func() { _ = store.Close() })
	factsCompareBindProfile(t, store, fixture.Profile)

	// Fair path: identical legacy Remember plus identical scripted proposal
	// bytes on both sides. The baseline has no facts/evidence API, so using
	// the legacy path for both sides avoids an ingestion advantage.
	oldReceipt := factsCompareRemember(t, store, auth, fixture.OldReceiptText, "facts-compare-old")
	oldRecord := factsCompareApply(t, store, oldReceipt, fixture.OldRecordKind, fixture.OldRecordText)
	result.OldRecordIDHash = factsCompareSha(string(oldRecord.RecordID))

	scripted := []string{fixture.OldRecordText}
	for index := 0; index < fixture.UnrelatedRecords; index++ {
		text := fmt.Sprintf(fixture.UnrelatedText, index)
		receipt := factsCompareRemember(t, store, auth, text, fmt.Sprintf("facts-compare-unrelated-%04d", index))
		factsCompareApply(t, store, receipt, fixture.UnrelatedKind, text)
		scripted = append(scripted, text)
	}
	scripted = append(scripted, fixture.ProbeReceiptText)
	result.ScriptedOutputsSHA256 = factsCompareSha(factsCompareJoinStrings(scripted))

	probeReceipt := factsCompareRemember(t, store, auth, fixture.ProbeReceiptText, "facts-compare-probe")
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil {
		t.Fatalf("claim probe job: %v", err)
	}
	if !found {
		t.Fatal("probe job was not available")
	}
	if work.Request.Receipt.ReceiptID != probeReceipt {
		t.Fatalf("claimed receipt %q, want the probe receipt %q", work.Request.Receipt.ReceiptID, probeReceipt)
	}
	result.RecordsCount = len(work.Request.Records)
	recordIDs := make([]string, 0, len(work.Request.Records))
	for index, record := range work.Request.Records {
		recordIDs = append(recordIDs, string(record.RecordID))
		if record.RecordID == oldRecord.RecordID {
			result.OldRecordIncluded = true
			result.OldRecordRank = index + 1
		}
	}
	result.RecordsSHA256 = factsCompareSha(factsCompareJoinStrings(recordIDs))
	result.ReceiptAttribution = factsCompareReceiptAttribution(work)
	// The scripted model output for the inspected probe is a deterministic
	// IGNORE, so the lease is released instead of lingering and being reclaimed
	// by a later claim.
	if _, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationIgnore,
	}); err != nil {
		t.Fatalf("ignore inspected probe job: %v", err)
	}

	// Retrieval-only evidence arms. A returned fragment is never proof that the
	// preference was answered or adopted; the false-adoption risk is that an
	// older preference fragment still ranks after a newer update.
	//
	// B0 is receipt-only and must not involve Steward at all, so it runs on a
	// separate fresh Store with no profile binding and no Record.
	result.NoStewardRecall, result.NoStewardJobs, result.NoStewardRecords = factsCompareNoStewardRecall(t, fixture)

	// B1 is the Steward-organized evidence Recall on this side: the old coffee
	// Record exists, so fragments include it alongside the raw receipts.
	result.StewardRecall = factsCompareRecallAll(t, store, auth, fixture.RecallQueries)

	result.Supplemental = factsCompareSupplemental(t, store, auth, fixture.Supplemental)

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal comparison result: %v", err)
	}
	if err := os.WriteFile(os.Getenv("FACTS_COMPARE_RESULT"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write comparison result: %v", err)
	}
}

// factsCompareClock is a deterministic whole-second monotonic clock shared by
// the fixture work on both sides. Whole seconds keep Steward job availability
// string comparisons unambiguous; strictly increasing seconds keep Record
// updated_at ordering, which the baseline recency-only context relies on.
func factsCompareClock() time.Time {
	base := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	return base.Add(time.Duration(factsCompareClockTicks.Add(1)) * time.Second)
}

var factsCompareClockTicks atomic.Int64

func factsCompareBindProfile(t *testing.T, store *Store, profile factsCompareProfile) {
	t.Helper()
	if _, err := store.PutStewardProfile(t.Context(), managementv1alpha1.PutStewardProfileRequest{
		Profile: stewardv1alpha1.ProfileSpec{
			ProfileID: stewardv1alpha1.ProfileID(profile.ProfileID), Version: profile.Version,
			SystemPrompt: profile.SystemPrompt, MaxContextRecords: profile.MaxContextRecords,
			MaxInputBytes: profile.MaxInputBytes, MaxOutputBytes: profile.MaxOutputBytes,
		},
	}); err != nil {
		t.Fatalf("put comparison profile: %v", err)
	}
	if _, err := store.BindStewardProfile(t.Context(), managementv1alpha1.BindStewardProfileRequest{
		ProfileID: stewardv1alpha1.ProfileID(profile.ProfileID), Version: profile.Version,
		SpaceIDs: []v1alpha1.SpaceID{"space-bot-a"},
	}); err != nil {
		t.Fatalf("bind comparison profile: %v", err)
	}
}

func factsCompareRemember(t *testing.T, store *Store, auth v1alpha1.CallAuthorization, text, key string) v1alpha1.ReceiptID {
	t.Helper()
	remembered, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: text, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("remember %q: %v", key, err)
	}
	return remembered.ReceiptID
}

func factsCompareApply(t *testing.T, store *Store, receipt v1alpha1.ReceiptID, kind, text string) stewardv1alpha1.ApplyResult {
	t.Helper()
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil {
		t.Fatalf("claim job for %q: %v", receipt, err)
	}
	if !found {
		t.Fatalf("no claimable job for %q: %s", receipt, factsCompareJobState(t, store))
	}
	if work.Request.Receipt.ReceiptID != receipt {
		t.Fatalf("claimed receipt %q, want %q: %s", work.Request.Receipt.ReceiptID, receipt, factsCompareJobState(t, store))
	}
	applied, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation:    stewardv1alpha1.OperationAdd,
		Kind:         kind,
		Text:         text,
		EvidenceRefs: []v1alpha1.ReceiptID{receipt},
	})
	if err != nil {
		t.Fatalf("apply scripted proposal for %q: %v", receipt, err)
	}
	return applied
}

func factsCompareSha(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func factsCompareRecallAll(t *testing.T, store *Store, auth v1alpha1.CallAuthorization, queries []factsCompareQuery) []factsCompareRecallObservation {
	t.Helper()
	var observations []factsCompareRecallObservation
	for _, query := range queries {
		response, err := store.Recall(t.Context(), auth, v1alpha1.RecallRequest{
			Query:  query.Query,
			Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 2000},
		})
		if err != nil {
			t.Fatalf("recall %q: %v", query.Query, err)
		}
		observation := factsCompareRecallObservation{
			Query: query.Query, RetrievalOnly: true, AnswersPreference: false,
			ConsistencyToken: string(response.ConsistencyToken),
		}
		for index, fragment := range response.Fragments {
			observation.Fragments = append(observation.Fragments, fragment.Text)
			if fragment.Text == query.ExpectedText {
				observation.ExpectedPresent = true
				if observation.ExpectedRank == 0 {
					observation.ExpectedRank = index + 1
				}
			}
			if query.SecondaryText != "" && fragment.Text == query.SecondaryText {
				observation.SecondaryPresent = true
				if observation.SecondaryRank == 0 {
					observation.SecondaryRank = index + 1
				}
			}
		}
		observations = append(observations, observation)
	}
	return observations
}

// factsCompareNoStewardRecall is the B0 arm: the same raw receipt fixture with no
// Steward profile binding, so there is no Steward job and no Record. It asserts
// that receipt-only Recall never creates derived state.
func factsCompareNoStewardRecall(t *testing.T, fixture factsCompareFixture) ([]factsCompareRecallObservation, int, int) {
	t.Helper()
	store, auth := newGoldenStore(t, t.TempDir(), factsCompareClock)
	defer func() { _ = store.Close() }()
	if jobs, records := factsCompareCount(t, store, "steward_jobs"), factsCompareCount(t, store, "semantic_records"); jobs != 0 || records != 0 {
		t.Fatalf("no-Steward store must start empty: jobs=%d records=%d", jobs, records)
	}
	factsCompareRemember(t, store, auth, fixture.OldReceiptText, "facts-compare-b0-old")
	for index := 0; index < fixture.UnrelatedRecords; index++ {
		factsCompareRemember(t, store, auth, fmt.Sprintf(fixture.UnrelatedText, index), fmt.Sprintf("facts-compare-b0-unrelated-%04d", index))
	}
	factsCompareRemember(t, store, auth, fixture.ProbeReceiptText, "facts-compare-b0-probe")
	observations := factsCompareRecallAll(t, store, auth, fixture.RecallQueries)
	jobs, records := factsCompareCount(t, store, "steward_jobs"), factsCompareCount(t, store, "semantic_records")
	if jobs != 0 || records != 0 {
		t.Errorf("receipt-only Recall must create no Steward job or Record: jobs=%d records=%d", jobs, records)
	}
	return observations, jobs, records
}

func factsCompareCount(t *testing.T, store *Store, table string) int {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// factsCompareJobState reports steward job states without receipt text so a
// failed claim can be diagnosed from the test output.
func factsCompareJobState(t *testing.T, store *Store) string {
	rows, err := store.db.QueryContext(t.Context(),
		"SELECT state, attempts, available_at, created_at FROM steward_jobs ORDER BY created_at, job_id")
	if err != nil {
		return "job state unavailable: " + err.Error()
	}
	defer rows.Close()
	counts := map[string]int{}
	var observed []string
	for rows.Next() {
		var state, availableAt, createdAt string
		var attempts int
		if err := rows.Scan(&state, &attempts, &availableAt, &createdAt); err != nil {
			return "job state scan failed: " + err.Error()
		}
		counts[state]++
		if len(observed) < 5 {
			observed = append(observed, fmt.Sprintf("%s attempts=%d available_at=%s", state, attempts, availableAt))
		}
	}
	return fmt.Sprintf("counts=%v first=%v", counts, observed)
}

func factsCompareJoinStrings(values []string) string {
	out := ""
	for _, value := range values {
		out += value + "\x00"
	}
	return out
}
`

const factsCompareBaselineSideTest = `package appliance

import (
	"testing"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// factsCompareReceiptAttribution reports only fields the baseline ReceiptInput
// exposes. The exact v0.5.2 baseline has no host source, subject or fact-key
// fields, so those stay empty and are reported as unavailable.
func factsCompareReceiptAttribution(work StewardWork) map[string]any {
	return map[string]any{
		"status":                 "measured",
		"baseline_fields_only":   true,
		"sources_count":          0,
		"subject":                "",
		"fact_key":               "",
		"side_note":              "v0.5.2 ReceiptInput has no host sources, subject or fact key",
	}
}

// factsCompareSupplemental is not applicable to the baseline: the trusted
// source subject/fact-key match requires the facts/evidence API that v0.5.2
// does not have.
func factsCompareSupplemental(t *testing.T, store *Store, auth v1alpha1.CallAuthorization, fixture factsCompareSupplement) map[string]any {
	return map[string]any{
		"status": "not_applicable",
		"reason": "the exact v0.5.2 baseline has no facts/evidence API or trusted host source attribution",
	}
}
`

const factsCompareCurrentSideTest = `package appliance

import (
	"fmt"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// factsCompareReceiptAttribution additionally reports the current tree host
// attribution fields. They are supplemental to the fair comparison because the
// baseline cannot expose them at all.
func factsCompareReceiptAttribution(work StewardWork) map[string]any {
	return map[string]any{
		"status":        "measured",
		"sources_count": len(work.Request.Receipt.Sources),
		"subject":       work.Request.Receipt.Subject,
		"fact_key":      work.Request.Receipt.FactKey,
	}
}

// factsCompareSupplemental exercises the current-only trusted-source match:
// one fact Record with a host subject/fact-key, then many newer unrelated
// Records, then a probe whose trusted Source carries the same subject and key.
// It is structural selection evidence, not extraction or answer quality.
func factsCompareSupplemental(t *testing.T, store *Store, auth v1alpha1.CallAuthorization, fixture factsCompareSupplement) map[string]any {
	out := map[string]any{"status": "measured", "evidence": "trusted source subject/fact-key match is current-only"}
	if fixture.Subject == "" || fixture.FactKey == "" {
		out["status"] = "not_applicable"
		out["reason"] = "fixture did not define a subject and fact key"
		return out
	}
	established, err := store.SubmitEvidence(t.Context(), auth, facts.SubmitEvidenceRequest{
		Source: facts.Source{
			Producer: "facts-compare", EventID: "facts-compare-supplemental-fact", Revision: "r1", Fragment: "f1",
			Subject: fixture.Subject, FactKey: fixture.FactKey, Role: facts.RoleObservation,
		},
		Text:           fixture.FactText,
		IdempotencyKey: "facts-compare-supplemental-fact",
		Mutations: []facts.Mutation{{
			Transition: facts.TransitionEstablish, Subject: fixture.Subject, Key: fixture.FactKey, Text: fixture.FactText,
		}},
	})
	if err != nil || len(established.Facts) != 1 {
		t.Fatalf("supplemental establish = %+v, %v", established, err)
	}
	factRecordID := established.Facts[0].RecordID
	for index := 0; index < fixture.UnrelatedRecords; index++ {
		text := fmt.Sprintf(fixture.UnrelatedTemplate, index)
		receipt := factsCompareRemember(t, store, auth, text, fmt.Sprintf("facts-compare-supplemental-unrelated-%04d", index))
		factsCompareApply(t, store, receipt, "note", text)
	}
	probe, err := store.SubmitEvidence(t.Context(), auth, facts.SubmitEvidenceRequest{
		Source: facts.Source{
			Producer: "facts-compare", EventID: "facts-compare-supplemental-probe", Revision: "r1", Fragment: "f1",
			Subject: fixture.Subject, FactKey: fixture.FactKey, Role: facts.RoleObservation,
		},
		Text:           fixture.ProbeReceiptText,
		IdempotencyKey: "facts-compare-supplemental-probe",
	})
	if err != nil || !probe.Accepted {
		t.Fatalf("supplemental probe = %+v, %v", probe, err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("supplemental claim found=%v err=%v", found, err)
	}
	out["probe_receipt_matched"] = work.Request.Receipt.ReceiptID == probe.ReceiptID
	out["records_count"] = len(work.Request.Records)
	out["relevant_included"] = false
	out["relevant_rank"] = 0
	for index, record := range work.Request.Records {
		if string(record.RecordID) == factRecordID {
			out["relevant_included"] = true
			out["relevant_rank"] = index + 1
		}
	}
	out["receipt_sources_count"] = len(work.Request.Receipt.Sources)
	out["receipt_subject"] = work.Request.Receipt.Subject
	out["receipt_fact_key"] = work.Request.Receipt.FactKey
	return out
}
`
