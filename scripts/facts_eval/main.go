// facts_eval is an offline conformance checker for the M06 facts/evidence
// longitudinal evaluation evidence. It verifies the frozen fixture manifest,
// the controlled alias dictionary, the machine-expanded candidate labels, the
// frozen thresholds, and — when a report is supplied — that the report does not
// overstate any arm. It never runs Memory and never reads private source text.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Frozen before any measurement. This is an independent enforcement point from
// the Go test copy: loosening the manifest alone is not enough to pass.
const (
	frozenExplicitPreferenceMin  = 0.95
	frozenRecallAt8AliasMin      = 0.90
	minimumCandidateTrajectories = 200
)

type options struct {
	fixtures string
	report   string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "facts_eval: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("facts_eval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts options
	flags.StringVar(&opts.fixtures, "fixtures", "internal/appliance/testdata/facts_eval", "facts evaluation fixture directory")
	flags.StringVar(&opts.report, "report", "", "optional facts evaluation report JSON to validate")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	manifest, err := loadManifest(opts.fixtures)
	if err != nil {
		return err
	}
	hashes, candidates, err := verifyFixtures(opts.fixtures, manifest)
	if err != nil {
		return err
	}
	if opts.report != "" {
		if err := verifyReport(opts.report, manifest, hashes, candidates); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "facts_eval: report %s conforms\n", opts.report)
	}
	fmt.Fprintf(stdout, "facts_eval: fixtures conform (trajectories file %s, %d subjects, %d templates, %d candidates)\n",
		hashes["trajectories.json"], manifest.CandidateExpansion.Subjects, manifest.CandidateExpansion.Templates, candidates)
	return nil
}

type manifestDoc struct {
	FormatVersion      int                 `json:"format_version"`
	Corpus             string              `json:"corpus"`
	ReviewStatus       string              `json:"review_status"`
	HumanReviewedCases int                 `json:"human_reviewed_cases"`
	ReleaseBlocker     string              `json:"release_blocker"`
	Thresholds         map[string]float64  `json:"thresholds"`
	ControlledAliases  map[string][]string `json:"controlled_aliases"`
	Fixtures           []manifestFixture   `json:"fixtures"`
	CandidateExpansion struct {
		Generator          string   `json:"generator"`
		Subjects           int      `json:"subjects"`
		Templates          int      `json:"templates"`
		ExpectedCandidates int      `json:"expected_candidates"`
		ReviewStatus       string   `json:"review_status"`
		Tiers              []string `json:"tiers"`
	} `json:"candidate_expansion"`
}

type manifestFixture struct {
	File                 string `json:"file"`
	SHA256               string `json:"sha256"`
	ExpectedTrajectories int    `json:"expected_trajectories"`
}

type candidatesDoc struct {
	FormatVersion int      `json:"format_version"`
	ReviewStatus  string   `json:"review_status"`
	Generator     string   `json:"generator"`
	Tiers         []string `json:"tiers"`
	Subjects      []string `json:"subjects"`
	Templates     []struct {
		ID            string   `json:"id"`
		Key           string   `json:"key"`
		Establish     string   `json:"establish"`
		Aliases       []string `json:"aliases"`
		ExpectCurrent string   `json:"expect_current"`
	} `json:"templates"`
}

