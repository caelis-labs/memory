package appliance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const releaseCorpusRounds = 4

type releaseCorpusManifest struct {
	FormatVersion int                          `json:"format_version"`
	Corpora       []releaseCorpusManifestEntry `json:"corpora"`
}

type releaseCorpusManifestEntry struct {
	File           string  `json:"file"`
	SHA256         string  `json:"sha256"`
	ExpectedCases  int     `json:"expected_cases"`
	MinRecallAt1   float64 `json:"min_recall_at_1"`
	MinRecallAt5   float64 `json:"min_recall_at_5"`
	MaxRecallP95MS int64   `json:"max_recall_p95_ms"`
}

type releaseCorpusFixture struct {
	FormatVersion int                   `json:"format_version"`
	Cohort        string                `json:"cohort"`
	Source        string                `json:"source"`
	Sanitization  string                `json:"sanitization"`
	Series        []releaseCorpusSeries `json:"series"`
}

type releaseCorpusSeries struct {
	Language string              `json:"language"`
	Subjects []string            `json:"subjects"`
	Facts    []releaseCorpusFact `json:"facts"`
}

type releaseCorpusFact struct {
	Text  string `json:"text"`
	Query string `json:"query"`
}

type releaseCorpusCase struct {
	id        string
	cohort    string
	text      string
	query     string
	receiptID v1alpha1.ReceiptID
}

type releaseCorpusResult struct {
	cases       int
	at1         int
	at5         int
	latencies   []time.Duration
	provenance  int
	zeroResults int
}

