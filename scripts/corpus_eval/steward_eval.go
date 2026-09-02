package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
	"github.com/caelis-labs/memory/sdk/go/memory/stewardworker"
)

const maxOllamaResponseBytes = 1 << 20

type stewardReport struct {
	Mode                string                      `json:"mode"`
	Cases               int                         `json:"cases"`
	Model               string                      `json:"model"`
	Policy              stewardPolicyReport         `json:"policy"`
	Jobs                stewardJobReport            `json:"jobs"`
	Operations          map[string]int              `json:"operations,omitempty"`
	AttemptedOperations map[string]int              `json:"attempted_operations,omitempty"`
	ProposalRejections  map[string]int              `json:"proposal_rejections,omitempty"`
	GeneratorFailures   map[string]int              `json:"generator_failures,omitempty"`
	TerminalErrorCodes  map[string]int              `json:"terminal_error_codes,omitempty"`
	ModelUsage          stewardModelUsageReport     `json:"model_usage"`
	SemanticRecall      stewardSemanticRecallReport `json:"semantic_recall"`
	WallDurationMS      int64                       `json:"wall_duration_ms"`
}

type stewardPolicyReport struct {
	ProfileID             string `json:"profile_id"`
	ProfileVersion        uint64 `json:"profile_version"`
	ProfilePromptSHA256   string `json:"profile_prompt_sha256"`
	EffectivePromptSHA256 string `json:"effective_prompt_sha256"`
	PromptDrift           bool   `json:"prompt_drift"`
	ProviderEnvelope      string `json:"provider_envelope"`
	NativeJSONSchemaUsed  bool   `json:"native_json_schema_used"`
	MaxContextRecords     int    `json:"max_context_records"`
	MaxInputBytes         int    `json:"max_input_bytes"`
	MaxOutputBytes        int    `json:"max_output_bytes"`
}

type stewardJobReport struct {
	Completed          int64 `json:"completed"`
	Failed             int64 `json:"failed"`
	Pending            int64 `json:"pending"`
	Leased             int64 `json:"leased"`
	ActiveRecords      int64 `json:"active_records"`
	InvalidatedRecords int64 `json:"invalidated_records"`
}

type stewardModelUsageReport struct {
	Calls            int   `json:"calls"`
	CallFailures     int   `json:"call_failures"`
	PromptTokens     int   `json:"prompt_tokens"`
	CompletionTokens int   `json:"completion_tokens"`
	TotalDurationMS  int64 `json:"total_duration_ms"`
	LoadDurationMS   int64 `json:"load_duration_ms"`
	GenerationP50MS  int64 `json:"generation_p50_ms"`
	GenerationP95MS  int64 `json:"generation_p95_ms"`
}

type stewardSemanticRecallReport struct {
	Queries                   int     `json:"queries"`
	TargetRetrievalAt8        float64 `json:"target_retrieval_at_8"`
	TargetRecallAt1           float64 `json:"target_recall_at_1"`
	TargetRecallAt5           float64 `json:"target_recall_at_5"`
	MRR                       float64 `json:"mrr"`
	MissingTargetSemanticRate float64 `json:"missing_target_semantic_rate"`
}

type evaluationStewardWorker struct {
	store      *appliance.Store
	operations map[string]int
	attempted  map[string]int
	rejections map[string]int
	failures   map[string]int
}

func (w *evaluationStewardWorker) Claim(ctx context.Context, leaseDuration time.Duration) (stewardv1alpha1.ClaimResponse, error) {
	work, found, err := w.store.ClaimStewardJob(ctx, leaseDuration)
	if err != nil {
		return stewardv1alpha1.ClaimResponse{}, err
	}
	response := stewardv1alpha1.ClaimResponse{Found: found}
	if found {
		response.Lease = &work.Lease
		response.Attempt = work.Attempt
		response.Work = &work.Request
	}
	return response, nil
}

func (w *evaluationStewardWorker) Apply(ctx context.Context, request stewardv1alpha1.ApplyRequest) (stewardv1alpha1.ApplyResponse, error) {
	if w.attempted == nil {
		w.attempted = make(map[string]int)
	}
	w.attempted[string(request.Proposal.Operation)]++
	result, err := w.store.ApplyStewardProposal(ctx, request.Lease, request.Proposal)
	if err != nil {
		if errors.Is(err, appliance.ErrStewardProposalInvalid) {
			if w.rejections == nil {
				w.rejections = make(map[string]int)
			}
			w.rejections[proposalRejectionClass(err)]++
		}
		return stewardv1alpha1.ApplyResponse{}, evaluationStewardServiceError(err)
	}
	if w.operations == nil {
		w.operations = make(map[string]int)
	}
	w.operations[string(result.Operation)]++
	return stewardv1alpha1.ApplyResponse{Result: result}, nil
}

