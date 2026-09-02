package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSourceUnderstandsMarkdownAndCodexMessages(t *testing.T) {
	markdownPath := filepath.Join(t.TempDir(), "MEMORY.md")
	markdown := "# Durable preferences\n- The workspace uses markeralpha for release checks.\n```\nsecret code block markerignored\n```\n"
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o600); err != nil {
		t.Fatal(err)
	}
	markdownSource, err := loadSource(markdownPath, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if markdownSource.kind != "markdown" || len(markdownSource.chunks) != 2 || strings.Contains(strings.Join(markdownSource.chunks, " "), "markerignored") {
		t.Fatalf("Markdown source shape = kind %q chunks %d", markdownSource.kind, len(markdownSource.chunks))
	}
	jsonlPath := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"developer markerignored"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"User decision markerbravo stays private."}]}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","content":"tool markerignored"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Assistant summary markercharlie is retained."}]}}`,
	}
	if err := os.WriteFile(jsonlPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jsonlSource, err := loadSource(jsonlPath, "auto")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(jsonlSource.chunks, " ")
	if jsonlSource.kind != "codex-jsonl" || len(jsonlSource.chunks) != 2 || strings.Contains(joined, "markerignored") {
		t.Fatalf("JSONL source shape = kind %q chunks %d", jsonlSource.kind, len(jsonlSource.chunks))
	}
}

func TestLoadSourceUnderstandsCanonicalCaelisMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.events.jsonl")
	lines := []string{
		`{"type":"user","visibility":"canonical","message":{"role":"user","parts":[{"kind":"text","text":"User decision caelismarkeralpha remains durable."}]}}`,
		`{"type":"assistant","visibility":"canonical","message":{"role":"assistant","parts":[{"kind":"reasoning","reasoning":"private markerignored"},{"kind":"text","text":{"text":"Assistant summary caelismarkerbravo is retained."}},{"kind":"tool_use","tool_use":{"name":"read"}}]}}`,
		`{"type":"tool_result","visibility":"canonical","message":{"role":"tool","parts":[{"kind":"text","text":"tool markerignored"}]}}`,
		`{"type":"assistant","visibility":"transient","message":{"role":"assistant","parts":[{"kind":"text","text":"transient markerignored"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := loadSource(path, "caelis-jsonl")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(source.chunks, " ")
	if source.kind != "caelis-jsonl" || len(source.chunks) != 2 || strings.Contains(joined, "markerignored") {
		t.Fatalf("Caelis JSONL source shape = kind %q chunks %d", source.kind, len(source.chunks))
	}
}

func TestEvaluateReportsMultiRoundDurabilityWithoutSourceText(t *testing.T) {
	chunks := make([]string, 0, 18)
	for index := range 18 {
		chunks = append(chunks, fmt.Sprintf("Fictional project decision uniquemarker%03d remains valid across later sessions.", index))
	}
	source := sourceData{
		kind: "markdown", digest: strings.Repeat("a", 64), bytes: 4096,
		extracted: len(chunks), chunks: chunks,
	}
	report, err := evaluate(context.Background(), source, options{dataDir: t.TempDir(), rounds: 3, limit: len(chunks)})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rounds) != 3 || report.Final.Receipts != len(chunks) || report.Final.DurableReceiptRate != 1 || report.Final.RetrievalAt8 != 1 || report.Final.RecallAt1 != 1 || report.Final.RecallAt5 != 1 {
		t.Fatalf("evaluation summary = %+v", report)
	}
	if report.Source.UniqueQueryCases != len(chunks) || report.Source.CollidingQueryCases != 0 || report.Source.MaxQueryDocumentFrequency != 1 {
		t.Fatalf("query shape = %+v", report.Source)
	}
	if report.Final.PrivateLeakageCount != 0 || len(report.Final.Cohorts) != 3 {
		t.Fatalf("evaluation isolation/cohorts = %+v", report.Final)
	}
	if report.FormatVersion != 2 || report.Configuration.RetrievalPolicy.Analyzer == "" ||
		report.Configuration.RetrievalPolicy.ExactPhraseWeight != 4 ||
		report.Configuration.LexiconPolicy.MinDocumentFrequency != 3 ||
		report.Configuration.LexiconPolicy.Enabled {
		t.Fatalf("evaluation policy = format %d config %+v", report.FormatVersion, report.Configuration)
	}
	if report.Final.Lexicon.CandidateTerms != 0 || report.Final.Lexicon.ActiveTerms != 0 {
		t.Fatalf("default evaluation unexpectedly enabled adaptive lexicon: %+v", report.Final.Lexicon)
	}
	for _, round := range report.Rounds {
		if round.ImmediateRetrievalAt8 != 1 || round.ImmediateRecallAt1 != 1 || round.PostRestartDurableReceiptRate != 1 || round.PostRestartRetrievalAt8 != 1 || round.PostRestartRecallAt1 != 1 || round.PrivateLeakageCount != 0 {
			t.Fatalf("round report = %+v", round)
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("uniquemarker")) || bytes.Contains(encoded, []byte("Fictional project")) {
		t.Fatal("aggregate report contains source text")
	}
}

func TestRunRequiresExplicitExperimentalLexiconOptIn(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "MEMORY.md")
	if err := os.WriteFile(sourcePath, []byte(strings.Repeat("- durable fact marker remains valid\n", 12)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"-source", sourcePath, "-lexicon-min-docs", "2"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "require -experimental-lexicon") {
		t.Fatalf("run without experimental opt-in error = %v", err)
	}
}

func TestEvaluateShowsChineseSubstringLiftOverUnicode61(t *testing.T) {
	phrases := []string{
		"蓝鲸协议", "星河网关", "月桂索引", "松涛队列", "云杉缓存", "白鹭审计",
		"玄武调度", "朱雀发布", "青龙存储", "琥珀追踪", "珊瑚备份", "翡翠路由",
	}
	chunks := make([]string, 0, len(phrases))
	for index, phrase := range phrases {
		chunks = append(chunks, fmt.Sprintf("第%02d组确认%s已经完成多轮验证并保留全部记录", index, phrase))
	}
	report, err := evaluate(t.Context(), sourceData{
		kind: "markdown", digest: strings.Repeat("b", 64), bytes: 4096,
		extracted: len(chunks), chunks: chunks,
	}, options{dataDir: t.TempDir(), rounds: 3, limit: len(chunks)})
	if err != nil {
		t.Fatal(err)
	}
	if report.Final.RecallAt5 != 1 || report.Final.ZeroResultRate != 0 {
		t.Fatalf("Chinese retrieval = %+v", report.Final)
	}
	if report.Final.RecallAt5Lift <= 0 || report.Final.LegacyUnicode61.ZeroResultRate == 0 {
		t.Fatalf("Unicode61 comparison = current r@5 %.3f legacy %+v lift %.3f",
			report.Final.RecallAt5, report.Final.LegacyUnicode61, report.Final.RecallAt5Lift)
	}
}

func TestEvaluationTokensIncludeBoundedHanSubstrings(t *testing.T) {
	tokens := evaluationTokens("团队会议安排在周三并开展代码评审")
	joined := " " + strings.Join(tokens, " ") + " "
	for _, want := range []string{" 周三 ", " 代码评审 ", " 团队会 "} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tokens %q do not contain %q", tokens, strings.TrimSpace(want))
		}
	}
	for _, token := range tokens {
		if len([]rune(token)) > 4 {
			t.Fatalf("unbounded Han query token %q", token)
		}
	}
}

func TestRunWritesOwnerOnlyAggregateReport(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "MEMORY.md")
	var source strings.Builder
	for index := range 12 {
		fmt.Fprintf(&source, "- Private memory uniquefact%03d is durable across rounds.\n", index)
	}
	if err := os.WriteFile(sourcePath, []byte(source.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "report.json")
	if err := run(t.Context(), []string{"-source", sourcePath, "-rounds", "3", "-limit", "12", "-output", outputPath}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("report mode = %o", info.Mode().Perm())
	}
}
