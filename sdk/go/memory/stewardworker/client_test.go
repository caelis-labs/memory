package stewardworker

import (
	"context"
	"errors"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
)

type panicGenerator struct{}

func (panicGenerator) Generate(context.Context, stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error) {
	panic("private provider detail")
}

func TestGeneratorBoundaryContainsPanicsAndSanitizesCodes(t *testing.T) {
	_, err := callGenerator(t.Context(), panicGenerator{}, stewardv1alpha1.WorkRequest{})
	var generationErr *GenerationError
	if !errors.As(err, &generationErr) || generationErr.Code != "generator_panic" || !generationErr.Retryable {
		t.Fatalf("panic classification = %#v", err)
	}
	if got, retryable := classifyGenerationError(&GenerationError{Code: "Provider URL: secret", Retryable: false}); got != "generator_failure" || retryable {
		t.Fatalf("unsafe classification = %q retryable=%v", got, retryable)
	}
}

func TestRunnerValidationBounds(t *testing.T) {
	runner := Runner{Client: &Client{}, Generator: panicGenerator{}, Options: RunnerOptions{
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
