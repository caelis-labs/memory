package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

type evaluationCase struct {
	text                   string
	query                  string
	queryDocumentFrequency int
	receiptID              v1alpha1.ReceiptID
	token                  v1alpha1.ConsistencyToken
	round                  int
}

type evaluationReport struct {
	FormatVersion int                 `json:"format_version"`
	Mode          string              `json:"mode"`
	Source        sourceReport        `json:"source"`
	Configuration configurationReport `json:"configuration"`
	Rounds        []roundReport       `json:"rounds"`
	Final         finalSnapshotReport `json:"final_snapshot"`
	DataRetained  bool                `json:"data_retained"`
}

type sourceReport struct {
	Kind                      string `json:"kind"`
	SHA256                    string `json:"sha256"`
	Bytes                     int64  `json:"bytes"`
	ExtractedLines            int    `json:"extracted_lines"`
	EligibleCases             int    `json:"eligible_cases"`
	UniqueQueryCases          int    `json:"unique_query_cases"`
	CollidingQueryCases       int    `json:"colliding_query_cases"`
	MaxQueryDocumentFrequency int    `json:"max_query_document_frequency"`
	DuplicateLines            int    `json:"duplicate_lines"`
	SkippedLines              int    `json:"skipped_lines"`
}

type configurationReport struct {
	Rounds             int  `json:"rounds"`
	RequestedLimit     int  `json:"requested_limit"`
	RecallMaxFragments int  `json:"recall_max_fragments"`
	RestartAfterWrites bool `json:"restart_after_writes"`
}

type roundReport struct {
	Round                         int     `json:"round"`
	NewReceipts                   int     `json:"new_receipts"`
	CumulativeReceipts            int     `json:"cumulative_receipts"`
	ImmediateQueries              int     `json:"immediate_queries"`
	ImmediateRetrievalAt8         float64 `json:"immediate_retrieval_at_8"`
	ImmediateRecallAt1            float64 `json:"immediate_recall_at_1"`
	ImmediateRecallAt5            float64 `json:"immediate_recall_at_5"`
	PostRestartQueries            int     `json:"post_restart_queries"`
	PostRestartDurableReceiptRate float64 `json:"post_restart_durable_receipt_rate"`
	PostRestartRetrievalAt8       float64 `json:"post_restart_retrieval_at_8"`
	PostRestartRecallAt1          float64 `json:"post_restart_recall_at_1"`
	PostRestartRecallAt5          float64 `json:"post_restart_recall_at_5"`
	PrivateLeakageCount           int     `json:"private_leakage_count"`
	RememberP50Microseconds       int64   `json:"remember_p50_us"`
	RememberP95Microseconds       int64   `json:"remember_p95_us"`
	RememberP99Microseconds       int64   `json:"remember_p99_us"`
	RecallP50Microseconds         int64   `json:"recall_p50_us"`
	RecallP95Microseconds         int64   `json:"recall_p95_us"`
	RecallP99Microseconds         int64   `json:"recall_p99_us"`
}

type finalSnapshotReport struct {
	Receipts            int            `json:"receipts"`
	DurableReceiptRate  float64        `json:"durable_receipt_rate"`
	RetrievalAt8        float64        `json:"retrieval_at_8"`
	RecallAt1           float64        `json:"recall_at_1"`
	RecallAt5           float64        `json:"recall_at_5"`
	PrivateLeakageCount int            `json:"private_leakage_count"`
	Cohorts             []cohortReport `json:"cohorts"`
}

type cohortReport struct {
	InsertedRound      int     `json:"inserted_round"`
	AgeRounds          int     `json:"age_rounds"`
	Cases              int     `json:"cases"`
	DurableReceiptRate float64 `json:"durable_receipt_rate"`
	RetrievalAt8       float64 `json:"retrieval_at_8"`
	RecallAt1          float64 `json:"recall_at_1"`
	RecallAt5          float64 `json:"recall_at_5"`
}

type queryResult struct {
	durable bool
	visible bool
	at1     bool
	at5     bool
	leaked  bool
	latency time.Duration
}

