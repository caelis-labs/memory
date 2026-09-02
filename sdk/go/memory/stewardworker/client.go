// Package stewardworker lets a downstream host execute appliance-owned
// semantic Jobs with its existing model/provider stack.
package stewardworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/sdk/go/memory/internal/localhttp"
)

const maxResponseBytes = 1 << 20

// Client binds one least-authority Worker bearer to one owner-local memoryd
// endpoint. The bearer cannot call management, issuer, or Runtime routes.
type Client struct {
	http       *http.Client
	credential string
}

// NewClient creates a Worker client for the current Unix-socket local profile.
func NewClient(socketPath, credential string) *Client {
	return NewClientForEndpoint(memoryv1alpha1.LocalEndpoint{Network: memoryv1alpha1.LocalNetworkUnix, Address: socketPath}, credential)
}

// NewClientForEndpoint creates a Worker client for an OS-local endpoint.
func NewClientForEndpoint(endpoint memoryv1alpha1.LocalEndpoint, credential string) *Client {
	return &Client{http: localhttp.NewClient(endpoint), credential: credential}
}

// Claim leases at most one available Job.
func (c *Client) Claim(ctx context.Context, leaseDuration time.Duration) (stewardv1alpha1.ClaimResponse, error) {
	var response stewardv1alpha1.ClaimResponse
	if leaseDuration%time.Second != 0 {
		return response, fmt.Errorf("Steward Worker lease must use whole seconds")
	}
	err := c.do(ctx, stewardv1alpha1.LocalPathClaim,
		stewardv1alpha1.ClaimRequest{LeaseSeconds: int64(leaseDuration / time.Second)}, &response)
	return response, err
}

// Apply submits one untrusted proposal under the claimed lease. A transport
// failure is an unknown outcome and must be retried with the identical input.
func (c *Client) Apply(ctx context.Context, request stewardv1alpha1.ApplyRequest) (stewardv1alpha1.ApplyResponse, error) {
	var response stewardv1alpha1.ApplyResponse
	err := c.do(ctx, stewardv1alpha1.LocalPathApply, request, &response)
	if err != nil {
		var serviceErr *memoryv1alpha1.ServiceError
		if !errors.As(err, &serviceErr) {
			return response, &memoryv1alpha1.ServiceError{
				Code: memoryv1alpha1.ErrorCodeUnknownOutcome, Message: "Steward apply outcome is unknown; retry identical input",
				Retryable: true, RequestID: "steward-worker-transport",
			}
		}
	}
	return response, err
}

// Fail reports one stable, non-sensitive Generator failure. Memory owns retry
// delay and the terminal-attempt ceiling.
func (c *Client) Fail(ctx context.Context, request stewardv1alpha1.FailRequest) error {
	var response stewardv1alpha1.FailResponse
	return c.do(ctx, stewardv1alpha1.LocalPathFail, request, &response)
}

// CloseIdleConnections closes pooled local connections.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

func (c *Client) do(ctx context.Context, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode Steward Worker request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://memoryd"+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Steward Worker request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.credential)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call Steward Worker route: %w", err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var serviceErr memoryv1alpha1.ServiceError
		if err := decoder.Decode(&serviceErr); err == nil && serviceErr.Code != "" {
			return &serviceErr
		}
		return fmt.Errorf("Steward Worker route returned HTTP %d", response.StatusCode)
	}
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode Steward Worker response: %w", err)
	}
	return nil
}

// Generator is the pre-GA direct-proposal callback retained only so a host can
// update to ModelGenerator after publishing the next Memory prerelease.
// Deprecated: use ModelGenerator. This compatibility path is removed before
// GA after Caelis no longer imports the older SDK contract.
type Generator interface {
	Generate(context.Context, stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error)
}

// ModelGenerator is the only target model integration point. Memory supplies
// the complete instructions, input, optional native schema, and output budget.
// The downstream host invokes its existing provider and returns untrusted text;
// it does not construct Memory policy or parse proposals.
type ModelGenerator interface {
	Generate(context.Context, GenerationRequest) (GenerationResponse, error)
}

// Worker is the provider-neutral Steward work plane. The local transport
// Client and the embedded appliance runtime both implement it.
type Worker interface {
	Claim(context.Context, time.Duration) (stewardv1alpha1.ClaimResponse, error)
	Apply(context.Context, stewardv1alpha1.ApplyRequest) (stewardv1alpha1.ApplyResponse, error)
	Fail(context.Context, stewardv1alpha1.FailRequest) error
}

// GenerationError classifies a model failure without persisting model output or
// provider details. Code must contain only lowercase letters, digits, or `_`.
type GenerationError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *GenerationError) Error() string {
	if e.Err == nil {
		return "Steward generation failed"
	}
	return "Steward generation failed: " + e.Err.Error()
}

func (e *GenerationError) Unwrap() error { return e.Err }

// RunnerOptions bounds one downstream Worker loop.
type RunnerOptions struct {
	LeaseDuration time.Duration
	PollInterval  time.Duration
}

// Runner claims Jobs, invokes ModelGenerator with only model-facing data, and
// submits results. Generator is a temporary pre-GA compatibility input; exactly
// one callback must be supplied. Durable retry policy remains in Memory.
type Runner struct {
	Client         Worker
	ModelGenerator ModelGenerator
	Generator      Generator
	Options        RunnerOptions
}

