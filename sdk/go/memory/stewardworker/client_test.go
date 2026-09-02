package stewardworker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
)

type panicModelGenerator struct{}

func (panicModelGenerator) Generate(context.Context, GenerationRequest) (GenerationResponse, error) {
	panic("private provider detail")
}

func TestGeneratorBoundaryContainsPanicsAndSanitizesCodes(t *testing.T) {
	_, err := callModelGenerator(t.Context(), panicModelGenerator{}, GenerationRequest{})
	var generationErr *GenerationError
	if !errors.As(err, &generationErr) || generationErr.Code != "generator_panic" || !generationErr.Retryable {
		t.Fatalf("panic classification = %#v", err)
	}
	if got, retryable := classifyGenerationError(&GenerationError{Code: "Provider URL: secret", Retryable: false}); got != "generator_failure" || retryable {
		t.Fatalf("unsafe classification = %q retryable=%v", got, retryable)
	}
}

func TestRunnerValidationBounds(t *testing.T) {
	runner := Runner{Client: &Client{}, ModelGenerator: panicModelGenerator{}, Options: RunnerOptions{
		LeaseDuration: 30 * time.Second, PollInterval: 20 * time.Millisecond,
	}}
	if err := runner.Validate(); err != nil {
		t.Fatal(err)
	}
	runner.Options.LeaseDuration = 1500 * time.Millisecond
	if err := runner.Validate(); err == nil {
		t.Fatal("fractional-second lease passed validation")
	}
}

type recordingWorker struct {
	claim   stewardv1alpha1.ClaimResponse
	applied *stewardv1alpha1.ApplyRequest
	failed  *stewardv1alpha1.FailRequest
}

func (w *recordingWorker) Claim(context.Context, time.Duration) (stewardv1alpha1.ClaimResponse, error) {
	return w.claim, nil
}

func (w *recordingWorker) Apply(_ context.Context, request stewardv1alpha1.ApplyRequest) (stewardv1alpha1.ApplyResponse, error) {
	w.applied = &request
	return stewardv1alpha1.ApplyResponse{}, nil
}

func (w *recordingWorker) Fail(_ context.Context, request stewardv1alpha1.FailRequest) error {
	w.failed = &request
	return nil
}

type recordingGenerator struct {
	request GenerationRequest
	output  GenerationResponse
}

func (g *recordingGenerator) Generate(_ context.Context, request GenerationRequest) (GenerationResponse, error) {
	g.request = request
	return g.output, nil
}

func TestRunnerOwnsPromptAndProposalParsing(t *testing.T) {
	lease := stewardv1alpha1.Lease{JobID: "job-1", Token: "opaque"}
	work := testWorkRequest(BuiltInProfile())
	worker := &recordingWorker{claim: stewardv1alpha1.ClaimResponse{
		Found: true, Lease: &lease, Attempt: 1, Work: &work,
	}}
	generator := &recordingGenerator{output: GenerationResponse{
		Text:      `provider wrapper {"operation":"ADD","kind":"fact","text":"durable","evidence_refs":["receipt-1"]}`,
		ParseMode: ParseModeText,
	}}
	runner := Runner{Client: worker, ModelGenerator: generator, Options: RunnerOptions{
		LeaseDuration: time.Minute, PollInterval: time.Second,
	}}
	found, err := runner.RunOnce(t.Context())
	if err != nil || !found {
		t.Fatalf("RunOnce() found=%v err=%v", found, err)
	}
	if worker.failed != nil || worker.applied == nil || worker.applied.Proposal.Operation != stewardv1alpha1.OperationAdd {
		t.Fatalf("worker applied=%+v failed=%+v", worker.applied, worker.failed)
	}
	if !strings.Contains(generator.request.Instructions, `"operation":"ADD"`) ||
		!strings.Contains(generator.request.Input, `"receipt_id":"receipt-1"`) {
		t.Fatalf("Memory-owned generation request = %+v", generator.request)
	}
}
