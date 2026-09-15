package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultFixtureIsDeterministicAndFair(t *testing.T) {
	first := defaultFixture(80)
	second := defaultFixture(80)
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("default fixture must be deterministic")
	}
	if first.Supplemental.UnrelatedRecords != first.UnrelatedRecords {
		t.Fatal("supplemental and primary unrelated counts must match")
	}
	if strings.Count(first.UnrelatedText, "%") != 1 {
		t.Fatalf("unrelated text must carry exactly one format verb: %q", first.UnrelatedText)
	}
	if first.Profile.MaxContextRecords <= 0 {
		t.Fatal("fixture profile must bound the context window")
	}
}

func TestBuildArmsVerdict(t *testing.T) {
	report := &compareReport{Fixture: fixture{UnrelatedRecords: 80}}
	report.Baseline = sideResult{
		Side: "baseline", OldRecordIncluded: false, RecordsCount: 8, RecordsSHA256: "aaa", SchedulerEncoding: "rfc3339nano_lexicographic",
		NoStewardRecall: []recallObservation{{Query: "coffee", ExpectedPresent: true, ExpectedRank: 2, RetrievalOnly: true}},
		StewardRecall:   []recallObservation{{Query: "coffee", ExpectedPresent: true, ExpectedRank: 1, RetrievalOnly: true}},
	}
	report.Current = sideResult{
		Side: "current", OldRecordIncluded: true, OldRecordRank: 1, RecordsCount: 8, RecordsSHA256: "bbb", SchedulerEncoding: "fixed_width_utc_nanoseconds",
		NoStewardRecall: []recallObservation{{Query: "coffee", ExpectedPresent: true, ExpectedRank: 2, RetrievalOnly: true}},
		StewardRecall:   []recallObservation{{Query: "coffee", ExpectedPresent: true, ExpectedRank: 1, RetrievalOnly: true}},
		Supplemental:    map[string]any{"status": "measured", "relevant_included": true, "relevant_rank": float64(1)},
	}
	report.buildArms()
	if err := report.validate(); err != nil {
		t.Fatal(err)
	}
	if !report.Verdict.ContextSelectionChanged {
		t.Fatal("differing context inclusion must be reported as a change")
	}
	if report.Verdict.ContextRecordsIdentical {
		t.Fatal("differing record sets must not be reported as identical")
	}
	if report.Verdict.AnswersPreference || report.Verdict.RealQualityClaim || !report.Verdict.StructuralOnly {
		t.Fatalf("verdict must stay structural and must not claim answer quality: %+v", report.Verdict)
	}
	for _, arm := range report.Arms {
		if arm.ID == "GA-real-quality" && arm.Status != "blocked" {
			t.Fatalf("real quality arm must stay blocked, got %q", arm.Status)
		}
		if arm.Status == "measured" && arm.AnswersPreference {
			t.Fatalf("arm %s claimed answered preference", arm.ID)
		}
	}
	b0, ok := findCompareArm(report.Arms, "B0-exact-v0.5.2-no-steward-recall")
	if !ok || b0.Status != "measured" || b0.RetrievalHitRate == nil {
		t.Fatalf("B0 must be the measured no-Steward receipt Recall arm: %+v", b0)
	}
	if _, ok := findCompareArm(report.Arms, "B1-exact-v0.5.2-steward-recall"); !ok {
		t.Fatal("the Steward-organized baseline Recall must be labeled B1")
	}
	if _, ok := findCompareArm(report.Arms, "C2-current-steward-recall"); !ok {
		t.Fatal("the current Steward-organized Recall must be labeled C2")
	}
	if len(report.Arms) == 0 {
		t.Fatal("report must carry arms")
	}
}

func TestValidateRejectsNoStewardDerivedState(t *testing.T) {
	report := compareReport{}
	report.Current = sideResult{Side: "current", NoStewardJobs: 1}
	if err := report.validate(); err == nil {
		t.Fatal("a no-Steward arm with created state must be rejected")
	}
}

func findCompareArm(arms []compareArm, id string) (compareArm, bool) {
	for _, arm := range arms {
		if arm.ID == id {
			return arm, true
		}
	}
	return compareArm{}, false
}

