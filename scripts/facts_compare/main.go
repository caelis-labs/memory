// facts_compare runs a structural, model-free comparison between the exact
// v0.5.2 baseline and the current tree:
//
//   - it extracts the baseline revision with "git archive" into the system
//     temporary directory (no credentials, no network);
//   - it copies the current working tree, including uncommitted work;
//   - it injects the same fixture and the same generated tests into both trees;
//   - it runs the same legacy Remember plus scripted deterministic proposal
//     bytes on both sides;
//   - it compares Steward context selection (baseline recency-only Record
//     context versus current relevant context) and evidence Recall fragments.
//
// It reports retrieval and structural selection only. It does not claim that a
// preference was answered, adopted, extracted, or paraphrased, and it calls no
// model.
package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultBaselineRevision = "51693ff135be8c4149c15117980290aaad6d90da"
	defaultUnrelatedRecords = 80
)

type options struct {
	baselineRevision string
	unrelated        int
	currentOnly      bool
	workdir          string
	keep             bool
	output           string
	timeout          time.Duration
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "facts_compare: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("facts_compare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts options
	flags.StringVar(&opts.baselineRevision, "baseline-revision", defaultBaselineRevision, "exact baseline revision to archive and compare")
	flags.IntVar(&opts.unrelated, "unrelated", defaultUnrelatedRecords, "number of unrelated Records inserted between the old Record and the probe (must exceed the context window)")
	flags.BoolVar(&opts.currentOnly, "current-only", false, "skip the baseline tree and run the current side only")
	flags.StringVar(&opts.workdir, "workdir", "", "optional existing directory for the temporary trees")
	flags.BoolVar(&opts.keep, "keep", false, "keep the temporary trees for inspection")
	flags.StringVar(&opts.output, "output", "", "optional owner-only aggregate JSON report path")
	flags.DurationVar(&opts.timeout, "timeout", 15*time.Minute, "timeout for each side's go test run")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if opts.unrelated < 1 {
		return fmt.Errorf("-unrelated must be positive")
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	if !opts.currentOnly {
		if err := verifyRevision(root, opts.baselineRevision); err != nil {
			return err
		}
	}
	tmpDir := opts.workdir
	if tmpDir == "" {
		tmpDir, err = os.MkdirTemp("", "memory-facts-compare-")
		if err != nil {
			return err
		}
		if !opts.keep {
			defer os.RemoveAll(tmpDir)
		}
	}
	fixture := defaultFixture(opts.unrelated)
	fixtureJSON, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return err
	}

	report := compareReport{
		FormatVersion:    1,
		BaselineRevision: opts.baselineRevision,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		CurrentHead:      gitRevision(root, "HEAD"),
		CurrentWorktree:  worktreeFingerprint(root),
		Engine:           "go test in temporary trees; modernc.org/sqlite real files; legacy Remember plus scripted deterministic proposals; no model, no credentials",
		Fixture:          fixture,
		FixtureSHA256:    sha256Hex(string(fixtureJSON)),
		Notes: []string{
			"This is a structural, model-free comparison. It reports retrieval fragments and context selection only.",
			"A returned fragment or a selected context Record is never evidence that the user's preference was answered, adopted, extracted or paraphrased.",
			"Both sides use the same legacy Remember path and the same scripted proposal bytes, so the comparison is not an ingestion-advantage test.",
			"The current-only trusted-source subject/fact-key match depends on the facts/evidence API, which the baseline does not have; it is supplemental.",
			"Real extraction, model and consumer-answer quality comparisons remain GA-blocked.",
			"Nomenclature: B0 is receipt-only Recall on a separate store with no Steward profile binding, so no Steward job and no Record exist. A Steward-organized Recall is B1, not B0.",
			"Scheduler encoding is reported per side: the exact v0.5.2 baseline compares RFC3339Nano schedule text; the final v0.6 candidate stores fixed-width UTC nanoseconds.",
		},
		Commands: []string{
			fmt.Sprintf("git -C %s archive --format=tar %s | tar -x -C <tmp>/baseline", root, opts.baselineRevision),
			"GOWORK=off FACTS_COMPARE_FIXTURE=<fixture> FACTS_COMPARE_RESULT=<result> FACTS_COMPARE_SIDE=baseline go test -count=1 -run=^TestFactsCompareStruct$ ./internal/appliance  (in the archived baseline tree)",
			"GOWORK=off FACTS_COMPARE_FIXTURE=<fixture> FACTS_COMPARE_RESULT=<result> FACTS_COMPARE_SIDE=current go test -count=1 -run=^TestFactsCompareStruct$ ./internal/appliance  (in the copied current tree)",
			fmt.Sprintf("go run ./scripts/facts_compare -baseline-revision %s -unrelated %d -output <report.json>", opts.baselineRevision, opts.unrelated),
		},
	}

	if !opts.currentOnly {
		baselineDir := filepath.Join(tmpDir, "baseline")
		if err := archiveRevision(root, opts.baselineRevision, baselineDir); err != nil {
			return err
		}
		report.BaselineSubject = gitSubject(root, opts.baselineRevision)
		baseline, err := runSide(tmpDir, baselineDir, "baseline", fixtureJSON, opts.timeout)
		if err != nil {
			return err
		}
		report.Baseline = baseline
	}

	currentDir := filepath.Join(tmpDir, "current")
	if err := copyTree(root, currentDir); err != nil {
		return err
	}
	current, err := runSide(tmpDir, currentDir, "current", fixtureJSON, opts.timeout)
	if err != nil {
		return err
	}
	report.Current = current
	report.buildArms()

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if opts.output != "" {
		if dir := filepath.Dir(opts.output); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
		}
		if err := os.WriteFile(opts.output, append(encoded, '\n'), 0o600); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "%s\n", encoded)
	return nil
}

