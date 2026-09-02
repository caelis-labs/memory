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
	Rounds             int                   `json:"rounds"`
	RequestedLimit     int                   `json:"requested_limit"`
	RecallMaxFragments int                   `json:"recall_max_fragments"`
	RestartAfterWrites bool                  `json:"restart_after_writes"`
	RetrievalPolicy    retrievalPolicyReport `json:"retrieval_policy"`
	LexiconPolicy      lexiconPolicyReport   `json:"lexicon_policy"`
}

type retrievalPolicyReport struct {
	Analyzer             string  `json:"analyzer"`
	FirstTermWeight      float64 `json:"first_term_weight"`
	HanNGramWeight       float64 `json:"han_ngram_weight"`
	ExactPhraseWeight    float64 `json:"exact_phrase_weight"`
	PrivateLexiconWeight float64 `json:"private_lexicon_weight"`
	BM25TieBreakWeight   float64 `json:"bm25_tie_break_weight"`
}

type lexiconPolicyReport struct {
	MinDocumentFrequency  int     `json:"min_document_frequency"`
	MinBoundaryDiversity  int     `json:"min_boundary_diversity"`
	MinActivationScore    float64 `json:"min_activation_score"`
	MaxAverageOccurrences float64 `json:"max_average_occurrences"`
}

type lexiconGrowthReport struct {
	GenerationSum   int64 `json:"generation_sum"`
	CandidateTerms  int64 `json:"candidate_terms"`
	ActiveTerms     int64 `json:"active_terms"`
	RetiredTerms    int64 `json:"retired_terms"`
	EvidenceLinks   int64 `json:"evidence_links"`
	PendingRebuilds int64 `json:"pending_rebuilds"`
}

type roundReport struct {
	Round                         int                 `json:"round"`
	NewReceipts                   int                 `json:"new_receipts"`
	CumulativeReceipts            int                 `json:"cumulative_receipts"`
	ImmediateQueries              int                 `json:"immediate_queries"`
	ImmediateRetrievalAt8         float64             `json:"immediate_retrieval_at_8"`
	ImmediateRecallAt1            float64             `json:"immediate_recall_at_1"`
	ImmediateRecallAt5            float64             `json:"immediate_recall_at_5"`
	PostRestartQueries            int                 `json:"post_restart_queries"`
	PostRestartDurableReceiptRate float64             `json:"post_restart_durable_receipt_rate"`
	PostRestartRetrievalAt8       float64             `json:"post_restart_retrieval_at_8"`
	PostRestartRecallAt1          float64             `json:"post_restart_recall_at_1"`
	PostRestartRecallAt5          float64             `json:"post_restart_recall_at_5"`
	PostRestartMRR                float64             `json:"post_restart_mrr"`
	PostRestartZeroResultRate     float64             `json:"post_restart_zero_result_rate"`
	PrivateLeakageCount           int                 `json:"private_leakage_count"`
	RememberP50Microseconds       int64               `json:"remember_p50_us"`
	RememberP95Microseconds       int64               `json:"remember_p95_us"`
	RememberP99Microseconds       int64               `json:"remember_p99_us"`
	RecallP50Microseconds         int64               `json:"recall_p50_us"`
	RecallP95Microseconds         int64               `json:"recall_p95_us"`
	RecallP99Microseconds         int64               `json:"recall_p99_us"`
	Lexicon                       lexiconGrowthReport `json:"lexicon"`
}

type finalSnapshotReport struct {
	Receipts            int                    `json:"receipts"`
	DurableReceiptRate  float64                `json:"durable_receipt_rate"`
	RetrievalAt8        float64                `json:"retrieval_at_8"`
	RecallAt1           float64                `json:"recall_at_1"`
	RecallAt5           float64                `json:"recall_at_5"`
	MRR                 float64                `json:"mrr"`
	ZeroResultRate      float64                `json:"zero_result_rate"`
	PrivateLeakageCount int                    `json:"private_leakage_count"`
	Lexicon             lexiconGrowthReport    `json:"lexicon"`
	LexiconQuality      lexiconQualityReport   `json:"lexicon_quality"`
	LegacyUnicode61     retrievalQualityReport `json:"legacy_unicode61"`
	RecallAt1Lift       float64                `json:"recall_at_1_lift_over_legacy"`
	RecallAt5Lift       float64                `json:"recall_at_5_lift_over_legacy"`
	Cohorts             []cohortReport         `json:"cohorts"`
}