func loadManifest(dir string) (manifestDoc, error) {
	var manifest manifestDoc
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, err
	}
	if manifest.FormatVersion != 1 {
		return manifest, fmt.Errorf("manifest format_version = %d", manifest.FormatVersion)
	}
	if manifest.ReviewStatus != "not_human_reviewed" || manifest.HumanReviewedCases != 0 {
		return manifest, fmt.Errorf("manifest claims human review (%q, %d) without evidence", manifest.ReviewStatus, manifest.HumanReviewedCases)
	}
	if strings.TrimSpace(manifest.ReleaseBlocker) == "" {
		return manifest, fmt.Errorf("manifest is missing the human-review release blocker")
	}
	for name, floor := range map[string]float64{
		"explicit_preference_min": frozenExplicitPreferenceMin,
		"recall_at8_alias_min":    frozenRecallAt8AliasMin,
	} {
		value, ok := manifest.Thresholds[name]
		if !ok {
			return manifest, fmt.Errorf("manifest is missing frozen threshold %q", name)
		}
		if value < floor {
			return manifest, fmt.Errorf("manifest threshold %s = %v is below the frozen floor %v", name, value, floor)
		}
	}
	if len(manifest.ControlledAliases) == 0 {
		return manifest, fmt.Errorf("manifest has no controlled alias dictionary")
	}
	for key, aliases := range manifest.ControlledAliases {
		if key == "" || len(aliases) == 0 {
			return manifest, fmt.Errorf("controlled alias entry %q is empty", key)
		}
	}
	if manifest.CandidateExpansion.Generator != "facts_eval_candidate_matrix_v1" {
		return manifest, fmt.Errorf("unexpected candidate generator %q", manifest.CandidateExpansion.Generator)
	}
	if manifest.CandidateExpansion.ReviewStatus != "not_human_reviewed" {
		return manifest, fmt.Errorf("candidate expansion review status = %q", manifest.CandidateExpansion.ReviewStatus)
	}
	return manifest, nil
}

func verifyFixtures(dir string, manifest manifestDoc) (map[string]string, int, error) {
	hashes := map[string]string{}
	for _, fixture := range manifest.Fixtures {
		raw, err := os.ReadFile(filepath.Join(dir, fixture.File))
		if err != nil {
			return nil, 0, err
		}
		digest := sha256.Sum256(raw)
		got := hex.EncodeToString(digest[:])
		if got != fixture.SHA256 {
			return nil, 0, fmt.Errorf("fixture %s digest %s != manifest %s", fixture.File, got, fixture.SHA256)
		}
		hashes[fixture.File] = got
	}
	raw, err := os.ReadFile(filepath.Join(dir, "candidates.json"))
	if err != nil {
		return nil, 0, err
	}
	var candidates candidatesDoc
	if err := json.Unmarshal(raw, &candidates); err != nil {
		return nil, 0, err
	}
	if candidates.FormatVersion != 1 || candidates.ReviewStatus != "not_human_reviewed" {
		return nil, 0, fmt.Errorf("candidates header = format %d review %q", candidates.FormatVersion, candidates.ReviewStatus)
	}
	if candidates.Generator != manifest.CandidateExpansion.Generator {
		return nil, 0, fmt.Errorf("candidates generator %q != manifest %q", candidates.Generator, manifest.CandidateExpansion.Generator)
	}
	if len(candidates.Subjects) != manifest.CandidateExpansion.Subjects {
		return nil, 0, fmt.Errorf("candidates subjects = %d, manifest %d", len(candidates.Subjects), manifest.CandidateExpansion.Subjects)
	}
	if len(candidates.Templates) != manifest.CandidateExpansion.Templates {
		return nil, 0, fmt.Errorf("candidates templates = %d, manifest %d", len(candidates.Templates), manifest.CandidateExpansion.Templates)
	}
	seen := map[string]bool{}
	for _, template := range candidates.Templates {
		if template.ID == "" || template.Key == "" || template.Establish == "" || template.ExpectCurrent == "" || len(template.Aliases) == 0 {
			return nil, 0, fmt.Errorf("candidate template %q is incomplete", template.ID)
		}
		if seen[template.ID] {
			return nil, 0, fmt.Errorf("duplicate candidate template %q", template.ID)
		}
		seen[template.ID] = true
		for _, alias := range template.Aliases {
			if !aliasInDictionary(manifest.ControlledAliases[template.Key], alias) {
				return nil, 0, fmt.Errorf("template %s alias %q is not in the controlled dictionary for %q", template.ID, alias, template.Key)
			}
		}
	}
	total := len(candidates.Subjects) * len(candidates.Templates)
	if total < minimumCandidateTrajectories {
		return nil, 0, fmt.Errorf("candidate expansion = %d, want at least %d", total, minimumCandidateTrajectories)
	}
	if total != manifest.CandidateExpansion.ExpectedCandidates {
		return nil, 0, fmt.Errorf("candidate expansion = %d, manifest expects %d", total, manifest.CandidateExpansion.ExpectedCandidates)
	}
	for _, fixture := range manifest.Fixtures {
		if fixture.File != "trajectories.json" {
			continue
		}
		trajectoryRaw, err := os.ReadFile(filepath.Join(dir, fixture.File))
		if err != nil {
			return nil, 0, err
		}
		var trajectories struct {
			Trajectories []json.RawMessage `json:"trajectories"`
		}
		if err := json.Unmarshal(trajectoryRaw, &trajectories); err != nil {
			return nil, 0, err
		}
		if fixture.ExpectedTrajectories != 0 && len(trajectories.Trajectories) != fixture.ExpectedTrajectories {
			return nil, 0, fmt.Errorf("trajectories = %d, manifest expects %d", len(trajectories.Trajectories), fixture.ExpectedTrajectories)
		}
	}
	return hashes, total, nil
}