// --- fixture ---

type profileFixture struct {
	ProfileID         string `json:"profile_id"`
	Version           uint64 `json:"version"`
	SystemPrompt      string `json:"system_prompt"`
	MaxContextRecords int    `json:"max_context_records"`
	MaxInputBytes     int    `json:"max_input_bytes"`
	MaxOutputBytes    int    `json:"max_output_bytes"`
}

type queryFixture struct {
	Query         string `json:"query"`
	ExpectedText  string `json:"expected_text"`
	SecondaryText string `json:"secondary_text,omitempty"`
}

type supplementalFixture struct {
	Subject           string `json:"subject"`
	FactKey           string `json:"fact_key"`
	FactText          string `json:"fact_text"`
	ProbeReceiptText  string `json:"probe_receipt_text"`
	UnrelatedRecords  int    `json:"unrelated_records"`
	UnrelatedTemplate string `json:"unrelated_template"`
}

type fixture struct {
	UnrelatedRecords int                 `json:"unrelated_records"`
	OldReceiptText   string              `json:"old_receipt_text"`
	OldRecordKind    string              `json:"old_record_kind"`
	OldRecordText    string              `json:"old_record_text"`
	UnrelatedKind    string              `json:"unrelated_kind"`
	UnrelatedText    string              `json:"unrelated_text"`
	ProbeReceiptText string              `json:"probe_receipt_text"`
	Profile          profileFixture      `json:"profile"`
	RecallQueries    []queryFixture      `json:"recall_queries"`
	Supplemental     supplementalFixture `json:"supplemental"`
}

func defaultFixture(unrelated int) fixture {
	return fixture{
		UnrelatedRecords: unrelated,
		OldReceiptText:   "I prefer coffee.",
		OldRecordKind:    "preference",
		OldRecordText:    "Prefers coffee.",
		UnrelatedKind:    "note",
		UnrelatedText:    "Unrelated operating note %03d about schedules and tooling.",
		ProbeReceiptText: "I now prefer tea over coffee.",
		Profile: profileFixture{
			ProfileID: "profile-facts-compare", Version: 1, SystemPrompt: "organize evidence",
			MaxContextRecords: 8, MaxInputBytes: 128 << 10, MaxOutputBytes: 16 << 10,
		},
		RecallQueries: []queryFixture{
			{Query: "coffee", ExpectedText: "I prefer coffee."},
			{Query: "tea coffee", ExpectedText: "I now prefer tea over coffee.", SecondaryText: "I prefer coffee."},
		},
		Supplemental: supplementalFixture{
			Subject: "subject-compare", FactKey: "preference.drink", FactText: "Prefers espresso.",
			ProbeReceiptText:  "Espresso preference update for the compare subject.",
			UnrelatedRecords:  unrelated,
			UnrelatedTemplate: "Supplemental unrelated note %03d about schedules and tooling.",
		},
	}
}

// --- result mirror ---

