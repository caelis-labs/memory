// Package steward executes durable appliance-owned semantic Jobs with
// replaceable model providers. Providers receive bounded proposals only.
package steward

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

// Provider turns one bounded structured request into an untrusted proposal.
type Provider interface {
	Propose(context.Context, stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error)
}

// ProviderError classifies a provider failure without carrying response
// content into durable status or ordinary logs.
type ProviderError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *ProviderError) Error() string {
	if e.Err == nil {
		return "Steward provider failed"
	}
	return "Steward provider failed: " + e.Err.Error()
}

func (e *ProviderError) Unwrap() error { return e.Err }

type jobStore interface {
	ClaimStewardJob(context.Context, time.Duration) (appliance.StewardWork, bool, error)
	ApplyStewardProposal(context.Context, appliance.StewardLease, stewardv1alpha1.Proposal) (stewardv1alpha1.ApplyResult, error)
	FailStewardJob(context.Context, appliance.StewardLease, appliance.StewardFailure) error
}

// Options bounds one shared worker pool.
type Options struct {
	Workers       int
	LeaseDuration time.Duration
	PollInterval  time.Duration
	RetryBase     time.Duration
	MaxAttempts   int
}

// Validate rejects an unbounded worker configuration.
func (o Options) Validate() error {
	if o.Workers < 1 || o.Workers > 32 {
		return fmt.Errorf("Steward workers must be within 1..32")
	}
	if o.LeaseDuration < time.Second || o.LeaseDuration > 10*time.Minute {
		return fmt.Errorf("Steward lease must be within 1s..10m")
	}
	if o.PollInterval < 10*time.Millisecond || o.PollInterval > 10*time.Second {
		return fmt.Errorf("Steward poll interval must be within 10ms..10s")
	}
	if o.RetryBase < 10*time.Millisecond || o.RetryBase > time.Minute {
		return fmt.Errorf("Steward retry base must be within 10ms..1m")
	}
	if o.MaxAttempts < 1 || o.MaxAttempts > 20 {
		return fmt.Errorf("Steward max attempts must be within 1..20")
	}
	return nil
}

// Pool is a shared bounded executor. It owns no durable state beyond the
// appliance leases and can be stopped without losing accepted receipts.
type Pool struct {
	store     jobStore
	providers map[string]Provider
	options   Options
}

// NewPool validates and freezes provider routing and worker policy.
func NewPool(store jobStore, providers map[string]Provider, options Options) (*Pool, error) {
	if store == nil {
		return nil, fmt.Errorf("Steward job store is required")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("at least one Steward provider is required")
	}
	frozen := make(map[string]Provider, len(providers))
	for reference, provider := range providers {
		if reference == "" || provider == nil {
			return nil, fmt.Errorf("Steward provider reference and implementation are required")
		}
		if _, exists := frozen[reference]; exists {
			return nil, fmt.Errorf("duplicate Steward provider reference")
		}
		frozen[reference] = provider
	}
	return &Pool{store: store, providers: frozen, options: options}, nil
}

// Run starts all workers and blocks until context cancellation drains them.
func (p *Pool) Run(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Add(p.options.Workers)
	for range p.options.Workers {
		go func() {
			defer workers.Done()
			p.runWorker(ctx)
		}()
	}
	workers.Wait()
}

func (p *Pool) runWorker(ctx context.Context) {
	for {
		work, found, err := p.store.ClaimStewardJob(ctx, p.options.LeaseDuration)
		if err != nil || !found {
			if !waitContext(ctx, p.options.PollInterval) {
				return
			}
			continue
		}
		p.execute(ctx, work)
	}
}

func (p *Pool) execute(ctx context.Context, work appliance.StewardWork) {
	provider, exists := p.providers[work.ProviderRef]
	if !exists {
		p.fail(ctx, work, "provider_unavailable", work.Attempt >= p.options.MaxAttempts)
		return
	}
	requestTimeout := p.options.LeaseDuration - 100*time.Millisecond
	if requestTimeout <= 0 {
		requestTimeout = p.options.LeaseDuration
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	proposal, err := callProvider(requestCtx, provider, work.Request)
	cancel()
	if err != nil {
		var providerErr *ProviderError
		if errors.As(err, &providerErr) {
			code := providerErr.Code
			if code == "" {
				code = "provider_failure"
			}
			p.fail(ctx, work, code, !providerErr.Retryable || work.Attempt >= p.options.MaxAttempts)
			return
		}
		p.fail(ctx, work, "provider_failure", work.Attempt >= p.options.MaxAttempts)
		return
	}
	_, err = p.store.ApplyStewardProposal(ctx, work.Lease, proposal)
	if errors.Is(err, appliance.ErrStewardUnknownOutcome) {
		_, err = p.store.ApplyStewardProposal(ctx, work.Lease, proposal)
	}
	switch {
	case err == nil, errors.Is(err, appliance.ErrStewardLeaseLost):
		return
	case errors.Is(err, appliance.ErrStewardProposalInvalid):
		p.fail(ctx, work, "proposal_invalid", work.Attempt >= p.options.MaxAttempts)
	case errors.Is(err, appliance.ErrStewardConflict):
		p.fail(ctx, work, "proposal_conflict", work.Attempt >= p.options.MaxAttempts)
	default:
		p.fail(ctx, work, "apply_unavailable", work.Attempt >= p.options.MaxAttempts)
	}
}

func (p *Pool) fail(ctx context.Context, work appliance.StewardWork, code string, terminal bool) {
	failure := appliance.StewardFailure{Code: safeFailureCode(code), Terminal: terminal}
	if !terminal {
		failure.RetryAfter = retryDelay(p.options.RetryBase, work.Attempt)
	}
	_ = p.store.FailStewardJob(ctx, work.Lease, failure)
}

func callProvider(
	ctx context.Context,
	provider Provider,
	request stewardv1alpha1.WorkRequest,
) (proposal stewardv1alpha1.Proposal, err error) {
	defer func() {
		if recover() != nil {
			proposal = stewardv1alpha1.Proposal{}
			err = &ProviderError{Code: "provider_panic", Retryable: true}
		}
	}()
	return provider.Propose(ctx, request)
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 10 {
		shift = 10
	}
	delay := base * time.Duration(1<<shift)
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func safeFailureCode(code string) string {
	if len(code) == 0 || len(code) > 64 {
		return "provider_failure"
	}
	for _, char := range code {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return "provider_failure"
		}
	}
	return code
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