func aliasInDictionary(dictionary []string, alias string) bool {
	for _, entry := range dictionary {
		if entry == alias {
			return true
		}
	}
	return false
}

type reportDoc struct {
	FormatVersion         int                `json:"format_version"`
	ManifestHashes        map[string]string  `json:"manifest_hashes"`
	ReviewStatus          string             `json:"review_status"`
	HumanReviewedCases    int                `json:"human_reviewed_cases"`
	ReleaseBlocker        string             `json:"release_blocker"`
	FrozenThresholds      map[string]float64 `json:"frozen_thresholds"`
	ManifestThresholds    map[string]float64 `json:"manifest_thresholds"`
	Candidates            int                `json:"candidate_trajectories"`
	CandidateReviewStatus string             `json:"candidate_review_status"`
	Arms                  []reportArm        `json:"arms"`
}

type reportArm struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Reason    string   `json:"reason"`
	Sample    int      `json:"sample"`
	Passed    int      `json:"passed"`
	Rate      *float64 `json:"rate"`
	Threshold *float64 `json:"threshold"`
	Detail    string   `json:"detail"`
}

func verifyReport(path string, manifest manifestDoc, hashes map[string]string, candidates int) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var report reportDoc
	if err := json.Unmarshal(raw, &report); err != nil {
		return err
	}
	if report.FormatVersion != 1 {
		return fmt.Errorf("report format_version = %d", report.FormatVersion)
	}
	if report.ReviewStatus != manifest.ReviewStatus || report.HumanReviewedCases != 0 {
		return fmt.Errorf("report claims review status %q with %d human-reviewed cases", report.ReviewStatus, report.HumanReviewedCases)
	}
	if report.ReleaseBlocker != manifest.ReleaseBlocker {
		return fmt.Errorf("report release blocker does not match the manifest")
	}
	for file, digest := range hashes {
		if report.ManifestHashes[file] != digest {
			return fmt.Errorf("report manifest hash for %s = %q, want %q", file, report.ManifestHashes[file], digest)
		}
	}
	if len(report.ManifestThresholds) != len(manifest.Thresholds) {
		return fmt.Errorf("report manifest thresholds do not match the manifest")
	}
	for name, value := range manifest.Thresholds {
		if report.ManifestThresholds[name] != value {
			return fmt.Errorf("report manifest threshold %s = %v, want %v", name, report.ManifestThresholds[name], value)
		}
	}
	for name, floor := range map[string]float64{
		"explicit_preference_min": frozenExplicitPreferenceMin,
		"recall_at8_alias_min":    frozenRecallAt8AliasMin,
	} {
		if report.FrozenThresholds[name] < floor {
			return fmt.Errorf("report frozen threshold %s = %v is below the floor %v", name, report.FrozenThresholds[name], floor)
		}
	}
	if report.Candidates < minimumCandidateTrajectories {
		return fmt.Errorf("report candidate count = %d, want at least %d", report.Candidates, minimumCandidateTrajectories)
	}
	if report.CandidateReviewStatus != "not_human_reviewed" {
		return fmt.Errorf("report candidate review status = %q, want not_human_reviewed", report.CandidateReviewStatus)
	}
	if report.Candidates != candidates {
		return fmt.Errorf("report candidate count = %d, fixtures expand to %d", report.Candidates, candidates)
	}

	required := map[string]string{
		"B0-v0.5.2":   "unrun",
		"B1-steward":  "unrun",
		"C2-steward":  "unrun",
		"HUMAN-GOLD":  "blocked",
		"C1-authored": "measured",
	}
	seen := map[string]bool{}
	for _, arm := range report.Arms {
		if arm.ID == "" || arm.Status == "" {
			return fmt.Errorf("report arm is missing identity or status: %+v", arm)
		}
		switch arm.Status {
		case "measured", "unrun", "blocked":
		default:
			return fmt.Errorf("report arm %s has unsupported status %q", arm.ID, arm.Status)
		}
		if seen[arm.ID] {
			return fmt.Errorf("report arm %s is duplicated", arm.ID)
		}
		seen[arm.ID] = true
		switch arm.Status {
		case "measured":
			if arm.Rate == nil {
				return fmt.Errorf("measured arm %s has no rate; it cannot claim a measured result", arm.ID)
			}
			if *arm.Rate < 0 || *arm.Rate > 1 {
				return fmt.Errorf("measured arm %s rate %v is out of range", arm.ID, *arm.Rate)
			}
			if arm.Passed > arm.Sample {
				return fmt.Errorf("measured arm %s passed %d of %d", arm.ID, arm.Passed, arm.Sample)
			}
		case "unrun", "blocked":
			if arm.Rate != nil {
				return fmt.Errorf("arm %s is %s but reports a rate; unrun/blocked arms must not claim a measured result", arm.ID, arm.Status)
			}
			if strings.TrimSpace(arm.Reason) == "" {
				return fmt.Errorf("arm %s is %s without a reason", arm.ID, arm.Status)
			}
		}
		if strings.Contains(strings.ToLower(arm.Name+arm.ID), "gold") && arm.Status == "measured" {
			return fmt.Errorf("arm %s claims measured human-gold quality", arm.ID)
		}
	}
	for id, status := range required {
		arm, ok := findArm(report.Arms, id)
		if !ok {
			return fmt.Errorf("report is missing required arm %s", id)
		}
		if arm.Status != status {
			return fmt.Errorf("arm %s status = %q, want %q under the frozen nomenclature", id, arm.Status, status)
		}
	}
	for _, id := range []string{"C1-candidate", "C3-candidate"} {
		arm, ok := findArm(report.Arms, id)
		if !ok {
			return fmt.Errorf("report is missing candidate arm %s", id)
		}
		if arm.Status != "measured" {
			continue
		}
		if !strings.Contains(arm.Detail, "CANDIDATE TIER ONLY") {
			return fmt.Errorf("candidate arm %s does not label its result as candidate-tier only", id)
		}
		if arm.Threshold == nil {
			return fmt.Errorf("candidate arm %s does not record its frozen threshold", id)
		}
		floor := frozenExplicitPreferenceMin
		if id == "C3-candidate" {
			floor = frozenRecallAt8AliasMin
		}
		if *arm.Threshold < floor {
			return fmt.Errorf("candidate arm %s threshold %v is below the frozen floor %v", id, *arm.Threshold, floor)
		}
		if arm.Rate == nil || *arm.Rate < floor {
			return fmt.Errorf("candidate arm %s rate is below the frozen floor %v", id, floor)
		}
	}
	return nil
}

func findArm(arms []reportArm, id string) (reportArm, bool) {
	for _, arm := range arms {
		if arm.ID == id {
			return arm, true
		}
	}
	return reportArm{}, false
}