type retrievalQualityReport struct {
	Queries        int     `json:"queries"`
	RetrievalAt8   float64 `json:"retrieval_at_8"`
	RecallAt1      float64 `json:"recall_at_1"`
	RecallAt5      float64 `json:"recall_at_5"`
	MRR            float64 `json:"mrr"`
	ZeroResultRate float64 `json:"zero_result_rate"`
}

type lexiconQualityReport struct {
	Queries             int     `json:"queries"`
	RelevantReceipts    int     `json:"relevant_receipts"`
	RetrievalAt8        float64 `json:"retrieval_at_8"`
	RecallAt1           float64 `json:"recall_at_1"`
	RecallAt5           float64 `json:"recall_at_5"`
	MRR                 float64 `json:"mrr"`
	MeanPrecisionAt5    float64 `json:"mean_precision_at_5"`
	ZeroResultRate      float64 `json:"zero_result_rate"`
	PrivateLeakageCount int     `json:"private_leakage_count"`
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
	durable        bool
	visible        bool
	at1            bool
	at5            bool
	leaked         bool
	zero           bool
	reciprocalRank float64
	latency        time.Duration
}

func evaluate(ctx context.Context, source sourceData, opts options) (evaluationReport, error) {
	opts.lexiconPolicy = normalizedEvaluationLexiconPolicy(opts.lexiconPolicy)
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
	store, privateAuth, isolatedAuth, err := bootstrapEvaluationStore(ctx, dataDir, opts.lexiconPolicy)
	if err != nil {
		return evaluationReport{}, err
	}
	defer func() { _ = store.Close() }()
	uniqueQueries, collidingQueries, maxQueryDocumentFrequency := queryShape(cases)
	report := evaluationReport{
		FormatVersion: 2,
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
			RetrievalPolicy: retrievalPolicyReport{
				Analyzer:        "gse-zh-s-v1+han-2-3gram",
				FirstTermWeight: 2, HanNGramWeight: 0.25, ExactPhraseWeight: 4,
				PrivateLexiconWeight: 3, BM25TieBreakWeight: 0.001,
			},
			LexiconPolicy: lexiconPolicyReport{
				MinDocumentFrequency:  opts.lexiconPolicy.MinDocumentFrequency,
				MinBoundaryDiversity:  opts.lexiconPolicy.MinBoundaryDiversity,
				MinActivationScore:    opts.lexiconPolicy.MinActivationScore,
				MaxAverageOccurrences: opts.lexiconPolicy.MaxAverageOccurrences,
			},
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
		store, err = appliance.Open(ctx, appliance.Options{DataDir: dataDir, LexiconPolicy: &opts.lexiconPolicy})
		if err != nil {
			return evaluationReport{}, fmt.Errorf("restart evaluation store after round %d: %w", round, err)
		}
		results := make([]queryResult, 0, visible)
		durableResults, visibleResults, at1, at5, leaks, zeroResults := 0, 0, 0, 0, 0, 0
		reciprocalRankSum := 0.0
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
			if result.zero {
				zeroResults++
			}
			reciprocalRankSum += result.reciprocalRank
		}
		inspection, err := store.Inspect(ctx)
		if err != nil {
			return evaluationReport{}, fmt.Errorf("inspect lexicon after round %d: %w", round, err)
		}
		roundMetrics := roundReport{
			Round: round, NewReceipts: newReceipts, CumulativeReceipts: visible,
			ImmediateQueries: newReceipts, ImmediateRetrievalAt8: rate(immediateVisible, newReceipts),
			ImmediateRecallAt1: rate(immediateAt1, newReceipts), ImmediateRecallAt5: rate(immediateAt5, newReceipts),
			PostRestartQueries: visible, PostRestartDurableReceiptRate: rate(durableResults, visible),
			PostRestartRetrievalAt8: rate(visibleResults, visible), PostRestartRecallAt1: rate(at1, visible),
			PostRestartRecallAt5: rate(at5, visible), PrivateLeakageCount: leaks,
			PostRestartMRR:            reciprocalRankSum / float64(visible),
			PostRestartZeroResultRate: rate(zeroResults, visible),
			Lexicon:                   lexiconGrowthFromInspection(inspection),
		}
		roundMetrics.RememberP50Microseconds, roundMetrics.RememberP95Microseconds, roundMetrics.RememberP99Microseconds = latencyPercentiles(rememberLatencies)
		roundMetrics.RecallP50Microseconds, roundMetrics.RecallP95Microseconds, roundMetrics.RecallP99Microseconds = latencyPercentiles(recallLatencies)
		report.Rounds = append(report.Rounds, roundMetrics)
		finalResults = results
	}
	report.Final = buildFinalSnapshot(cases[:visible], finalResults, report.Rounds[len(report.Rounds)-1].Round)
	report.Final.Lexicon = report.Rounds[len(report.Rounds)-1].Lexicon
	report.Final.LexiconQuality, err = evaluateLexiconQuality(
		ctx, store, privateAuth, isolatedAuth, cases[:visible], normalizedEvaluationLexiconPolicy(appliance.LexiconPolicy{}),
	)
	if err != nil {
		return evaluationReport{}, err
	}
	report.Final.LegacyUnicode61, err = evaluateLegacyUnicode61(ctx, cases[:visible])
	if err != nil {
		return evaluationReport{}, err
	}
	report.Final.RecallAt1Lift = report.Final.RecallAt1 - report.Final.LegacyUnicode61.RecallAt1
	report.Final.RecallAt5Lift = report.Final.RecallAt5 - report.Final.LegacyUnicode61.RecallAt5
	return report, nil
}

