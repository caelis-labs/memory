package appliance

import (
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/go-ego/gse"
)

const (
	maxLexicalTerms           = 256
	maxLexicalNGrams          = 512
	lexicalFirstTermWeight    = 2.0
	lexicalNGramWeight        = 0.25
	lexicalExactPhraseWeight  = 4.0
	lexicalPrivateTermWeight  = 3.0
	lexicalBM25TieBreakWeight = 0.001
)

var (
	baseSegmenter     gse.Segmenter
	baseSegmenterErr  error
	baseSegmenterOnce sync.Once
)

// lexicalDocument is a rebuildable projection. Text remains the immutable
// source; terms and n-grams only make the source searchable with a tokenizer
// that otherwise treats an unspaced Chinese sentence as one token.
type lexicalDocument struct {
	terms  string
	ngrams string
}

func projectLexical(text string, privateTerms []string) (lexicalDocument, error) {
	segmenter, err := sharedBaseSegmenter()
	if err != nil {
		return lexicalDocument{}, err
	}
	terms := newOrderedTerms(maxLexicalTerms)
	for _, token := range segmenter.CutSearch(text, false) {
		terms.add(token)
	}
	for _, token := range lexicalAtoms(text) {
		terms.add(token)
	}
	for _, term := range privateTerms {
		if term = normalizeLexicalToken(term); term != "" && strings.Contains(strings.ToLower(text), term) {
			terms.add(term)
		}
	}

	ngrams := newOrderedTerms(maxLexicalNGrams)
	for _, run := range hanRuns(text) {
		runes := []rune(run)
		for size := 2; size <= 3; size++ {
			for start := 0; start+size <= len(runes); start++ {
				ngrams.add(string(runes[start : start+size]))
			}
		}
	}
	return lexicalDocument{
		terms:  strings.Join(terms.values, " "),
		ngrams: strings.Join(ngrams.values, " "),
	}, nil
}

func lexicalFTSQuery(query string, privateTerms []string) (string, error) {
	document, err := projectLexical(query, privateTerms)
	if err != nil {
		return "", err
	}
	terms := newOrderedTerms(128)
	for _, field := range []string{document.terms, document.ngrams} {
		for _, token := range strings.Fields(field) {
			terms.add(token)
		}
	}
	quoted := make([]string, 0, len(terms.values))
	for _, term := range terms.values {
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR "), nil
}

// lexicalRank makes query coverage the primary signal and retains SQLite's
// corpus-level BM25 as a deterministic tie-breaker. The first query term is
// intentionally more valuable: callers and models usually put the most
// discriminating entity before qualifiers such as a release stage or date.
func lexicalRank(query, text string, privateTerms []string, bm25 float64) (float64, error) {
	queryDocument, err := projectLexical(query, privateTerms)
	if err != nil {
		return 0, err
	}
	textDocument, err := projectLexical(text, privateTerms)
	if err != nil {
		return 0, err
	}
	textTerms := fieldSet(textDocument.terms)
	textNGrams := fieldSet(textDocument.ngrams)
	score := 0.0
	for index, term := range strings.Fields(queryDocument.terms) {
		if _, found := textTerms[term]; !found {
			continue
		}
		weight := 1.0
		if index == 0 {
			weight = lexicalFirstTermWeight
		}
		score += weight
	}
	for _, term := range strings.Fields(queryDocument.ngrams) {
		if _, found := textNGrams[term]; found {
			score += lexicalNGramWeight
		}
	}
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	if normalizedQuery != "" && strings.Contains(strings.ToLower(text), normalizedQuery) {
		score += lexicalExactPhraseWeight
	}
	for _, term := range privateTerms {
		term = normalizeLexicalToken(term)
		if term != "" && strings.Contains(normalizedQuery, term) && strings.Contains(strings.ToLower(text), term) {
			score += lexicalPrivateTermWeight
		}
	}
	return -score + bm25*lexicalBM25TieBreakWeight, nil
}

func fieldSet(value string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range strings.Fields(value) {
		result[item] = struct{}{}
	}
	return result
}

func sharedBaseSegmenter() (*gse.Segmenter, error) {
	baseSegmenterOnce.Do(func() {
		baseSegmenter, baseSegmenterErr = gse.NewEmbed("zh_s")
	})
	if baseSegmenterErr != nil {
		return nil, fmt.Errorf("load embedded Chinese dictionary: %w", baseSegmenterErr)
	}
	return &baseSegmenter, nil
}

type orderedTerms struct {
	limit  int
	seen   map[string]struct{}
	values []string
}

func newOrderedTerms(limit int) *orderedTerms {
	return &orderedTerms{limit: limit, seen: make(map[string]struct{}, limit)}
}

func (t *orderedTerms) add(value string) {
	if len(t.values) == t.limit {
		return
	}
	value = normalizeLexicalToken(value)
	if value == "" {
		return
	}
	if _, found := t.seen[value]; found {
		return
	}
	t.seen[value] = struct{}{}
	t.values = append(t.values, value)
}

func normalizeLexicalToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsNumber(r)
	}) {
		return ""
	}
	if utf8.RuneCountInString(value) == 1 {
		for _, r := range value {
			if unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r) {
				return ""
			}
		}
	}
	return value
}

func lexicalAtoms(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func hanRuns(value string) []string {
	runs := make([]string, 0)
	var current strings.Builder
	flush := func() {
		if current.Len() != 0 {
			runs = append(runs, current.String())
			current.Reset()
		}
	}
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return runs
}
