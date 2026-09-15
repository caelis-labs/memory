package appliance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// Candidate trajectories are machine-expanded from the implementation's own
// controlled vocabulary. They are frozen and labeled not_human_reviewed so a
// reviewer can later replace or confirm them. Any rate measured over them is
// candidate-tier circular evidence and never a human quality claim.

type factsEvalCandidateDoc struct {
	FormatVersion int                          `json:"format_version"`
	Corpus        string                       `json:"corpus"`
	ReviewStatus  string                       `json:"review_status"`
	Note          string                       `json:"note"`
	Generator     string                       `json:"generator"`
	Tiers         []string                     `json:"tiers"`
	Subjects      []string                     `json:"subjects"`
	Templates     []factsEvalCandidateTemplate `json:"templates"`
}

type factsEvalCandidateTemplate struct {
	ID            string   `json:"id"`
	Key           string   `json:"key"`
	Language      string   `json:"language"`
	Establish     string   `json:"establish"`
	Change        string   `json:"change"`
	Aliases       []string `json:"aliases"`
	ExpectCurrent string   `json:"expect_current"`
}

type factsEvalCandidate struct {
	ID            string
	Subject       string
	Key           string
	Language      string
	Establish     string
	Change        string
	Aliases       []string
	ExpectCurrent string
	ReviewStatus  string
	Tiers         []string
}

func loadFactsEvalCandidates(t *testing.T, dir string) []factsEvalCandidate {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "candidates.json"))
	if err != nil {
		t.Fatalf("read facts evaluation candidates: %v", err)
	}
	var doc factsEvalCandidateDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse facts evaluation candidates: %v", err)
	}
	if doc.FormatVersion != 1 || doc.ReviewStatus != "not_human_reviewed" {
		t.Fatalf("facts evaluation candidates header = format %d review %q", doc.FormatVersion, doc.ReviewStatus)
	}
	if doc.Generator != "facts_eval_candidate_matrix_v1" {
		t.Fatalf("facts evaluation candidate generator = %q", doc.Generator)
	}
	if len(doc.Subjects) == 0 || len(doc.Templates) == 0 {
		t.Fatal("facts evaluation candidate matrix is empty")
	}
	var out []factsEvalCandidate
	seen := map[string]bool{}
	for _, template := range doc.Templates {
		if template.Key == "" || template.Establish == "" || template.ExpectCurrent == "" || len(template.Aliases) == 0 {
			t.Fatalf("candidate template %q is incomplete", template.ID)
		}
		for index, subject := range doc.Subjects {
			id := fmt.Sprintf("cand-%s-%d", template.ID, index)
			if seen[id] {
				t.Fatalf("duplicate candidate identity %q", id)
			}
			seen[id] = true
			out = append(out, factsEvalCandidate{
				ID: id, Subject: subject, Key: template.Key, Language: template.Language,
				Establish: template.Establish, Change: template.Change,
				Aliases: append([]string(nil), template.Aliases...), ExpectCurrent: template.ExpectCurrent,
				ReviewStatus: doc.ReviewStatus, Tiers: append([]string(nil), doc.Tiers...),
			})
		}
	}
	return out
}

type factsEvalTierResult struct {
	Total  int
	Passed int
}