func normalizedEvaluationLexiconPolicy(policy appliance.LexiconPolicy) appliance.LexiconPolicy {
	if policy.MinDocumentFrequency == 0 {
		policy.MinDocumentFrequency = 3
	}
	if policy.MinTermRunes == 0 {
		policy.MinTermRunes = 3
	}
	if policy.MaxTermRunes == 0 {
		policy.MaxTermRunes = 8
	}
	if policy.MaxCandidatesPerText == 0 {
		policy.MaxCandidatesPerText = 128
	}
	if policy.MinBoundaryDiversity == 0 {
		policy.MinBoundaryDiversity = 2
	}
	if policy.MinActivationScore == 0 {
		policy.MinActivationScore = 6
	}
	if policy.MaxAverageOccurrences == 0 {
		policy.MaxAverageOccurrences = 8
	}
	return policy
}

func bootstrapEvaluationStore(
	ctx context.Context,
	dataDir string,
	policy appliance.LexiconPolicy,
) (*appliance.Store, v1alpha1.CallAuthorization, v1alpha1.CallAuthorization, error) {
	store, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir, LexiconPolicy: &policy})
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
	result.zero = len(response.Fragments) == 0
	for index, fragment := range response.Fragments {
		if containsEvidence(fragment.EvidenceRefs, test.receiptID) {
			result.visible = true
			if index == 0 {
				result.at1 = true
			}
			if index < 5 {
				result.at5 = true
			}
			if result.reciprocalRank == 0 {
				result.reciprocalRank = 1 / float64(index+1)
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
	seen := make(map[string]struct{}, len(values)*4)
	tokens := make([]string, 0, len(values)*4)
	add := func(token string) {
		runes := utf8.RuneCountInString(token)
		if runes < 2 || len(token) > 128 || isCommonEvaluationToken(token) {
			return
		}
		if isASCII(token) && runes < 4 {
			return
		}
		if _, exists := seen[token]; exists {
			return
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	for _, token := range values {
		if !strings.ContainsFunc(token, func(char rune) bool { return unicode.Is(unicode.Han, char) }) {
			add(token)
			continue
		}
		for _, run := range evaluationHanRuns(token) {
			runes := []rune(run)
			for size := 2; size <= min(4, len(runes)); size++ {
				for start := 0; start+size <= len(runes); start++ {
					add(string(runes[start : start+size]))
				}
			}
		}
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
	zero := 0
	reciprocalRankSum := 0.0
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
		if result.zero {
			zero++
		}
		reciprocalRankSum += result.reciprocalRank
	}
	report.DurableReceiptRate = rate(durable, len(results))
	report.RetrievalAt8 = rate(visible, len(results))
	report.RecallAt1, report.RecallAt5 = rate(at1, len(results)), rate(at5, len(results))
	report.MRR = reciprocalRankSum / float64(len(results))
	report.ZeroResultRate = rate(zero, len(results))
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

func lexiconGrowthFromInspection(inspection appliance.Inspection) lexiconGrowthReport {
	return lexiconGrowthReport{
		GenerationSum:   inspection.Lexicon.GenerationSum,
		CandidateTerms:  inspection.Lexicon.CandidateTerms,
		ActiveTerms:     inspection.Lexicon.ActiveTerms,
		RetiredTerms:    inspection.Lexicon.RetiredTerms,
		EvidenceLinks:   inspection.Lexicon.EvidenceLinks,
		PendingRebuilds: inspection.Lexicon.PendingRebuilds,
	}
}