func evaluate(ctx context.Context, source sourceData, opts options) (evaluationReport, error) {
	cases := buildEvaluationCases(source.chunks, opts.limit)
	if len(cases) < opts.rounds {
		return evaluationReport{}, fmt.Errorf("source produced %d eligible cases; at least %d are required", len(cases), opts.rounds)
	}
	dataDir := opts.dataDir
	removeData := false
	if dataDir == "" {
		var err error
		dataDir, err = os.MkdirTemp("", "memory-corpus-eval-*")
		if err != nil {
			return evaluationReport{}, fmt.Errorf("create temporary evaluation data: %w", err)
		}
		removeData = true
	}
	if removeData {
		defer removeTemporaryData(dataDir)
	}
	store, privateAuth, isolatedAuth, err := bootstrapEvaluationStore(ctx, dataDir)
	if err != nil {
		return evaluationReport{}, err
	}
	defer func() { _ = store.Close() }()
	uniqueQueries, collidingQueries, maxQueryDocumentFrequency := queryShape(cases)
	report := evaluationReport{
		FormatVersion: 1,
		Mode:          "durable_private_receipt_lexical",
		Source: sourceReport{
			Kind: source.kind, SHA256: source.digest, Bytes: source.bytes,
			ExtractedLines: source.extracted, EligibleCases: len(cases),
			UniqueQueryCases: uniqueQueries, CollidingQueryCases: collidingQueries,
			MaxQueryDocumentFrequency: maxQueryDocumentFrequency,
			DuplicateLines:            source.duplicates, SkippedLines: source.skipped,
		},
		Configuration: configurationReport{
			Rounds: opts.rounds, RequestedLimit: opts.limit, RecallMaxFragments: 8, RestartAfterWrites: true,
		},
		Rounds:       make([]roundReport, 0, opts.rounds),
		DataRetained: !removeData,
	}
	batchSize := (len(cases) + opts.rounds - 1) / opts.rounds
	visible := 0
	var finalResults []queryResult
	for round := 1; round <= opts.rounds && visible < len(cases); round++ {
		end := min(visible+batchSize, len(cases))
		rememberLatencies := make([]time.Duration, 0, end-visible)
		immediateVisible, immediateAt1, immediateAt5 := 0, 0, 0
		for index := visible; index < end; index++ {
			cases[index].round = round
			started := time.Now()
			response, err := store.Remember(ctx, privateAuth, v1alpha1.RememberRequest{
				Text: cases[index].text, IdempotencyKey: evaluationEffectKey(index, cases[index].text),
				SourceContext: v1alpha1.SourceContext{SourceType: "local_corpus_evaluation"},
			})
			if err != nil {
				return evaluationReport{}, fmt.Errorf("Remember evaluation case %d: %w", index, err)
			}
			rememberLatencies = append(rememberLatencies, time.Since(started))
			cases[index].receiptID, cases[index].token = response.ReceiptID, response.ConsistencyToken
			result, err := queryEvaluationCase(ctx, store, privateAuth, isolatedAuth, cases[index])
			if err != nil {
				return evaluationReport{}, fmt.Errorf("immediate Recall evaluation case %d: %w", index, err)
			}
			if result.at1 {
				immediateAt1++
			}
			if result.at5 {
				immediateAt5++
			}
			if result.visible {
				immediateVisible++
			}
		}
		newReceipts := end - visible
		visible = end
		if err := store.Close(); err != nil {
			return evaluationReport{}, fmt.Errorf("close evaluation store after round %d: %w", round, err)
		}
		store, err = appliance.Open(ctx, appliance.Options{DataDir: dataDir})
		if err != nil {
			return evaluationReport{}, fmt.Errorf("restart evaluation store after round %d: %w", round, err)
		}
		results := make([]queryResult, 0, visible)
		durableResults, visibleResults, at1, at5, leaks := 0, 0, 0, 0, 0
		recallLatencies := make([]time.Duration, 0, visible)
		for index := 0; index < visible; index++ {
			result, err := queryEvaluationCase(ctx, store, privateAuth, isolatedAuth, cases[index])
			if err != nil {
				return evaluationReport{}, fmt.Errorf("post-restart Recall evaluation case %d: %w", index, err)
			}
			results = append(results, result)
			recallLatencies = append(recallLatencies, result.latency)
			if result.durable {
				durableResults++
			}
			if result.at1 {
				at1++
			}
			if result.visible {
				visibleResults++
			}
			if result.at5 {
				at5++
			}
			if result.leaked {
				leaks++
			}
		}
		roundMetrics := roundReport{
			Round: round, NewReceipts: newReceipts, CumulativeReceipts: visible,
			ImmediateQueries: newReceipts, ImmediateRetrievalAt8: rate(immediateVisible, newReceipts),
			ImmediateRecallAt1: rate(immediateAt1, newReceipts), ImmediateRecallAt5: rate(immediateAt5, newReceipts),
			PostRestartQueries: visible, PostRestartDurableReceiptRate: rate(durableResults, visible),
			PostRestartRetrievalAt8: rate(visibleResults, visible), PostRestartRecallAt1: rate(at1, visible),
			PostRestartRecallAt5: rate(at5, visible), PrivateLeakageCount: leaks,
		}
		roundMetrics.RememberP50Microseconds, roundMetrics.RememberP95Microseconds, roundMetrics.RememberP99Microseconds = latencyPercentiles(rememberLatencies)
		roundMetrics.RecallP50Microseconds, roundMetrics.RecallP95Microseconds, roundMetrics.RecallP99Microseconds = latencyPercentiles(recallLatencies)
		report.Rounds = append(report.Rounds, roundMetrics)
		finalResults = results
	}
	report.Final = buildFinalSnapshot(cases[:visible], finalResults, report.Rounds[len(report.Rounds)-1].Round)
	return report, nil
}