func TestGeneratedTopLevelIdentifiersArePrefixed(t *testing.T) {
	pattern := regexp.MustCompile(`(?m)^(?:func|type|var|const)\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)
	for _, source := range []string{factsCompareSharedTest, factsCompareBaselineSideTest, factsCompareCurrentSideTest} {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			name := match[1]
			if !strings.HasPrefix(name, "factsCompare") && !strings.HasPrefix(name, "TestFactsCompare") {
				t.Fatalf("generated top-level identifier %q must be prefixed to avoid colliding with package test helpers", name)
			}
		}
	}
}

func TestCopyTreeExcludesRepositoryState(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, ".git", "config"), "x")
	writeFile(t, filepath.Join(source, "dist", "artifact.json"), "x")
	writeFile(t, filepath.Join(source, "docs", "note.md"), "ok")
	destination := t.TempDir()
	if err := copyTree(source, filepath.Join(destination, "current")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "current", "docs", "note.md")); err != nil {
		t.Fatalf("copy must keep repository content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "current", ".git")); !os.IsNotExist(err) {
		t.Fatal("copy must exclude .git")
	}
	if _, err := os.Stat(filepath.Join(destination, "current", "dist")); !os.IsNotExist(err) {
		t.Fatal("copy must exclude dist")
	}
}

func TestExtractTarRejectsTraversal(t *testing.T) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractTar(bytes.NewReader(buffer.Bytes()), t.TempDir()); err == nil {
		t.Fatal("archive traversal must be rejected")
	}
}

func TestSHA256HexAndTail(t *testing.T) {
	if got, want := sha256Hex("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Fatalf("sha256Hex = %s, want %s", got, want)
	}
	if got := tail("abcdef", 3); got != "def" {
		t.Fatalf("tail = %q", got)
	}
	if got := tail("abc", 10); got != "abc" {
		t.Fatalf("tail = %q", got)
	}
}

// TestFactsCompareEndToEnd archives the exact baseline and runs the structural
// comparison. It is opt-in because it builds two trees.
func TestFactsCompareEndToEnd(t *testing.T) {
	if os.Getenv("MEMORY_FACTS_COMPARE") != "1" {
		t.Skip("set MEMORY_FACTS_COMPARE=1 to run the two-tree structural comparison")
	}
	var out bytes.Buffer
	if err := run([]string{"-unrelated", "12"}, &out); err != nil {
		t.Fatalf("comparison failed: %v", err)
	}
	var report compareReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("parse comparison report: %v", err)
	}
	if report.Baseline.Side != "baseline" || report.Current.Side != "current" {
		t.Fatalf("both sides must run: %+v", report)
	}
	if report.Baseline.ScriptedOutputsSHA256 != report.Current.ScriptedOutputsSHA256 {
		t.Fatal("both sides must use the same scripted proposal bytes")
	}
	if report.Baseline.NoStewardJobs != 0 || report.Baseline.NoStewardRecords != 0 || report.Current.NoStewardJobs != 0 || report.Current.NoStewardRecords != 0 {
		t.Fatalf("no-Steward receipt-only arms must create no job or Record: baseline=%+v current=%+v", report.Baseline, report.Current)
	}
	if _, ok := findCompareArm(report.Arms, "B0-exact-v0.5.2-no-steward-recall"); !ok {
		t.Fatal("B0 must be the no-Steward receipt Recall arm")
	}
	if _, ok := findCompareArm(report.Arms, "B1-exact-v0.5.2-steward-recall"); !ok {
		t.Fatal("the Steward-organized baseline Recall must be labeled B1")
	}
	if report.Baseline.SchedulerEncoding != "rfc3339nano_lexicographic" {
		t.Fatalf("exact v0.5.2 baseline scheduler encoding = %q", report.Baseline.SchedulerEncoding)
	}
	if report.Current.SchedulerEncoding != "fixed_width_utc_nanoseconds" {
		t.Fatalf("current scheduler encoding = %q, want the fixed-width encoding", report.Current.SchedulerEncoding)
	}
	if !report.Verdict.StructuralOnly || report.Verdict.RealQualityClaim {
		t.Fatalf("comparison must stay structural: %+v", report.Verdict)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