// runFactsEvalCandidates executes every candidate trajectory against its own
// on-disk SQLite store through the real Store facts/evidence APIs. The first
// result counts C1 structured adoption (one confirmed current fact equal to the
// latest explicit statement); the second counts C3 controlled-alias Recall@8.
// Both are candidate-tier only.
func runFactsEvalCandidates(t *testing.T, candidates []factsEvalCandidate) (factsEvalTierResult, factsEvalTierResult) {
	t.Helper()
	var adoption, aliases factsEvalTierResult
	validFrom := factsEvalParseOptionalTime(t, "2026-01-01T00:00:00Z")
	changeFrom := factsEvalParseOptionalTime(t, "2026-02-01T00:00:00Z")
	for _, candidate := range candidates {
		harness := newFactsEvalHarness(t, t.TempDir())
		auth := harness.auth("a")
		ctx := harness.ctx

		establish := facts.SubmitEvidenceRequest{
			Source: facts.Source{
				Producer: "facts-eval-candidates", EventID: candidate.ID, Revision: "r1", Fragment: "f1",
				Subject: candidate.Subject, FactKey: candidate.Key, Role: facts.RoleUserQuote,
			},
			Text:           candidate.Establish,
			IdempotencyKey: candidate.ID + "-establish",
			Mutations: []facts.Mutation{{
				Transition: facts.TransitionEstablish, Subject: candidate.Subject, Key: candidate.Key,
				Text: candidate.Establish, ValidFrom: validFrom,
			}},
		}
		established, err := harness.store.SubmitEvidence(ctx, auth, establish)
		if err != nil || len(established.Facts) != 1 {
			t.Fatalf("candidate %s establish = %+v, %v", candidate.ID, established, err)
		}
		if established.Facts[0].Metadata.Adoption != facts.AdoptionPending {
			t.Fatalf("candidate %s establish adoption = %q, want pending", candidate.ID, established.Facts[0].Metadata.Adoption)
		}
		pending, err := harness.store.ReadFacts(ctx, auth, facts.ReadRequest{
			Subject: candidate.Subject, Key: candidate.Key, Budget: facts.Budget{MaxFacts: 8, MaxBytes: 8192},
		})
		if err != nil || len(pending.Facts) != 0 || pending.Background != "" {
			t.Fatalf("candidate %s adopted before confirmation: %+v %v", candidate.ID, pending, err)
		}
		confirmed := facts.SubmitEvidenceRequest{
			Source: facts.Source{
				Producer: "facts-eval-candidates", EventID: candidate.ID, Revision: "r1", Fragment: "f2",
				Subject: candidate.Subject, FactKey: candidate.Key, Role: facts.RoleConfirmation,
			},
			Text:           candidate.Establish,
			IdempotencyKey: candidate.ID + "-confirm",
			Mutations: []facts.Mutation{{
				Transition: facts.TransitionConfirm, TargetRecordID: established.Facts[0].RecordID,
				ExpectedRevision: established.Facts[0].Revision, Subject: candidate.Subject, Key: candidate.Key,
				Text: candidate.Establish,
			}},
		}
		confirmedFacts, err := harness.store.SubmitEvidence(ctx, auth, confirmed)
		if err != nil || len(confirmedFacts.Facts) != 1 {
			t.Fatalf("candidate %s confirm = %+v, %v", candidate.ID, confirmedFacts, err)
		}
		target := confirmedFacts.Facts[0]
		expectedSources := map[memory.ReceiptID]facts.Source{
			established.ReceiptID:    establish.Source,
			confirmedFacts.ReceiptID: confirmed.Source,
		}
		// This is the independently reviewed fixture label, not a value inferred
		// from the operation the harness just submitted.
		expectCurrent := candidate.ExpectCurrent
		if candidate.Change != "" {
			change := facts.SubmitEvidenceRequest{
				Source: facts.Source{
					Producer: "facts-eval-candidates", EventID: candidate.ID, Revision: "r1", Fragment: "f3",
					Subject: candidate.Subject, FactKey: candidate.Key, Role: facts.RoleConfirmation,
				},
				Text:           candidate.Change,
				IdempotencyKey: candidate.ID + "-change",
				Mutations: []facts.Mutation{{
					Transition: facts.TransitionChange, TargetRecordID: target.RecordID,
					ExpectedRevision: target.Revision, Subject: candidate.Subject, Key: candidate.Key,
					Text: candidate.Change, ValidFrom: changeFrom,
				}},
			}
			changed, err := harness.store.SubmitEvidence(ctx, auth, change)
			if err != nil || len(changed.Facts) != 1 {
				t.Fatalf("candidate %s change = %+v, %v", candidate.ID, changed, err)
			}
			target = changed.Facts[0]
			expectedSources = map[memory.ReceiptID]facts.Source{changed.ReceiptID: change.Source}
		}

		adoption.Total++
		current, err := harness.store.ReadFacts(ctx, auth, facts.ReadRequest{
			Subject: candidate.Subject, Key: candidate.Key, Budget: facts.Budget{MaxFacts: 8, MaxBytes: 8192},
		})
		switch {
		case err != nil:
			t.Errorf("candidate %s current read: %v", candidate.ID, err)
		case len(current.Facts) != 1:
			t.Errorf("candidate %s current read returned %d facts, want 1 (%v)", candidate.ID, len(current.Facts), factsEvalFactTexts(current.Facts))
		case current.Facts[0].Text != expectCurrent:
			t.Errorf("candidate %s current fact = %q, want %q", candidate.ID, current.Facts[0].Text, expectCurrent)
		case current.Facts[0].Metadata.Adoption != facts.AdoptionConfirmed:
			t.Errorf("candidate %s current adoption = %q, want confirmed", candidate.ID, current.Facts[0].Metadata.Adoption)
		case current.Facts[0].Metadata.Key != candidate.Key:
			t.Errorf("candidate %s current key = %q, want %q", candidate.ID, current.Facts[0].Metadata.Key, candidate.Key)
		case current.Facts[0].Metadata.Subject != candidate.Subject || current.Facts[0].SpaceID != harness.scopes["a"].Space:
			t.Errorf("candidate %s current attribution differs from admitted subject/Space", candidate.ID)
		case current.Facts[0].RecordID != target.RecordID || current.Facts[0].Revision != target.Revision || !factsEvalCandidateSourcesMatch(current.Facts[0], expectedSources):
			t.Errorf("candidate %s current identity/evidence differs from admitted source effects", candidate.ID)
		default:
			adoption.Passed++
		}

		for _, alias := range candidate.Aliases {
			aliases.Total++
			response, err := harness.store.ReadFacts(ctx, auth, facts.ReadRequest{
				Subject: candidate.Subject, Query: alias, Budget: facts.Budget{MaxFacts: 8, MaxBytes: 8192},
			})
			if err != nil {
				t.Errorf("candidate %s alias %q read: %v", candidate.ID, alias, err)
				continue
			}
			found := false
			for index, fact := range response.Facts {
				if index >= 8 {
					break
				}
				if fact.Text == expectCurrent && fact.RecordID == target.RecordID && fact.Revision == target.Revision &&
					fact.Metadata.Subject == candidate.Subject && fact.SpaceID == harness.scopes["a"].Space &&
					factsEvalCandidateSourcesMatch(fact, expectedSources) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("candidate %s alias %q did not return %q within 8 facts (%v)", candidate.ID, alias, expectCurrent, factsEvalFactTexts(response.Facts))
				continue
			}
			aliases.Passed++
		}
		if err := harness.store.Close(); err != nil {
			t.Fatalf("close candidate %s store: %v", candidate.ID, err)
		}
	}
	return adoption, aliases
}

