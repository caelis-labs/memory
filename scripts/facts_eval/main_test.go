package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixturesRel = "../../internal/appliance/testdata/facts_eval"

func TestFactsEvalFixturesConform(t *testing.T) {
	var out strings.Builder
	if err := run([]string{"-fixtures", fixturesRel}, &out); err != nil {
		t.Fatalf("fixtures should conform: %v", err)
	}
	if !strings.Contains(out.String(), "fixtures conform") {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestFactsEvalRejectsLoosenedThreshold(t *testing.T) {
	dir := copyFixtures(t)
	rewriteManifest(t, dir, func(manifest map[string]any) {
		manifest["thresholds"].(map[string]any)["explicit_preference_min"] = 0.5
	})
	if err := run([]string{"-fixtures", dir}, &strings.Builder{}); err == nil {
		t.Fatal("loosened threshold must be rejected")
	}
}

func TestFactsEvalRejectsHumanGoldClaim(t *testing.T) {
	dir := copyFixtures(t)
	rewriteManifest(t, dir, func(manifest map[string]any) {
		manifest["review_status"] = "human_reviewed"
		manifest["human_reviewed_cases"] = 208
	})
	if err := run([]string{"-fixtures", dir}, &strings.Builder{}); err == nil {
		t.Fatal("human-review claim without evidence must be rejected")
	}
}

func TestFactsEvalRejectsTamperedFixtureDigest(t *testing.T) {
	dir := copyFixtures(t)
	path := filepath.Join(dir, "candidates.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-fixtures", dir}, &strings.Builder{}); err == nil {
		t.Fatal("tampered fixture must be rejected")
	}
}

func TestFactsEvalRejectsOverstatedReport(t *testing.T) {
	dir := copyFixtures(t)
	report := reportFixture(t, dir)
	// A report that claims the exact v0.5.2 baseline was measured is a lie.
	for index := range report["arms"].([]any) {
		arm := report["arms"].([]any)[index].(map[string]any)
		if arm["id"] == "B0-v0.5.2" {
			arm["status"] = "measured"
			arm["rate"] = 1.0
			arm["sample"] = 10
			arm["passed"] = 10
		}
	}
	path := filepath.Join(t.TempDir(), "report.json")
	writeJSON(t, path, report)
	if err := run([]string{"-fixtures", dir, "-report", path}, &strings.Builder{}); err == nil {
		t.Fatal("overstated report must be rejected")
	}
}

func TestFactsEvalAcceptsConformingReport(t *testing.T) {
	dir := copyFixtures(t)
	report := reportFixture(t, dir)
	path := filepath.Join(t.TempDir(), "report.json")
	writeJSON(t, path, report)
	if err := run([]string{"-fixtures", dir, "-report", path}, &strings.Builder{}); err != nil {
		t.Fatalf("conforming report rejected: %v", err)
	}
}

func copyFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(fixturesRel)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(fixturesRel, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func rewriteManifest(t *testing.T, dir string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	mutate(manifest)
	writeJSON(t, path, manifest)
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// reportFixture builds a minimal conforming report for the fixture manifest.
func reportFixture(t *testing.T, dir string) map[string]any {
	t.Helper()
	manifestRaw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	hashes := map[string]any{}
	for _, fixture := range manifest["fixtures"].([]any) {
		entry := fixture.(map[string]any)
		hashes[entry["file"].(string)] = entry["sha256"]
	}
	expansion := manifest["candidate_expansion"].(map[string]any)
	rate := 1.0
	threshold := 0.95
	aliasThreshold := 0.90
	return map[string]any{
		"format_version":          1,
		"review_status":           "not_human_reviewed",
		"human_reviewed_cases":    0,
		"release_blocker":         manifest["release_blocker"],
		"manifest_hashes":         hashes,
		"manifest_thresholds":     manifest["thresholds"],
		"frozen_thresholds":       map[string]any{"explicit_preference_min": 0.95, "recall_at8_alias_min": 0.90},
		"candidate_trajectories":  expansion["expected_candidates"],
		"candidate_review_status": "not_human_reviewed",
		"arms": []any{
			map[string]any{"id": "C1-authored", "name": "structured_lifecycle_conformance", "status": "measured", "sample": 14, "passed": 14, "rate": 1.0},
			map[string]any{"id": "L0-legacy", "name": "in_tree_model_free_recall_control", "status": "measured", "sample": 2, "passed": 2, "rate": 1.0},
			map[string]any{"id": "B0-v0.5.2", "name": "exact_v0_5_2_checkout_recall", "status": "unrun", "reason": "not exercised in this workspace"},
			map[string]any{"id": "B1-steward", "name": "prior_steward_baseline", "status": "unrun", "reason": "needs prior Steward revision"},
			map[string]any{"id": "C2-steward", "name": "relevant_steward_context", "status": "unrun", "reason": "needs scripted deterministic generator"},
			map[string]any{"id": "HUMAN-GOLD", "name": "human_reviewed_quality", "status": "blocked", "reason": manifest["release_blocker"]},
			map[string]any{"id": "C1-candidate", "name": "structured_adoption_candidates", "status": "measured", "sample": 208, "passed": 208, "rate": rate, "threshold": threshold, "detail": "CANDIDATE TIER ONLY, not human-reviewed"},
			map[string]any{"id": "C3-candidate", "name": "controlled_alias_recall_at8", "status": "measured", "sample": 448, "passed": 448, "rate": rate, "threshold": aliasThreshold, "detail": "CANDIDATE TIER ONLY, not human-reviewed"},
		},
	}
}