type recallObservation struct {
	Query             string   `json:"query"`
	ExpectedPresent   bool     `json:"expected_present"`
	ExpectedRank      int      `json:"expected_rank"`
	SecondaryPresent  bool     `json:"secondary_present"`
	SecondaryRank     int      `json:"secondary_rank"`
	Fragments         []string `json:"fragments"`
	ConsistencyToken  string   `json:"consistency_token,omitempty"`
	RetrievalOnly     bool     `json:"retrieval_only"`
	AnswersPreference bool     `json:"answers_preference"`
}

type sideResult struct {
	Side                  string              `json:"side"`
	SchedulerEncoding     string              `json:"scheduler_encoding"`
	ProfileID             string              `json:"profile_id"`
	ProfileVersion        uint64              `json:"profile_version"`
	MaxContextRecords     int                 `json:"max_context_records"`
	OldRecordIncluded     bool                `json:"old_record_included"`
	OldRecordRank         int                 `json:"old_record_rank"`
	RecordsCount          int                 `json:"records_count"`
	RecordsSHA256         string              `json:"records_sha256"`
	OldRecordIDHash       string              `json:"old_record_id_hash"`
	ReceiptAttribution    map[string]any      `json:"receipt_attribution"`
	ScriptedOutputsSHA256 string              `json:"scripted_outputs_sha256"`
	NoStewardRecall       []recallObservation `json:"no_steward_recall"`
	NoStewardJobs         int                 `json:"no_steward_jobs"`
	NoStewardRecords      int                 `json:"no_steward_records"`
	StewardRecall         []recallObservation `json:"steward_recall"`
	Supplemental          map[string]any      `json:"supplemental"`
}

type compareArm struct {
	ID                string   `json:"id"`
	Side              string   `json:"side"`
	Status            string   `json:"status"`
	Reason            string   `json:"reason,omitempty"`
	Detail            string   `json:"detail,omitempty"`
	RelevantIncluded  *bool    `json:"relevant_included,omitempty"`
	RelevantRank      int      `json:"relevant_rank,omitempty"`
	RecordsCount      int      `json:"records_count,omitempty"`
	RetrievalHitRate  *float64 `json:"retrieval_hit_rate,omitempty"`
	RetrievalOnly     bool     `json:"retrieval_only"`
	AnswersPreference bool     `json:"answers_preference"`
}

type compareVerdict struct {
	B1RelevantIncluded      bool `json:"b1_relevant_included"`
	C2RelevantIncluded      bool `json:"c2_relevant_included"`
	ContextSelectionChanged bool `json:"context_selection_changed"`
	ContextRecordsIdentical bool `json:"context_records_identical"`
	StructuralOnly          bool `json:"structural_only"`
	AnswersPreference       bool `json:"answers_preference"`
	RealQualityClaim        bool `json:"real_quality_claim"`
}

type compareReport struct {
	FormatVersion    int            `json:"format_version"`
	BaselineRevision string         `json:"baseline_revision"`
	BaselineSubject  string         `json:"baseline_subject,omitempty"`
	CurrentHead      string         `json:"current_head,omitempty"`
	CurrentWorktree  string         `json:"current_worktree_sha256,omitempty"`
	GeneratedAt      string         `json:"generated_at"`
	Engine           string         `json:"engine"`
	Fixture          fixture        `json:"fixture"`
	FixtureSHA256    string         `json:"fixture_sha256"`
	Baseline         sideResult     `json:"baseline_side"`
	Current          sideResult     `json:"current_side"`
	Arms             []compareArm   `json:"arms"`
	Verdict          compareVerdict `json:"verdict"`
	Commands         []string       `json:"commands"`
	Notes            []string       `json:"notes"`
}

func (r *compareReport) validate() error {
	for _, side := range []sideResult{r.Baseline, r.Current} {
		if side.Side == "" {
			continue
		}
		if side.NoStewardJobs != 0 || side.NoStewardRecords != 0 {
			return fmt.Errorf("%s no-Steward arm created derived state: jobs=%d records=%d", side.Side, side.NoStewardJobs, side.NoStewardRecords)
		}
	}
	return nil
}