func factsEvalCandidateSourcesMatch(fact facts.Fact, expected map[memory.ReceiptID]facts.Source) bool {
	if len(fact.Evidence) != len(expected) {
		return false
	}
	seen := make(map[memory.ReceiptID]bool, len(expected))
	for _, evidence := range fact.Evidence {
		source, ok := expected[evidence.ReceiptID]
		if !ok || seen[evidence.ReceiptID] || evidence.Source == nil || *evidence.Source != source {
			return false
		}
		seen[evidence.ReceiptID] = true
	}
	return true
}

type factsEvalReport struct {
	FormatVersion         int                `json:"format_version"`
	Corpus                string             `json:"corpus"`
	Engine                string             `json:"engine"`
	GeneratedAt           string             `json:"generated_at"`
	FinishedAt            string             `json:"finished_at"`
	ManifestHashes        map[string]string  `json:"manifest_hashes"`
	ReviewStatus          string             `json:"review_status"`
	HumanReviewedCases    int                `json:"human_reviewed_cases"`
	ReleaseBlocker        string             `json:"release_blocker"`
	FrozenThresholds      map[string]float64 `json:"frozen_thresholds"`
	ManifestThresholds    map[string]float64 `json:"manifest_thresholds"`
	Candidates            int                `json:"candidate_trajectories"`
	CandidateReviewStatus string             `json:"candidate_review_status"`
	RecallRuns            int                `json:"baseline_recall_runs"`
	RecallHits            int                `json:"baseline_recall_hits"`
	Arms                  []factsEvalArm     `json:"arms"`
	Notes                 []string           `json:"notes"`
}

type factsEvalArm struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Reason    string   `json:"reason,omitempty"`
	Sample    int      `json:"sample"`
	Passed    int      `json:"passed"`
	Rate      *float64 `json:"rate,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}

func newFactsEvalReport(manifest factsEvalManifestDoc, started time.Time) *factsEvalReport {
	return &factsEvalReport{
		FormatVersion:      1,
		Corpus:             manifest.Corpus,
		Engine:             "sqlite (modernc.org/sqlite) real files; Store facts/evidence/management APIs; no model calls",
		GeneratedAt:        started.Format(time.RFC3339),
		ManifestHashes:     map[string]string{},
		ReviewStatus:       manifest.ReviewStatus,
		HumanReviewedCases: manifest.HumanReviewedCases,
		ReleaseBlocker:     manifest.ReleaseBlocker,
		FrozenThresholds: map[string]float64{
			"explicit_preference_min": factsEvalFrozenExplicitPreferenceMin,
			"recall_at8_alias_min":    factsEvalFrozenRecallAt8AliasMin,
		},
		ManifestThresholds:    manifest.Thresholds,
		CandidateReviewStatus: manifest.CandidateExpansion.ReviewStatus,
		Notes: []string{
			"C1-candidate and C3-candidate are candidate-tier circular evidence: the fixtures are derived from the implementation's own controlled vocabulary and cannot support a human quality claim.",
			"No extraction, paraphrase, model or consumer-answer quality is measured by structural reads; arbitrary free-text paraphrase is out of scope.",
			"B0-v0.5.2, B1-steward and C2-steward are unrun; they are not measured results.",
			"Human-gold quality is not measured. Independent AI-agent trajectory review, authorized by the user as a manual-review substitute, is recorded separately in docs/evidence/memory-v0.6-agent-*-review; it does not qualify production model or holdout quality.",
		},
	}
}

func (r *factsEvalReport) setArm(arm factsEvalArm) {
	for index := range r.Arms {
		if r.Arms[index].ID == arm.ID {
			r.Arms[index] = arm
			return
		}
	}
	r.Arms = append(r.Arms, arm)
}

func writeFactsEvalReport(path string, report *factsEvalReport) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return err
	}
	if strings.TrimSpace(report.ReleaseBlocker) == "" {
		return fmt.Errorf("report is missing the human-review release blocker")
	}
	return nil
}
