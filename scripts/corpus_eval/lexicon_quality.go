package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/go-ego/gse"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

var (
	evaluationSegmenter     gse.Segmenter
	evaluationSegmenterErr  error
	evaluationSegmenterOnce sync.Once
)

type qualityObservation struct {
	term        string
	occurrences int
	left        map[string]struct{}
	right       map[string]struct{}
	relevant    map[int]struct{}
}

type lexiconQualityCase struct {
	term     string
	relevant map[v1alpha1.ReceiptID]struct{}
}

func evaluateLexiconQuality(
	ctx context.Context,
	store *appliance.Store,
	privateAuth v1alpha1.CallAuthorization,
	isolatedAuth v1alpha1.CallAuthorization,
	cases []evaluationCase,
	policy appliance.LexiconPolicy,
) (lexiconQualityReport, error) {
	queries, err := buildLexiconQualityCases(cases, policy)
	if err != nil {
		return lexiconQualityReport{}, err
	}
	report := lexiconQualityReport{Queries: len(queries)}
	if len(queries) == 0 {
		return report, nil
	}
	reciprocalRankSum := 0.0
	precisionSum := 0.0
	zero := 0
	for _, query := range queries {
		report.RelevantReceipts += len(query.relevant)
		request := v1alpha1.RecallRequest{
			Query:         query.term,
			Budget:        v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 64 << 10, DeadlineMS: 5_000},
			SourceContext: v1alpha1.SourceContext{SourceType: "local_lexicon_evaluation"},
		}
		response, err := store.Recall(ctx, privateAuth, request)
		if err != nil {
			return lexiconQualityReport{}, fmt.Errorf("Recall lexicon quality query: %w", err)
		}
		if len(response.Fragments) == 0 {
			zero++
		}
		firstRelevant := -1
		relevantAt5 := 0
		for index, fragment := range response.Fragments {
			if !fragmentHasRelevantEvidence(fragment.EvidenceRefs, query.relevant) {
				continue
			}
			if firstRelevant == -1 {
				firstRelevant = index
			}
			if index < 5 {
				relevantAt5++
			}
		}
		if firstRelevant >= 0 {
			report.RetrievalAt8++
			reciprocalRankSum += 1 / float64(firstRelevant+1)
			if firstRelevant == 0 {
				report.RecallAt1++
			}
			if firstRelevant < 5 {
				report.RecallAt5++
			}
		}
		denominator := min(5, len(response.Fragments))
		if denominator > 0 {
			precisionSum += float64(relevantAt5) / float64(denominator)
		}
		isolated, err := store.Recall(ctx, isolatedAuth, request)
		if err != nil {
			return lexiconQualityReport{}, fmt.Errorf("Recall isolated lexicon quality query: %w", err)
		}
		if len(isolated.Fragments) != 0 {
			report.PrivateLeakageCount++
		}
	}
	count := float64(len(queries))
	report.RetrievalAt8 /= count
	report.RecallAt1 /= count
	report.RecallAt5 /= count
	report.MRR = reciprocalRankSum / count
	report.MeanPrecisionAt5 = precisionSum / count
	report.ZeroResultRate = float64(zero) / count
	return report, nil
}