func proposalRejectionClass(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "cite its job receipt"):
		return "missing_job_receipt"
	case strings.Contains(message, "evidence"):
		return "evidence_invalid"
	case strings.Contains(message, "target") || strings.Contains(message, "revision"):
		return "target_or_revision_invalid"
	default:
		return "shape_or_policy_invalid"
	}
}

func evaluationStewardServiceError(err error) error {
	switch {
	case errors.Is(err, appliance.ErrStewardProposalInvalid):
		return &v1alpha1.ServiceError{Code: v1alpha1.ErrorCodeInvalidArgument, Message: "Steward proposal is invalid", RequestID: "corpus-eval-invalid"}
	case errors.Is(err, appliance.ErrStewardLeaseLost), errors.Is(err, appliance.ErrStewardConflict):
		return &v1alpha1.ServiceError{Code: v1alpha1.ErrorCodeConflict, Message: "Steward lease or proposal conflicted", RequestID: "corpus-eval-conflict"}
	case errors.Is(err, appliance.ErrStewardUnknownOutcome):
		return &v1alpha1.ServiceError{Code: v1alpha1.ErrorCodeUnknownOutcome, Message: "Steward apply outcome is unknown", Retryable: true, RequestID: "corpus-eval-unknown"}
	default:
		return err
	}
}

func (w *evaluationStewardWorker) Fail(ctx context.Context, request stewardv1alpha1.FailRequest) error {
	if err := w.store.ReportStewardFailure(ctx, request); err != nil {
		return err
	}
	if w.failures == nil {
		w.failures = make(map[string]int)
	}
	w.failures[request.Code]++
	return nil
}

type ollamaGenerator struct {
	endpoint string
	model    string
	timeout  time.Duration
	client   *http.Client

	calls             int
	callFailures      int
	promptTokens      int
	completionTokens  int
	totalDuration     time.Duration
	loadDuration      time.Duration
	durations         []time.Duration
	instructionDigest string
	instructionDrift  bool
}

type ollamaChatRequest struct {
	Model     string              `json:"model"`
	Messages  []ollamaChatMessage `json:"messages"`
	Stream    bool                `json:"stream"`
	Format    string              `json:"format"`
	Think     bool                `json:"think"`
	KeepAlive string              `json:"keep_alive"`
	Options   map[string]any      `json:"options"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	Error           string `json:"error"`
	TotalDuration   int64  `json:"total_duration"`
	LoadDuration    int64  `json:"load_duration"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

func (g *ollamaGenerator) Generate(ctx context.Context, request stewardworker.GenerationRequest) (stewardworker.GenerationResponse, error) {
	started := time.Now()
	g.calls++
	digest := sha256.Sum256([]byte(request.Instructions))
	instructionDigest := hex.EncodeToString(digest[:])
	if g.instructionDigest == "" {
		g.instructionDigest = instructionDigest
	} else if g.instructionDigest != instructionDigest {
		g.instructionDrift = true
	}
	body, err := json.Marshal(ollamaChatRequest{
		Model: g.model,
		Messages: []ollamaChatMessage{
			{Role: "system", Content: request.Instructions},
			{Role: "user", Content: request.Input},
		},
		Stream: false, Format: "json", Think: false, KeepAlive: "5m",
		Options: map[string]any{"temperature": 0, "num_ctx": 32768, "num_predict": 512},
	})
	if err != nil {
		g.recordFailure(started)
		return stewardworker.GenerationResponse{}, &stewardworker.GenerationError{Code: "request_encode", Retryable: false, Err: err}
	}
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, g.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		g.recordFailure(started)
		return stewardworker.GenerationResponse{}, &stewardworker.GenerationError{Code: "request_create", Retryable: false, Err: err}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(httpRequest)
	if err != nil {
		g.recordFailure(started)
		return stewardworker.GenerationResponse{}, &stewardworker.GenerationError{Code: "ollama_unavailable", Retryable: true, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxOllamaResponseBytes))
		g.recordFailure(started)
		return stewardworker.GenerationResponse{}, &stewardworker.GenerationError{Code: "ollama_http", Retryable: response.StatusCode >= 500}
	}
	var output ollamaChatResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxOllamaResponseBytes))
	if err := decoder.Decode(&output); err != nil {
		g.recordFailure(started)
		return stewardworker.GenerationResponse{}, &stewardworker.GenerationError{Code: "response_decode", Retryable: true, Err: err}
	}
	if output.Error != "" || !output.Done || strings.TrimSpace(output.Message.Content) == "" {
		g.recordFailure(started)
		return stewardworker.GenerationResponse{}, &stewardworker.GenerationError{Code: "response_invalid", Retryable: output.Error != ""}
	}
	duration := time.Since(started)
	g.durations = append(g.durations, duration)
	g.promptTokens += output.PromptEvalCount
	g.completionTokens += output.EvalCount
	g.totalDuration += time.Duration(output.TotalDuration)
	g.loadDuration += time.Duration(output.LoadDuration)
	return stewardworker.GenerationResponse{Text: output.Message.Content, ParseMode: stewardworker.ParseModeStrict}, nil
}