func (r *compareReport) buildArms() {
	baselineMeasured := r.Baseline.Side != ""
	b1Included := r.Baseline.OldRecordIncluded
	c2Included := r.Current.OldRecordIncluded
	if baselineMeasured {
		r.Arms = append(r.Arms, compareArm{
			ID: "B0-exact-v0.5.2-no-steward-recall", Side: "baseline", Status: "measured",
			Detail:        fmt.Sprintf("receipt-only Recall with no Steward profile binding, no Steward job and no Record (jobs=%d records=%d); retrieval fragments only", r.Baseline.NoStewardJobs, r.Baseline.NoStewardRecords),
			RetrievalOnly: true, AnswersPreference: false,
			RetrievalHitRate: hitRate(r.Baseline.NoStewardRecall),
		})
		r.Arms = append(r.Arms, compareArm{
			ID: "B1-prior-steward-context", Side: "baseline", Status: "measured",
			RelevantIncluded: &b1Included, RelevantRank: r.Baseline.OldRecordRank, RecordsCount: r.Baseline.RecordsCount,
			Detail: "exact v0.5.2 Claim Work.Records context after the old coffee Record, " +
				fmt.Sprintf("%d newer unrelated Records and the related probe", r.Fixture.UnrelatedRecords),
		})
		r.Arms = append(r.Arms, compareArm{
			ID: "B1-exact-v0.5.2-steward-recall", Side: "baseline", Status: "measured",
			Detail:        "Steward-organized evidence Recall on the same side (fragments include the old coffee Record); retrieval only",
			RetrievalOnly: true, AnswersPreference: false,
			RetrievalHitRate: hitRate(r.Baseline.StewardRecall),
		})
	} else {
		for _, id := range []string{"B0-exact-v0.5.2-no-steward-recall", "B1-prior-steward-context", "B1-exact-v0.5.2-steward-recall"} {
			r.Arms = append(r.Arms, compareArm{ID: id, Side: "baseline", Status: "unrun", Reason: "baseline side skipped with -current-only"})
		}
	}
	r.Arms = append(r.Arms, compareArm{
		ID: "C2-relevant-steward-context", Side: "current", Status: "measured",
		RelevantIncluded: &c2Included, RelevantRank: r.Current.OldRecordRank, RecordsCount: r.Current.RecordsCount,
		Detail: "current Claim Work.Records context under the same fixture, profile and scripted proposal bytes",
	})
	r.Arms = append(r.Arms, compareArm{
		ID: "L0-current-no-steward-recall", Side: "current", Status: "measured",
		Detail:        fmt.Sprintf("in-tree receipt-only Recall control with no Steward job and no Record (jobs=%d records=%d); not a baseline result", r.Current.NoStewardJobs, r.Current.NoStewardRecords),
		RetrievalOnly: true, AnswersPreference: false,
		RetrievalHitRate: hitRate(r.Current.NoStewardRecall),
	})
	r.Arms = append(r.Arms, compareArm{
		ID: "C2-current-steward-recall", Side: "current", Status: "measured",
		Detail:        "current Steward-organized evidence Recall on the same fixture; retrieval only",
		RetrievalOnly: true, AnswersPreference: false,
		RetrievalHitRate: hitRate(r.Current.StewardRecall),
	})
	if status, ok := r.Current.Supplemental["status"].(string); ok {
		arm := compareArm{
			ID: "C2-trusted-source-supplemental", Side: "current", Status: status,
			Detail:            "current-only trusted host source subject/fact-key match; the baseline cannot run it",
			RetrievalOnly:     false,
			AnswersPreference: false,
		}
		if included, ok := r.Current.Supplemental["relevant_included"].(bool); ok {
			arm.RelevantIncluded = &included
		}
		if rank, ok := r.Current.Supplemental["relevant_rank"].(float64); ok {
			arm.RelevantRank = int(rank)
		}
		if reason, ok := r.Current.Supplemental["reason"].(string); ok {
			arm.Reason = reason
		}
		r.Arms = append(r.Arms, arm)
	}
	r.Arms = append(r.Arms, compareArm{
		ID: "C2-trusted-source-supplemental-baseline", Side: "baseline", Status: "not_applicable",
		Reason: "the exact v0.5.2 baseline has no facts/evidence API or trusted host source attribution",
	})
	r.Arms = append(r.Arms, compareArm{
		ID: "GA-real-quality", Side: "none", Status: "blocked",
		Reason: "extraction, model and consumer-answer quality require a real model and human-reviewed data; this comparison is structural only",
	})
	r.Verdict = compareVerdict{
		B1RelevantIncluded:      b1Included,
		C2RelevantIncluded:      c2Included,
		ContextSelectionChanged: baselineMeasured && b1Included != c2Included,
		ContextRecordsIdentical: baselineMeasured && r.Baseline.RecordsSHA256 == r.Current.RecordsSHA256,
		StructuralOnly:          true,
		AnswersPreference:       false,
		RealQualityClaim:        false,
	}
}

