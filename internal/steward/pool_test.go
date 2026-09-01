package steward

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

func TestPoolRetriesUnknownApplyWithIdenticalLeaseAndProposal(t *testing.T) {
	store := &fakeJobStore{applyErrors: []error{appliance.ErrStewardUnknownOutcome, nil}}
	provider := providerFunc(func(_ context.Context, request stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error) {
		return stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore}, nil
	})
	pool := testPool(t, store, map[string]Provider{"provider-test": provider}, 3)
	work := testWork(1)
	pool.execute(t.Context(), work)
	if store.applyCalls != 2 {
		t.Fatalf("Apply calls = %d, want 2", store.applyCalls)
	}
	if store.firstLease != work.Lease || store.secondLease != work.Lease || !reflect.DeepEqual(store.firstProposal, store.secondProposal) {
		t.Fatalf("unknown outcome changed lease or proposal: %+v", store)
	}
	if len(store.failures) != 0 {
		t.Fatalf("unknown outcome produced failures %+v", store.failures)
	}
}

func TestPoolContainsProviderPanicAndBoundsRetries(t *testing.T) {
	provider := providerFunc(func(context.Context, stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error) {
		panic("receipt text must not escape through panic handling")
	})
	store := &fakeJobStore{}
	pool := testPool(t, store, map[string]Provider{"provider-test": provider}, 2)
	pool.execute(t.Context(), testWork(1))
	if len(store.failures) != 1 || store.failures[0].Code != "provider_panic" || store.failures[0].Terminal {
		t.Fatalf("first panic failure = %+v", store.failures)
	}
	if store.failures[0].RetryAfter != 10*time.Millisecond {
		t.Fatalf("first retry delay = %s", store.failures[0].RetryAfter)
	}
	store.failures = nil
	pool.execute(t.Context(), testWork(2))
	if len(store.failures) != 1 || !store.failures[0].Terminal || store.failures[0].Code != "provider_panic" {
		t.Fatalf("terminal panic failure = %+v", store.failures)
	}
}

func TestPoolClassifiesMissingAndTerminalProviderFailures(t *testing.T) {
	store := &fakeJobStore{}
	pool := testPool(t, store, map[string]Provider{"another-provider": providerFunc(func(context.Context, stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error) {
		return stewardv1alpha1.Proposal{}, nil
	})}, 1)
	pool.execute(t.Context(), testWork(1))
	if len(store.failures) != 1 || store.failures[0].Code != "provider_unavailable" || !store.failures[0].Terminal {
		t.Fatalf("missing provider failure = %+v", store.failures)
	}
	store.failures = nil
	pool.providers["provider-test"] = providerFunc(func(context.Context, stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error) {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "provider_rejected", Retryable: false, Err: errors.New("private response body")}
	})
	pool.execute(t.Context(), testWork(1))
	if len(store.failures) != 1 || store.failures[0].Code != "provider_rejected" || !store.failures[0].Terminal {
		t.Fatalf("terminal provider failure = %+v", store.failures)
	}
}

func TestPoolSerializesLocalJobTransitionsAcrossWorkers(t *testing.T) {
	store := &blockingJobStore{entered: make(chan struct{}, 2), release: make(chan struct{})}
	pool := testPool(t, store, map[string]Provider{
		"provider-test": providerFunc(func(context.Context, stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error) {
			return stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore}, nil
		}),
	}, 1)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		_, _, _ = pool.claim(t.Context())
	}()
	<-store.entered
	started := make(chan struct{})
	go func() {
		defer workers.Done()
		close(started)
		_, _ = pool.apply(t.Context(), testWork(1).Lease, stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore})
	}()
	<-started
	overlapped := false
	select {
	case <-store.entered:
		overlapped = true
	case <-time.After(100 * time.Millisecond):
	}
	close(store.release)
	workers.Wait()
	if overlapped {
		t.Fatal("local Job transitions overlapped")
	}
}

type providerFunc func(context.Context, stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error)

func (f providerFunc) Propose(ctx context.Context, request stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error) {
	return f(ctx, request)
}

type fakeJobStore struct {
	applyErrors    []error
	applyCalls     int
	firstLease     appliance.StewardLease
	secondLease    appliance.StewardLease
	firstProposal  stewardv1alpha1.Proposal
	secondProposal stewardv1alpha1.Proposal
	failures       []appliance.StewardFailure
}

type blockingJobStore struct {
	entered chan struct{}
	release chan struct{}
}

func (s *blockingJobStore) wait() {
	s.entered <- struct{}{}
	<-s.release
}

func (s *blockingJobStore) ClaimStewardJob(context.Context, time.Duration) (appliance.StewardWork, bool, error) {
	s.wait()
	return appliance.StewardWork{}, false, nil
}

func (s *blockingJobStore) ApplyStewardProposal(
	context.Context,
	appliance.StewardLease,
	stewardv1alpha1.Proposal,
) (stewardv1alpha1.ApplyResult, error) {
	s.wait()
	return stewardv1alpha1.ApplyResult{}, nil
}

func (*blockingJobStore) FailStewardJob(context.Context, appliance.StewardLease, appliance.StewardFailure) error {
	return nil
}

func (*fakeJobStore) ClaimStewardJob(context.Context, time.Duration) (appliance.StewardWork, bool, error) {
	return appliance.StewardWork{}, false, nil
}

func (s *fakeJobStore) ApplyStewardProposal(
	_ context.Context,
	lease appliance.StewardLease,
	proposal stewardv1alpha1.Proposal,
) (stewardv1alpha1.ApplyResult, error) {
	s.applyCalls++
	if s.applyCalls == 1 {
		s.firstLease, s.firstProposal = lease, proposal
	} else {
		s.secondLease, s.secondProposal = lease, proposal
	}
	if s.applyCalls <= len(s.applyErrors) {
		return stewardv1alpha1.ApplyResult{}, s.applyErrors[s.applyCalls-1]
	}
	return stewardv1alpha1.ApplyResult{}, nil
}

func (s *fakeJobStore) FailStewardJob(_ context.Context, _ appliance.StewardLease, failure appliance.StewardFailure) error {
	s.failures = append(s.failures, failure)
	return nil
}

func testPool(t *testing.T, store jobStore, providers map[string]Provider, maxAttempts int) *Pool {
	t.Helper()
	pool, err := NewPool(store, providers, Options{
		Workers: 1, LeaseDuration: time.Second, PollInterval: 10 * time.Millisecond,
		RetryBase: 10 * time.Millisecond, MaxAttempts: maxAttempts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func testWork(attempt int) appliance.StewardWork {
	return appliance.StewardWork{
		Lease:       appliance.StewardLease{JobID: "job-test", Token: "lease-test"},
		ProviderRef: "provider-test", Attempt: attempt,
		Request: stewardv1alpha1.WorkRequest{Protocol: stewardv1alpha1.ProtocolVersion},
	}
}