func bootstrapEvaluationStore(
	ctx context.Context,
	dataDir string,
) (*appliance.Store, v1alpha1.CallAuthorization, v1alpha1.CallAuthorization, error) {
	store, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
	if err != nil {
		return nil, v1alpha1.CallAuthorization{}, v1alpha1.CallAuthorization{}, fmt.Errorf("open evaluation store: %w", err)
	}
	now := time.Now().UTC()
	operations := []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus}
	bootstrap, err := store.Bootstrap(ctx, appliance.BootstrapRequest{
		Realms: []appliance.Realm{{ID: "realm-eval"}},
		Identities: []appliance.Identity{
			{ID: "identity-eval-private", RealmID: "realm-eval"},
			{ID: "identity-eval-isolated", RealmID: "realm-eval"},
		},
		Spaces: []appliance.Space{
			{ID: "space-eval-private", RealmID: "realm-eval", IdentityID: "identity-eval-private", Class: v1alpha1.SpaceClassPrivate},
			{ID: "space-eval-isolated", RealmID: "realm-eval", IdentityID: "identity-eval-isolated", Class: v1alpha1.SpaceClassPrivate},
		},
		Views: []appliance.ViewDefinition{
			{ID: "view-eval-private", RealmID: "realm-eval", ReadSpaceIDs: []v1alpha1.SpaceID{"space-eval-private"}, WriteSpaceID: "space-eval-private", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1},
			{ID: "view-eval-isolated", RealmID: "realm-eval", ReadSpaceIDs: []v1alpha1.SpaceID{"space-eval-isolated"}, WriteSpaceID: "space-eval-isolated", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1},
		},
		Grants: []appliance.Grant{
			{ID: "grant-eval-private", PrincipalRef: "principal:eval-private", ActorRef: "actor-eval-private", ViewRef: "view-eval-private", AllowedOperations: operations, AllowedAudiences: []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: now.Add(24 * time.Hour), Version: 1},
			{ID: "grant-eval-isolated", PrincipalRef: "principal:eval-isolated", ActorRef: "actor-eval-isolated", ViewRef: "view-eval-isolated", AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRecall}, AllowedAudiences: []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: now.Add(24 * time.Hour), Version: 1},
		},
		IssuerPrincipals: []string{"principal:eval-private", "principal:eval-isolated"},
	})
	if err != nil {
		_ = store.Close()
		return nil, v1alpha1.CallAuthorization{}, v1alpha1.CallAuthorization{}, fmt.Errorf("bootstrap evaluation store: %w", err)
	}
	issue := func(principal string, grant v1alpha1.GrantID, actor string, allowed []v1alpha1.Operation) (v1alpha1.CallAuthorization, error) {
		capability, err := store.IssueCapability(ctx, appliance.IssueCapabilityRequest{
			Authorization: appliance.IssuerAuthorization{PrincipalRef: principal, Credential: bootstrap.IssuerCredentials[principal]},
			GrantRef:      grant, ActorRef: actor, Audience: v1alpha1.AudiencePrivate,
			Operations: allowed, TTL: 12 * time.Hour,
		})
		if err != nil {
			return v1alpha1.CallAuthorization{}, err
		}
		return v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: actor, Audience: v1alpha1.AudiencePrivate}, nil
	}
	privateAuth, err := issue("principal:eval-private", "grant-eval-private", "actor-eval-private", operations)
	if err != nil {
		_ = store.Close()
		return nil, v1alpha1.CallAuthorization{}, v1alpha1.CallAuthorization{}, fmt.Errorf("issue private evaluation capability: %w", err)
	}
	isolatedAuth, err := issue("principal:eval-isolated", "grant-eval-isolated", "actor-eval-isolated", []v1alpha1.Operation{v1alpha1.OperationRecall})
	if err != nil {
		_ = store.Close()
		return nil, v1alpha1.CallAuthorization{}, v1alpha1.CallAuthorization{}, fmt.Errorf("issue isolated evaluation capability: %w", err)
	}
	return store, privateAuth, isolatedAuth, nil
}

