package appliance

import (
	"strings"
	"testing"
)

func TestProjectLexicalChineseAndMixedText(t *testing.T) {
	projection, err := projectLexical("项目技术栈采用 React、PostgreSQL 和向量数据库。", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"项目", "技术", "react", "postgresql", "数据库"} {
		if !strings.Contains(projection.terms, want) {
			t.Fatalf("terms %q do not contain %q", projection.terms, want)
		}
	}
	for _, want := range []string{"项目", "技术", "术栈", "向量", "数据", "据库"} {
		if !strings.Contains(projection.ngrams, want) {
			t.Fatalf("ngrams %q do not contain %q", projection.ngrams, want)
		}
	}
}

func TestProjectLexicalAddsOnlyPresentPrivateTerms(t *testing.T) {
	projection, err := projectLexical("量子织网协议已经完成灰度发布。", []string{"量子织网", "不存在的词"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(projection.terms, "量子织网") {
		t.Fatalf("terms %q do not contain private term", projection.terms)
	}
	if strings.Contains(projection.terms, "不存在的词") {
		t.Fatalf("terms %q contain absent private term", projection.terms)
	}
}

func TestLexicalFTSQueryIsBoundedAndEscaped(t *testing.T) {
	query, err := lexicalFTSQuery(`项目 "技术栈" PostgreSQL`, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"项目"`, `"技术"`, `"postgresql"`} {
		if !strings.Contains(query, want) {
			t.Fatalf("query %q does not contain %q", query, want)
		}
	}
	if strings.Count(query, " OR ") >= 128 {
		t.Fatalf("query has too many terms: %q", query)
	}
}