func buildLexiconQualityCases(cases []evaluationCase, policy appliance.LexiconPolicy) ([]lexiconQualityCase, error) {
	aggregates := make(map[string]*qualityObservation)
	for index, test := range cases {
		observations, err := discoverQualityCandidates(test.text, policy)
		if err != nil {
			return nil, err
		}
		for _, observation := range observations {
			item := aggregates[observation.term]
			if item == nil {
				item = &qualityObservation{
					term: observation.term, left: make(map[string]struct{}), right: make(map[string]struct{}),
					relevant: make(map[int]struct{}),
				}
				aggregates[observation.term] = item
			}
			item.occurrences += observation.occurrences
			item.relevant[index] = struct{}{}
			for value := range observation.left {
				item.left[value] = struct{}{}
			}
			for value := range observation.right {
				item.right[value] = struct{}{}
			}
		}
	}
	selected := make([]*qualityObservation, 0)
	for _, item := range aggregates {
		documents := len(item.relevant)
		score := 2*math.Log1p(float64(documents)) + math.Log1p(float64(item.occurrences)) +
			0.5*float64(min(len(item.left), 4)+min(len(item.right), 4))
		if documents < policy.MinDocumentFrequency ||
			len(item.left) < policy.MinBoundaryDiversity || len(item.right) < policy.MinBoundaryDiversity ||
			score < policy.MinActivationScore ||
			float64(item.occurrences)/float64(documents) > policy.MaxAverageOccurrences {
			continue
		}
		selected = append(selected, item)
	}
	sort.Slice(selected, func(i, j int) bool {
		if len(selected[i].relevant) != len(selected[j].relevant) {
			return len(selected[i].relevant) > len(selected[j].relevant)
		}
		return selected[i].term < selected[j].term
	})
	if len(selected) > 256 {
		selected = selected[:256]
	}
	result := make([]lexiconQualityCase, 0, len(selected))
	for _, item := range selected {
		qualityCase := lexiconQualityCase{term: item.term, relevant: make(map[v1alpha1.ReceiptID]struct{}, len(item.relevant))}
		for index := range item.relevant {
			qualityCase.relevant[cases[index].receiptID] = struct{}{}
		}
		result = append(result, qualityCase)
	}
	return result, nil
}

func discoverQualityCandidates(text string, policy appliance.LexiconPolicy) ([]qualityObservation, error) {
	segmenter, err := evaluationBaseSegmenter()
	if err != nil {
		return nil, err
	}
	observations := make(map[string]*qualityObservation)
	for _, run := range evaluationHanRuns(text) {
		runes := []rune(run)
		for size := policy.MinTermRunes; size <= policy.MaxTermRunes; size++ {
			for start := 0; start+size <= len(runes); start++ {
				term := string(runes[start : start+size])
				if evaluationBaseContains(segmenter, term) {
					continue
				}
				item := observations[term]
				if item == nil {
					item = &qualityObservation{term: term, left: make(map[string]struct{}), right: make(map[string]struct{})}
					observations[term] = item
				}
				item.occurrences++
				left := "^"
				if start > 0 {
					left = string(runes[start-1])
				}
				right := "$"
				if start+size < len(runes) {
					right = string(runes[start+size])
				}
				item.left[left] = struct{}{}
				item.right[right] = struct{}{}
			}
		}
	}
	result := make([]qualityObservation, 0, len(observations))
	for _, item := range observations {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].occurrences != result[j].occurrences {
			return result[i].occurrences > result[j].occurrences
		}
		iRunes, jRunes := utf8.RuneCountInString(result[i].term), utf8.RuneCountInString(result[j].term)
		if iRunes != jRunes {
			return iRunes > jRunes
		}
		return result[i].term < result[j].term
	})
	if len(result) > policy.MaxCandidatesPerText {
		result = result[:policy.MaxCandidatesPerText]
	}
	return result, nil
}

func evaluationBaseSegmenter() (*gse.Segmenter, error) {
	evaluationSegmenterOnce.Do(func() {
		evaluationSegmenter, evaluationSegmenterErr = gse.NewEmbed("zh_s")
	})
	return &evaluationSegmenter, evaluationSegmenterErr
}

func evaluationBaseContains(segmenter *gse.Segmenter, term string) bool {
	pieces := segmenter.Cut(term, false)
	meaningful := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		piece = strings.ToLower(strings.TrimSpace(piece))
		if piece != "" && strings.ContainsFunc(piece, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
			meaningful = append(meaningful, piece)
		}
	}
	return len(meaningful) == 1 && meaningful[0] == term
}

func evaluationHanRuns(value string) []string {
	var runs []string
	var current strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			current.WriteRune(r)
			continue
		}
		if current.Len() != 0 {
			runs = append(runs, current.String())
			current.Reset()
		}
	}
	if current.Len() != 0 {
		runs = append(runs, current.String())
	}
	return runs
}

func fragmentHasRelevantEvidence(values []v1alpha1.ReceiptID, relevant map[v1alpha1.ReceiptID]struct{}) bool {
	for _, value := range values {
		if _, found := relevant[value]; found {
			return true
		}
	}
	return false
}