func queryEvaluationCase(
	ctx context.Context,
	store *appliance.Store,
	privateAuth v1alpha1.CallAuthorization,
	isolatedAuth v1alpha1.CallAuthorization,
	test evaluationCase,
) (queryResult, error) {
	status, err := store.GetReceiptStatus(ctx, privateAuth, v1alpha1.GetReceiptStatusRequest{ReceiptID: test.receiptID})
	result := queryResult{}
	if err == nil {
		result.durable = status.ReceiptID == test.receiptID
	} else if !v1alpha1.IsCode(err, v1alpha1.ErrorCodeNotFound) {
		return queryResult{}, fmt.Errorf("read receipt status: %w", err)
	}
	request := v1alpha1.RecallRequest{
		Query: test.query, MinConsistencyToken: test.token,
		Budget:        v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 64 << 10, DeadlineMS: 5_000},
		SourceContext: v1alpha1.SourceContext{SourceType: "local_corpus_evaluation"},
	}
	started := time.Now()
	response, err := store.Recall(ctx, privateAuth, request)
	latency := time.Since(started)
	if err != nil {
		return queryResult{}, err
	}
	result.latency = latency
	for index, fragment := range response.Fragments {
		if containsEvidence(fragment.EvidenceRefs, test.receiptID) {
			result.visible = true
			if index == 0 {
				result.at1 = true
			}
			if index < 5 {
				result.at5 = true
			}
		}
	}
	request.MinConsistencyToken = ""
	isolated, err := store.Recall(ctx, isolatedAuth, request)
	if err != nil {
		return queryResult{}, err
	}
	for _, fragment := range isolated.Fragments {
		if containsEvidence(fragment.EvidenceRefs, test.receiptID) {
			result.leaked = true
		}
	}
	return result, nil
}

func buildEvaluationCases(chunks []string, limit int) []evaluationCase {
	documentTokens := make([][]string, len(chunks))
	documentFrequency := make(map[string]int)
	for index, chunk := range chunks {
		documentTokens[index] = evaluationTokens(chunk)
		for _, token := range documentTokens[index] {
			documentFrequency[token]++
		}
	}
	cases := make([]evaluationCase, 0, min(limit, len(chunks)))
	for index, chunk := range chunks {
		query := selectEvaluationQuery(documentTokens[index], documentFrequency)
		if query == "" {
			continue
		}
		cases = append(cases, evaluationCase{
			text: chunk, query: query, queryDocumentFrequency: documentFrequency[query],
		})
		if len(cases) == limit {
			break
		}
	}
	return cases
}

func queryShape(cases []evaluationCase) (unique, colliding, maximumFrequency int) {
	for _, test := range cases {
		if test.queryDocumentFrequency == 1 {
			unique++
		} else {
			colliding++
		}
		maximumFrequency = max(maximumFrequency, test.queryDocumentFrequency)
	}
	return unique, colliding, maximumFrequency
}

