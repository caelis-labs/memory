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
