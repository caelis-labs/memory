package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

func evaluateLegacyUnicode61(ctx context.Context, cases []evaluationCase) (retrievalQualityReport, error) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return retrievalQualityReport{}, err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(ctx,
		`CREATE VIRTUAL TABLE legacy_fts USING fts5(case_id UNINDEXED, text, tokenize = 'unicode61')`); err != nil {
		return retrievalQualityReport{}, fmt.Errorf("create legacy evaluation index: %w", err)
	}
	for index, test := range cases {
		if _, err := database.ExecContext(ctx,
			`INSERT INTO legacy_fts(case_id, text) VALUES (?, ?)`, index, test.text); err != nil {
			return retrievalQualityReport{}, fmt.Errorf("index legacy evaluation case: %w", err)
		}
	}
	report := retrievalQualityReport{Queries: len(cases)}
	reciprocalRankSum := 0.0
	zero := 0
	for target, test := range cases {
		query := legacyFTSQuery(test.query)
		if query == "" {
			zero++
			continue
		}
		rows, err := database.QueryContext(ctx,
			`SELECT case_id FROM legacy_fts WHERE legacy_fts MATCH ?
			 ORDER BY bm25(legacy_fts), case_id DESC LIMIT 8`, query)
		if err != nil {
			return retrievalQualityReport{}, fmt.Errorf("query legacy evaluation index: %w", err)
		}
		position := -1
		returned := 0
		for rows.Next() {
			var caseID int
			if err := rows.Scan(&caseID); err != nil {
				_ = rows.Close()
				return retrievalQualityReport{}, fmt.Errorf("read legacy evaluation result: %w", err)
			}
			if caseID == target && position == -1 {
				position = returned
			}
			returned++
		}
		if err := rows.Close(); err != nil {
			return retrievalQualityReport{}, fmt.Errorf("close legacy evaluation result: %w", err)
		}
		if err := rows.Err(); err != nil {
			return retrievalQualityReport{}, fmt.Errorf("query legacy evaluation index: %w", err)
		}
		if returned == 0 {
			zero++
		}
		if position >= 0 {
			report.RetrievalAt8++
			reciprocalRankSum += 1 / float64(position+1)
			if position == 0 {
				report.RecallAt1++
			}
			if position < 5 {
				report.RecallAt5++
			}
		}
	}
	if len(cases) == 0 {
		return report, nil
	}
	count := float64(len(cases))
	report.RetrievalAt8 /= count
	report.RecallAt1 /= count
	report.RecallAt5 /= count
	report.MRR = reciprocalRankSum / count
	report.ZeroResultRate = float64(zero) / count
	return report, nil
}

func legacyFTSQuery(query string) string {
	terms := strings.Fields(query)
	quoted := make([]string, 0, min(len(terms), 64))
	for _, term := range terms {
		if len(quoted) == 64 {
			break
		}
		if !strings.ContainsFunc(term, func(value rune) bool {
			return unicode.IsLetter(value) || unicode.IsNumber(value)
		}) {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}