func evaluationTokens(value string) []string {
	values := strings.FieldsFunc(strings.ToLower(value), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsNumber(char)
	})
	seen := make(map[string]struct{}, len(values))
	tokens := make([]string, 0, len(values))
	for _, token := range values {
		runes := utf8.RuneCountInString(token)
		if runes < 2 || len(token) > 128 || isCommonEvaluationToken(token) {
			continue
		}
		if isASCII(token) && runes < 4 {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return tokens
}

func selectEvaluationQuery(tokens []string, frequency map[string]int) string {
	if len(tokens) == 0 {
		return ""
	}
	candidates := append([]string(nil), tokens...)
	sort.Slice(candidates, func(i, j int) bool {
		left, right := frequency[candidates[i]], frequency[candidates[j]]
		if left != right {
			return left < right
		}
		leftRunes, rightRunes := utf8.RuneCountInString(candidates[i]), utf8.RuneCountInString(candidates[j])
		if leftRunes != rightRunes {
			return leftRunes > rightRunes
		}
		return candidates[i] < candidates[j]
	})
	return candidates[0]
}

func isCommonEvaluationToken(value string) bool {
	_, exists := map[string]struct{}{
		"about": {}, "after": {}, "before": {}, "from": {}, "into": {}, "that": {}, "their": {},
		"there": {}, "these": {}, "this": {}, "through": {}, "using": {}, "when": {}, "where": {},
		"which": {}, "with": {}, "without": {}, "should": {}, "must": {}, "will": {}, "would": {},
		"memory": {}, "caelis": {}, "codex": {}, "agent": {}, "user": {}, "assistant": {},
	}[value]
	return exists
}

func isASCII(value string) bool {
	for _, char := range value {
		if char > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func evaluationEffectKey(index int, text string) string {
	digest := sha256.Sum256([]byte(text))
	return fmt.Sprintf("corpus-%05d-%s", index, hex.EncodeToString(digest[:6]))
}

func containsEvidence(values []v1alpha1.ReceiptID, target v1alpha1.ReceiptID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func latencyPercentiles(values []time.Duration) (int64, int64, int64) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	value := func(percentile int) int64 {
		index := (len(sorted)*percentile + 99) / 100
		if index > 0 {
			index--
		}
		return sorted[index].Microseconds()
	}
	return value(50), value(95), value(99)
}

func rate(success, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(success) / float64(total)
}

func buildFinalSnapshot(cases []evaluationCase, results []queryResult, finalRound int) finalSnapshotReport {
	report := finalSnapshotReport{Receipts: len(cases), Cohorts: make([]cohortReport, 0, finalRound)}
	durable, visible, at1, at5 := 0, 0, 0, 0
	for _, result := range results {
		if result.durable {
			durable++
		}
		if result.visible {
			visible++
		}
		if result.at1 {
			at1++
		}
		if result.at5 {
			at5++
		}
		if result.leaked {
			report.PrivateLeakageCount++
		}
	}
	report.DurableReceiptRate = rate(durable, len(results))
	report.RetrievalAt8 = rate(visible, len(results))
	report.RecallAt1, report.RecallAt5 = rate(at1, len(results)), rate(at5, len(results))
	for insertedRound := 1; insertedRound <= finalRound; insertedRound++ {
		cohort := cohortReport{InsertedRound: insertedRound, AgeRounds: finalRound - insertedRound}
		cohortDurable, cohortVisible, cohortAt1, cohortAt5 := 0, 0, 0, 0
		for index, test := range cases {
			if test.round != insertedRound {
				continue
			}
			cohort.Cases++
			if results[index].durable {
				cohortDurable++
			}
			if results[index].visible {
				cohortVisible++
			}
			if results[index].at1 {
				cohortAt1++
			}
			if results[index].at5 {
				cohortAt5++
			}
		}
		cohort.DurableReceiptRate = rate(cohortDurable, cohort.Cases)
		cohort.RetrievalAt8 = rate(cohortVisible, cohort.Cases)
		cohort.RecallAt1, cohort.RecallAt5 = rate(cohortAt1, cohort.Cases), rate(cohortAt5, cohort.Cases)
		report.Cohorts = append(report.Cohorts, cohort)
	}
	return report
}