// TestReleaseMultilingualCorpusGate is the reproducible package-release
// retrieval and partition gate. The checked-in fixtures contain authored,
// de-identified product-shaped facts; no private source text is committed.
func TestReleaseMultilingualCorpusGate(t *testing.T) {
	manifest := loadReleaseCorpusManifest(t)
	fixtures := make(map[string]releaseCorpusFixture, len(manifest.Corpora))
	casesByCohort := make(map[string][]releaseCorpusCase, len(manifest.Corpora))
	for _, entry := range manifest.Corpora {
		fixture, cases := loadReleaseCorpusFixture(t, entry)
		if _, duplicate := fixtures[fixture.Cohort]; duplicate {
			t.Fatalf("duplicate release corpus cohort %q", fixture.Cohort)
		}
		fixtures[fixture.Cohort] = fixture
		casesByCohort[fixture.Cohort] = cases
	}

	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()
	store, credentials := bootstrapFixture(t, Options{DataDir: dataDir, Clock: func() time.Time { return now }})
	t.Cleanup(func() { _ = store.Close() })
	operations := []v1alpha1.Operation{
		v1alpha1.OperationRemember,
		v1alpha1.OperationRecall,
		v1alpha1.OperationReceiptStatus,
	}
	issue := func(grant, actor string, labels v1alpha1.LabelSet) v1alpha1.CallAuthorization {
		request := issueRequest(v1alpha1.GrantID(grant), actor, v1alpha1.AudiencePrivate, operations)
		request.Labels = labels
		capability := mustIssue(t, store, credentials["principal:"+actor], request)
		return callAuth(capability, actor, v1alpha1.AudiencePrivate)
	}
	authorizations := make(map[string]v1alpha1.CallAuthorization, len(fixtures))
	for cohort := range fixtures {
		authorizations[cohort] = issue("grant-bot-a", "actor-bot-a", releaseCorpusLabels(cohort))
	}
	otherLabelAuth := issue("grant-bot-a", "actor-bot-a", v1alpha1.LabelSet{"language:zh", "workspace:decoy"})
	otherSpaceAuth := issue("grant-bot-b", "actor-bot-b", releaseCorpusLabels("zh"))

	firstChinese := casesByCohort["zh"][0]
	labelDecoy, err := store.Remember(t.Context(), otherLabelAuth, v1alpha1.RememberRequest{
		Text: firstChinese.text + " 这条同空间干扰项不得跨越 LabelSet。", IdempotencyKey: "release-corpus-label-decoy",
	})
	if err != nil {
		t.Fatal(err)
	}
	spaceDecoy, err := store.Remember(t.Context(), otherSpaceAuth, v1alpha1.RememberRequest{
		Text: firstChinese.text + " 这条同标签干扰项不得跨越 Space。", IdempotencyKey: "release-corpus-space-decoy",
	})
	if err != nil {
		t.Fatal(err)
	}

	for round := range releaseCorpusRounds {
		for cohort, cases := range casesByCohort {
			start, end := releaseCorpusRoundBounds(len(cases), round)
			for index := start; index < end; index++ {
				remembered, err := store.Remember(t.Context(), authorizations[cohort], v1alpha1.RememberRequest{
					Text: cases[index].text, IdempotencyKey: "release-corpus-" + cases[index].id,
					SourceContext: v1alpha1.SourceContext{SourceType: "release_corpus_fixture"},
				})
				if err != nil {
					t.Fatalf("Remember %s: %v", cases[index].id, err)
				}
				cases[index].receiptID = remembered.ReceiptID
				response, err := store.Recall(t.Context(), authorizations[cohort], testRecall(cases[index].query, remembered.ConsistencyToken))
				if err != nil || releaseCorpusRank(response, cases[index]) != 1 {
					t.Fatalf("immediate Recall %s = %+v, %v", cases[index].id, response, err)
				}
			}
			casesByCohort[cohort] = cases
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		store, err = Open(t.Context(), Options{DataDir: dataDir, Clock: func() time.Time { return now }})
		if err != nil {
			t.Fatalf("restart after release corpus round %d: %v", round+1, err)
		}
	}

	for _, entry := range manifest.Corpora {
		cohort := fixtureCohortForFile(t, fixtures, entry.File)
		result := evaluateReleaseCorpus(t, store, authorizations[cohort], casesByCohort[cohort])
		at1 := float64(result.at1) / float64(result.cases)
		at5 := float64(result.at5) / float64(result.cases)
		p95 := durationPercentile(result.latencies, 0.95)
		t.Logf("release corpus %s: cases=%d recall@1=%.4f recall@5=%.4f zero=%d provenance=%d p95=%s",
			cohort, result.cases, at1, at5, result.zeroResults, result.provenance, p95)
		if at1 < entry.MinRecallAt1 || at5 < entry.MinRecallAt5 || result.zeroResults != 0 || result.provenance != result.cases {
			t.Fatalf("release corpus %s quality gate failed: %+v", cohort, result)
		}
		if p95 > time.Duration(entry.MaxRecallP95MS)*time.Millisecond {
			t.Fatalf("release corpus %s Recall p95 %s exceeds %dms interactive budget", cohort, p95, entry.MaxRecallP95MS)
		}
	}

	assertReleaseCorpusPartitions(
		t, store, authorizations["zh"], otherLabelAuth, otherSpaceAuth,
		firstChinese, labelDecoy, spaceDecoy,
	)
}

func loadReleaseCorpusManifest(t *testing.T) releaseCorpusManifest {
	t.Helper()
	path := filepath.Join("testdata", "release_corpus", "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseCorpusManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != 1 || len(manifest.Corpora) != 3 {
		t.Fatalf("release corpus manifest = %+v", manifest)
	}
	return manifest
}

func loadReleaseCorpusFixture(t *testing.T, entry releaseCorpusManifestEntry) (releaseCorpusFixture, []releaseCorpusCase) {
	t.Helper()
	path := filepath.Join("testdata", "release_corpus", entry.File)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != entry.SHA256 {
		t.Fatalf("release corpus %s digest changed; review the privacy and quality delta, then update the manifest", entry.File)
	}
	var fixture releaseCorpusFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.FormatVersion != 1 || fixture.Cohort == "" || fixture.Source != "authored_deidentified_v1" ||
		fixture.Sanitization != "no_private_source_text_no_personal_identifiers_no_credentials" {
		t.Fatalf("release corpus %s provenance is incomplete: %+v", entry.File, fixture)
	}
	cases := expandReleaseCorpus(t, fixture)
	if len(cases) != entry.ExpectedCases || entry.MinRecallAt1 <= 0 || entry.MinRecallAt1 > 1 ||
		entry.MinRecallAt5 <= 0 || entry.MinRecallAt5 > 1 || entry.MaxRecallP95MS < 100 {
		t.Fatalf("release corpus %s manifest thresholds are invalid: %+v, cases=%d", entry.File, entry, len(cases))
	}
	return fixture, cases
}

func expandReleaseCorpus(t *testing.T, fixture releaseCorpusFixture) []releaseCorpusCase {
	t.Helper()
	result := make([]releaseCorpusCase, 0)
	seen := make(map[string]struct{})
	for _, series := range fixture.Series {
		if series.Language == "" || len(series.Subjects) == 0 || len(series.Facts) == 0 {
			t.Fatalf("release corpus %s has an empty series", fixture.Cohort)
		}
		for subjectIndex, subject := range series.Subjects {
			for factIndex, fact := range series.Facts {
				text := strings.ReplaceAll(fact.Text, "{subject}", subject)
				query := strings.ReplaceAll(fact.Query, "{subject}", subject)
				if text == fact.Text || query == fact.Query || !releaseCorpusTextIsSanitized(text) || !releaseCorpusTextIsSanitized(query) {
					t.Fatalf("release corpus %s contains an invalid or unsafe fixture: %q / %q", fixture.Cohort, text, query)
				}
				id := fmt.Sprintf("%s-%s-%02d-%02d", fixture.Cohort, series.Language, subjectIndex, factIndex)
				if _, duplicate := seen[id]; duplicate {
					t.Fatalf("duplicate release corpus case %q", id)
				}
				seen[id] = struct{}{}
				result = append(result, releaseCorpusCase{id: id, cohort: fixture.Cohort, text: text, query: query})
			}
		}
	}
	return result
}

func releaseCorpusTextIsSanitized(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1<<10 {
		return false
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"@", "://", "/users/", "c:\\users\\", "bearer ", "api_key", "secret="} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}

func releaseCorpusLabels(cohort string) v1alpha1.LabelSet {
	return v1alpha1.LabelSet{v1alpha1.Label("language:" + cohort), "workspace:release"}
}

func releaseCorpusRoundBounds(total, round int) (int, int) {
	batch := (total + releaseCorpusRounds - 1) / releaseCorpusRounds
	start := min(total, round*batch)
	return start, min(total, start+batch)
}

func fixtureCohortForFile(t *testing.T, fixtures map[string]releaseCorpusFixture, filename string) string {
	t.Helper()
	for cohort := range fixtures {
		if filename == cohort+".json" {
			return cohort
		}
	}
	t.Fatalf("manifest file %q has no matching cohort", filename)
	return ""
}

func evaluateReleaseCorpus(
	t *testing.T,
	store *Store,
	auth v1alpha1.CallAuthorization,
	cases []releaseCorpusCase,
) releaseCorpusResult {
	t.Helper()
	result := releaseCorpusResult{cases: len(cases), latencies: make([]time.Duration, 0, len(cases))}
	for _, item := range cases {
		status, err := store.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: item.receiptID})
		if err != nil || status.State == "" {
			t.Fatalf("durable ReceiptStatus %s = %+v, %v", item.id, status, err)
		}
		started := time.Now()
		response, err := store.Recall(t.Context(), auth, testRecall(item.query, ""))
		result.latencies = append(result.latencies, time.Since(started))
		if err != nil {
			t.Fatalf("Recall %s: %v", item.id, err)
		}
		if len(response.Fragments) == 0 {
			result.zeroResults++
			continue
		}
		rank := releaseCorpusRank(response, item)
		if rank == 1 {
			result.at1++
		}
		if rank > 0 && rank <= 5 {
			result.at5++
		}
		if rank > 0 {
			fragment := response.Fragments[rank-1]
			if slices.Contains(fragment.EvidenceRefs, item.receiptID) {
				result.provenance++
			}
		}
	}
	return result
}