func hitRate(observations []recallObservation) *float64 {
	if len(observations) == 0 {
		return nil
	}
	hits := 0
	for _, observation := range observations {
		if observation.ExpectedPresent {
			hits++
		}
	}
	rate := float64(hits) / float64(len(observations))
	return &rate
}

// --- tree preparation ---

func moduleRoot() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func verifyRevision(root, revision string) error {
	command := exec.Command("git", "-C", root, "rev-parse", "--verify", revision+"^{commit}")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("baseline revision %s is not a commit: %v: %s", revision, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitSubject(root, revision string) string {
	output, err := exec.Command("git", "-C", root, "log", "-1", "--format=%H %s", revision).Output()
	if err != nil {
		return revision
	}
	return strings.TrimSpace(string(output))
}

func gitRevision(root, revision string) string {
	output, err := exec.Command("git", "-C", root, "rev-parse", revision).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// worktreeFingerprint records the uncommitted state that the current side
// snapshot contains, so a result can be tied to an exact working tree.
func worktreeFingerprint(root string) string {
	status, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	diff, _ := exec.Command("git", "-C", root, "diff", "HEAD").Output()
	untracked, _ := exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard").Output()
	return sha256Hex(string(status) + "\x00" + string(diff) + "\x00" + string(untracked))
}

func archiveRevision(root, revision, destination string) error {
	command := exec.Command("git", "-C", root, "archive", "--format=tar", revision)
	pipe, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	if err := extractTar(pipe, destination); err != nil {
		_ = command.Wait()
		return err
	}
	if err := command.Wait(); err != nil {
		return fmt.Errorf("git archive %s: %w", revision, err)
	}
	return nil
}

func extractTar(reader io.Reader, destination string) error {
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		cleaned := filepath.Clean(header.Name)
		if cleaned == "." || strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
			return fmt.Errorf("unsafe archive entry %q", header.Name)
		}
		target := filepath.Join(destination, cleaned)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, archive); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
}

var factsCompareCopyExcludes = map[string]bool{
	".git": true, "dist": true, ".cache": true, ".tmp": true, "bin": true,
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0o755)
		}
		top := strings.Split(filepath.ToSlash(relative), "/")[0]
		if factsCompareCopyExcludes[top] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, 0o755)
		case entry.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			info, err := entry.Info()
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, raw, info.Mode()&0o777)
		}
	})
}

// --- side execution ---

func runSide(tmpDir, tree, side string, fixtureJSON []byte, timeout time.Duration) (sideResult, error) {
	var result sideResult
	fixtureDir := filepath.Join(tree, "internal", "appliance", "testdata", "facts_compare")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		return result, err
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "fixture.json"), append(fixtureJSON, '\n'), 0o644); err != nil {
		return result, err
	}
	applianceDir := filepath.Join(tree, "internal", "appliance")
	if err := os.WriteFile(filepath.Join(applianceDir, factsCompareSharedTestFile), []byte(factsCompareSharedTest), 0o644); err != nil {
		return result, err
	}
	sideSource := factsCompareBaselineSideTest
	if side == "current" {
		sideSource = factsCompareCurrentSideTest
	}
	if err := os.WriteFile(filepath.Join(applianceDir, factsCompareSideTestFile), []byte(sideSource), 0o644); err != nil {
		return result, err
	}
	resultPath := filepath.Join(tmpDir, side+"-result.json")
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-count=1", "-run=^TestFactsCompareStruct$", "./internal/appliance")
	command.Dir = tree
	command.Env = append(os.Environ(),
		"GOWORK=off",
		"FACTS_COMPARE_FIXTURE="+filepath.Join(fixtureDir, "fixture.json"),
		"FACTS_COMPARE_RESULT="+resultPath,
		"FACTS_COMPARE_SIDE="+side,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return result, fmt.Errorf("%s side go test failed: %v\n%s", side, err, tail(string(output), 4000))
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		return result, fmt.Errorf("%s side did not write a result: %w\n%s", side, err, tail(string(output), 2000))
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("parse %s side result: %w", side, err)
	}
	result.SchedulerEncoding = schedulerEncoding(tree)
	return result, nil
}

// schedulerEncoding reports which Steward schedule-time encoding the side
// stores. The exact v0.5.2 baseline compares RFC3339Nano text; the final v0.6
// candidate stores fixed-width UTC nanoseconds.
func schedulerEncoding(tree string) string {
	if _, err := os.Stat(filepath.Join(tree, "internal", "appliance", "steward_schedule.go")); err == nil {
		return "fixed_width_utc_nanoseconds"
	}
	return "rfc3339nano_lexicographic"
}

func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