func (g *ollamaGenerator) recordFailure(started time.Time) {
	g.callFailures++
	g.durations = append(g.durations, time.Since(started))
}

func (g *ollamaGenerator) usage() stewardModelUsageReport {
	durations := append([]time.Duration(nil), g.durations...)
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	percentile := func(value int) int64 {
		if len(durations) == 0 {
			return 0
		}
		index := (len(durations)*value + 99) / 100
		return durations[index-1].Milliseconds()
	}
	return stewardModelUsageReport{
		Calls: g.calls, CallFailures: g.callFailures,
		PromptTokens: g.promptTokens, CompletionTokens: g.completionTokens,
		TotalDurationMS: g.totalDuration.Milliseconds(), LoadDurationMS: g.loadDuration.Milliseconds(),
		GenerationP50MS: percentile(50), GenerationP95MS: percentile(95),
	}
}

func evaluateSteward(ctx context.Context, dataDir string, cases []evaluationCase, opts options) (*stewardReport, error) {
	selected := cases[:min(opts.stewardLimit, len(cases))]
	store, privateAuth, _, err := bootstrapEvaluationStore(ctx, dataDir, nil)
	if err != nil {
		return nil, fmt.Errorf("bootstrap Steward evaluation: %w", err)
	}
	defer store.Close()
	profile := stewardworker.BuiltInProfile()
	if _, err := store.PutStewardProfile(ctx, managementv1alpha1.PutStewardProfileRequest{Profile: profile}); err != nil {
		return nil, fmt.Errorf("put Steward evaluation profile: %w", err)
	}
	if _, err := store.BindStewardProfile(ctx, managementv1alpha1.BindStewardProfileRequest{
		ProfileID: profile.ProfileID, Version: profile.Version,
		SpaceIDs: []v1alpha1.SpaceID{"space-eval-private"},
	}); err != nil {
		return nil, fmt.Errorf("bind Steward evaluation profile: %w", err)
	}
	generator := &ollamaGenerator{
		endpoint: opts.stewardEndpoint, model: opts.stewardModel, timeout: opts.stewardTimeout,
		client: &http.Client{
			Transport: &http.Transport{Proxy: nil},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	worker := &evaluationStewardWorker{store: store}
	runner := stewardworker.Runner{
		Client: worker, ModelGenerator: generator,
		Options: stewardworker.RunnerOptions{LeaseDuration: 5 * time.Minute, PollInterval: 100 * time.Millisecond},
	}
	type storedCase struct {
		value     evaluationCase
		receiptID v1alpha1.ReceiptID
		token     v1alpha1.ConsistencyToken
	}
	stored := make([]storedCase, 0, len(selected))
	started := time.Now()
	terminalCodes := make(map[string]int)
	for index, test := range selected {
		response, err := store.Remember(ctx, privateAuth, v1alpha1.RememberRequest{
			Text: test.text, IdempotencyKey: "steward-" + evaluationEffectKey(index, test.text),
			SourceContext: v1alpha1.SourceContext{SourceType: "local_corpus_steward_evaluation"},
		})
		if err != nil {
			return nil, fmt.Errorf("Steward Remember case %d: %w", index, err)
		}
		stored = append(stored, storedCase{value: test, receiptID: response.ReceiptID, token: response.ConsistencyToken})
		for {
			status, err := store.GetReceiptStatus(ctx, privateAuth, v1alpha1.GetReceiptStatusRequest{ReceiptID: response.ReceiptID})
			if err != nil {
				return nil, fmt.Errorf("read Steward status case %d: %w", index, err)
			}
			if status.State == v1alpha1.ProcessingStateOrganized || status.State == v1alpha1.ProcessingStateFailed {
				if status.TerminalErrorCode != "" {
					terminalCodes[string(status.TerminalErrorCode)]++
				}
				break
			}
			found, err := runner.RunOnce(ctx)
			if err != nil {
				return nil, fmt.Errorf("run Steward case %d: %w", index, err)
			}
			if !found {
				timer := time.NewTimer(100 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
	inspection, err := store.Inspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspect Steward evaluation: %w", err)
	}
	semantic := stewardSemanticRecallReport{Queries: len(stored)}
	reciprocalRank := 0.0
	missing := 0
	for index, test := range stored {
		response, err := store.Recall(ctx, privateAuth, v1alpha1.RecallRequest{
			Query: test.value.query, MinConsistencyToken: test.token,
			Budget:        v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 64 << 10, DeadlineMS: 5_000},
			SourceContext: v1alpha1.SourceContext{SourceType: "local_corpus_steward_evaluation"},
		})
		if err != nil {
			return nil, fmt.Errorf("Steward semantic Recall case %d: %w", index, err)
		}
		position := -1
		for fragmentIndex, fragment := range response.Fragments {
			if len(fragment.RecordRefs) > 0 && containsEvidence(fragment.EvidenceRefs, test.receiptID) {
				position = fragmentIndex
				break
			}
		}
		if position < 0 {
			missing++
			continue
		}
		semantic.TargetRetrievalAt8++
		reciprocalRank += 1 / float64(position+1)
		if position == 0 {
			semantic.TargetRecallAt1++
		}
		if position < 5 {
			semantic.TargetRecallAt5++
		}
	}
	if semantic.Queries > 0 {
		count := float64(semantic.Queries)
		semantic.TargetRetrievalAt8 /= count
		semantic.TargetRecallAt1 /= count
		semantic.TargetRecallAt5 /= count
		semantic.MRR = reciprocalRank / count
		semantic.MissingTargetSemanticRate = float64(missing) / count
	}
	return &stewardReport{
		Mode: "local_ollama_model_generator", Cases: len(selected), Model: opts.stewardModel,
		Policy: stewardPolicyReport{
			ProfileID: string(profile.ProfileID), ProfileVersion: profile.Version,
			ProfilePromptSHA256: digestText(profile.SystemPrompt), EffectivePromptSHA256: generator.instructionDigest,
			PromptDrift: generator.instructionDrift, ProviderEnvelope: "ollama_json_object",
			NativeJSONSchemaUsed: false, MaxContextRecords: profile.MaxContextRecords,
			MaxInputBytes: profile.MaxInputBytes, MaxOutputBytes: profile.MaxOutputBytes,
		},
		Jobs: stewardJobReport{
			Completed: inspection.Steward.CompletedJobs, Failed: inspection.Steward.FailedJobs,
			Pending: inspection.Steward.PendingJobs, Leased: inspection.Steward.LeasedJobs,
			ActiveRecords: inspection.Steward.ActiveRecords, InvalidatedRecords: inspection.Steward.InvalidatedRecords,
		},
		Operations: cloneCounts(worker.operations), AttemptedOperations: cloneCounts(worker.attempted),
		ProposalRejections: cloneCounts(worker.rejections), GeneratorFailures: cloneCounts(worker.failures),
		TerminalErrorCodes: cloneCounts(terminalCodes),
		ModelUsage:         generator.usage(), SemanticRecall: semantic, WallDurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func localOllamaEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("-steward-endpoint must be an HTTP loopback origin")
	}
	host := parsed.Hostname()
	address := net.ParseIP(host)
	if host != "localhost" && (address == nil || !address.IsLoopback()) {
		return "", fmt.Errorf("-steward-endpoint must be loopback-only so private corpus text cannot leave the host")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func cloneCounts(source map[string]int) map[string]int {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]int, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