func releaseCorpusRank(response v1alpha1.RecallResponse, item releaseCorpusCase) int {
	for index, fragment := range response.Fragments {
		if fragment.Text == item.text && slices.Contains(fragment.EvidenceRefs, item.receiptID) {
			return index + 1
		}
	}
	return 0
}

func durationPercentile(values []time.Duration, quantile float64) time.Duration {
	values = append([]time.Duration(nil), values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * quantile)
	return values[index]
}

func assertReleaseCorpusPartitions(
	t *testing.T,
	store *Store,
	targetAuth v1alpha1.CallAuthorization,
	otherLabelAuth v1alpha1.CallAuthorization,
	otherSpaceAuth v1alpha1.CallAuthorization,
	target releaseCorpusCase,
	labelDecoy v1alpha1.RememberResponse,
	spaceDecoy v1alpha1.RememberResponse,
) {
	t.Helper()
	response, err := store.Recall(t.Context(), targetAuth, testRecall(target.query, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range response.Fragments {
		if strings.Contains(fragment.Text, "干扰项") {
			t.Fatalf("release corpus Recall crossed Space or LabelSet: %+v", response.Fragments)
		}
	}
	if _, err := store.GetReceiptStatus(t.Context(), targetAuth, v1alpha1.GetReceiptStatusRequest{ReceiptID: labelDecoy.ReceiptID}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeNotFound) {
		t.Fatalf("cross-LabelSet ReceiptStatus error = %v, want not_found", err)
	}
	if _, err := store.GetReceiptStatus(t.Context(), targetAuth, v1alpha1.GetReceiptStatusRequest{ReceiptID: spaceDecoy.ReceiptID}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeNotFound) {
		t.Fatalf("cross-Space ReceiptStatus error = %v, want not_found", err)
	}
	if _, err := store.Recall(t.Context(), targetAuth, testRecall(target.query, labelDecoy.ConsistencyToken)); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnauthorized) {
		t.Fatalf("cross-LabelSet consistency token error = %v, want unauthorized", err)
	}
	if _, err := store.Recall(t.Context(), targetAuth, testRecall(target.query, spaceDecoy.ConsistencyToken)); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnauthorized) {
		t.Fatalf("cross-Space consistency token error = %v, want unauthorized", err)
	}
	for name, isolated := range map[string]struct {
		auth v1alpha1.CallAuthorization
		text string
	}{
		"LabelSet": {auth: otherLabelAuth, text: target.text + " 这条同空间干扰项不得跨越 LabelSet。"},
		"Space":    {auth: otherSpaceAuth, text: target.text + " 这条同标签干扰项不得跨越 Space。"},
	} {
		isolatedResponse, err := store.Recall(t.Context(), isolated.auth, testRecall(target.query, ""))
		if err != nil {
			t.Fatalf("%s decoy Recall: %v", name, err)
		}
		found := false
		for _, fragment := range isolatedResponse.Fragments {
			found = found || fragment.Text == isolated.text
		}
		if !found {
			t.Fatalf("%s decoy was not searchable in its own partition: %+v", name, isolatedResponse.Fragments)
		}
	}
}