// Validate checks the local execution bounds.
func (r Runner) Validate() error {
	if r.Client == nil {
		return fmt.Errorf("Steward Worker client is required")
	}
	if (r.ModelGenerator == nil) == (r.Generator == nil) {
		return fmt.Errorf("exactly one Steward model Generator is required")
	}
	if r.Options.LeaseDuration < time.Second || r.Options.LeaseDuration > 10*time.Minute || r.Options.LeaseDuration%time.Second != 0 {
		return fmt.Errorf("Steward Worker lease must be whole seconds within 1s..10m")
	}
	if r.Options.PollInterval < 10*time.Millisecond || r.Options.PollInterval > 10*time.Second {
		return fmt.Errorf("Steward Worker poll interval must be within 10ms..10s")
	}
	return nil
}

// RunOnce handles at most one available Job. found is false when the queue is
// empty. A claimed Job is either applied, failed, or left to lease expiry when
// the apply outcome remains unknown.
func (r Runner) RunOnce(ctx context.Context) (found bool, err error) {
	if err := r.Validate(); err != nil {
		return false, err
	}
	claim, err := r.Client.Claim(ctx, r.Options.LeaseDuration)
	if err != nil || !claim.Found {
		return false, err
	}
	if claim.Lease == nil || claim.Work == nil || claim.Attempt < 1 {
		return true, fmt.Errorf("memoryd returned an invalid Steward claim")
	}
	if claim.Work.Protocol != stewardv1alpha1.ProtocolVersion {
		return true, fmt.Errorf("memoryd returned an incompatible Steward work protocol")
	}
	if err := claim.Work.Profile.Validate(); err != nil {
		return true, fmt.Errorf("memoryd returned an invalid Steward profile: %w", err)
	}
	var proposal stewardv1alpha1.Proposal
	if r.ModelGenerator != nil {
		generationRequest, prepareErr := PrepareGeneration(*claim.Work)
		if prepareErr != nil {
			return true, r.Client.Fail(ctx, stewardv1alpha1.FailRequest{
				Lease: *claim.Lease, Code: "input_invalid", Retryable: false,
			})
		}
		generationResponse, generateErr := callModelGenerator(ctx, r.ModelGenerator, generationRequest)
		if generateErr != nil {
			code, retryable := classifyGenerationError(generateErr)
			return true, r.Client.Fail(ctx, stewardv1alpha1.FailRequest{Lease: *claim.Lease, Code: code, Retryable: retryable})
		}
		if len(generationResponse.Text) > generationRequest.MaxOutputBytes {
			return true, r.Client.Fail(ctx, stewardv1alpha1.FailRequest{
				Lease: *claim.Lease, Code: "output_too_large", Retryable: false,
			})
		}
		proposal, prepareErr = ParseProposal(generationResponse.Text, generationResponse.ParseMode)
		if prepareErr != nil {
			return true, r.Client.Fail(ctx, stewardv1alpha1.FailRequest{
				Lease: *claim.Lease, Code: "output_invalid", Retryable: false,
			})
		}
	} else {
		var generateErr error
		proposal, generateErr = callGenerator(ctx, r.Generator, *claim.Work)
		if generateErr != nil {
			code, retryable := classifyGenerationError(generateErr)
			return true, r.Client.Fail(ctx, stewardv1alpha1.FailRequest{Lease: *claim.Lease, Code: code, Retryable: retryable})
		}
	}
	apply := stewardv1alpha1.ApplyRequest{Lease: *claim.Lease, Proposal: proposal}
	_, err = r.Client.Apply(ctx, apply)
	if memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeUnknownOutcome) {
		_, err = r.Client.Apply(ctx, apply)
	}
	if err == nil {
		return true, nil
	}
	if memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeConflict) {
		failErr := r.Client.Fail(ctx, stewardv1alpha1.FailRequest{Lease: *claim.Lease, Code: "proposal_conflict", Retryable: true})
		if failErr != nil {
			return true, errors.Join(err, failErr)
		}
		return true, nil
	}
	if memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeInvalidArgument) {
		failErr := r.Client.Fail(ctx, stewardv1alpha1.FailRequest{Lease: *claim.Lease, Code: "proposal_invalid", Retryable: false})
		if failErr != nil {
			return true, errors.Join(err, failErr)
		}
		return true, nil
	}
	return true, err
}

// Run polls until context cancellation. Per-Job failures are durably reported or
// left to lease expiry and retried after PollInterval.
func (r Runner) Run(ctx context.Context) error {
	if err := r.Validate(); err != nil {
		return err
	}
	for {
		_, err := r.RunOnce(ctx)
		if err != nil && (memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeUnauthorized) ||
			memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeIncompatible)) {
			return err
		}
		timer := time.NewTimer(r.Options.PollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func callModelGenerator(ctx context.Context, generator ModelGenerator, request GenerationRequest) (response GenerationResponse, err error) {
	defer func() {
		if recover() != nil {
			response = GenerationResponse{}
			err = &GenerationError{Code: "generator_panic", Retryable: true}
		}
	}()
	return generator.Generate(ctx, request)
}

func callGenerator(ctx context.Context, generator Generator, request stewardv1alpha1.WorkRequest) (proposal stewardv1alpha1.Proposal, err error) {
	defer func() {
		if recover() != nil {
			proposal = stewardv1alpha1.Proposal{}
			err = &GenerationError{Code: "generator_panic", Retryable: true}
		}
	}()
	return generator.Generate(ctx, request)
}

func classifyGenerationError(err error) (string, bool) {
	var generationErr *GenerationError
	if errors.As(err, &generationErr) {
		return safeCode(generationErr.Code), generationErr.Retryable
	}
	return "generator_failure", true
}

func safeCode(code string) string {
	if code == "" || len(code) > 64 || strings.TrimSpace(code) != code {
		return "generator_failure"
	}
	for _, char := range code {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return "generator_failure"
		}
	}
	return code
}
