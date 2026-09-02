// Package appliance exposes the durable Memory runtime for in-process hosts.
//
// The runtime owns Memory domain state, schema migration, retrieval, and
// Steward work. Hosts provide a data directory and bind the versioned Memory
// APIs directly; they never access the SQLite database or internal packages.
package appliance

import (
	"context"
	"errors"
	"fmt"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	core "github.com/caelis-labs/memory/internal/appliance"
	"github.com/caelis-labs/memory/sdk/go/memory/stewardworker"
)

// Options configures one embedded Memory runtime.
type Options struct {
	DataDir string
}

// Management is the narrow owner plane needed by an embedding to provision
// topology and select Steward policy. Memory remains the authority for every
// mutation performed through this interface.
type Management interface {
	Bootstrap(context.Context, managementv1alpha1.BootstrapRequest) (managementv1alpha1.BootstrapResponse, error)
	Inspect(context.Context) (managementv1alpha1.Inspection, error)
	RotateIssuerCredential(context.Context, string) (managementv1alpha1.IssuerAuthorization, error)
	PutStewardProfile(context.Context, managementv1alpha1.PutStewardProfileRequest) (managementv1alpha1.PutStewardProfileResponse, error)
	BindStewardProfile(context.Context, managementv1alpha1.BindStewardProfileRequest) (managementv1alpha1.BindStewardProfileResponse, error)
	DisableSteward(context.Context, managementv1alpha1.DisableStewardRequest) (managementv1alpha1.DisableStewardResponse, error)
	GetStewardConfiguration(context.Context) (managementv1alpha1.StewardConfiguration, error)
}

// Runtime is one synchronously opened durable Memory component.
type Runtime struct {
	store *core.Store
}

// Open opens storage, applies schema migrations, and returns an in-process
// runtime. Failure is reported synchronously to the embedding Host.
func Open(ctx context.Context, options Options) (*Runtime, error) {
	store, err := core.Open(ctx, core.Options{DataDir: options.DataDir})
	if err != nil {
		return nil, err
	}
	if err := store.Ready(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("open embedded Memory: %w", err)
	}
	return &Runtime{store: store}, nil
}

// DataPlane returns the stable Remember/Recall service boundary.
func (r *Runtime) DataPlane() memoryv1alpha1.DataPlane {
	if r == nil {
		return nil
	}
	return r.store
}

// Management returns the appliance-owned management plane.
func (r *Runtime) Management() Management {
	if r == nil {
		return nil
	}
	return r.store
}

// StewardWorker returns the appliance-owned Steward work plane.
func (r *Runtime) StewardWorker() stewardworker.Worker {
	if r == nil {
		return nil
	}
	return embeddedStewardWorker{store: r.store}
}

// IssueCapability authenticates one persisted issuer delegation and returns
// bounded Runtime authority for the requested operation set.
func (r *Runtime) IssueCapability(
	ctx context.Context,
	issuerCredential string,
	request memoryv1alpha1.CapabilityIssueRequest,
) (memoryv1alpha1.RuntimeCapability, error) {
	if r == nil || r.store == nil {
		return memoryv1alpha1.RuntimeCapability{}, unavailableError("Memory runtime is closed")
	}
	issued, err := r.store.IssueCapability(ctx, core.IssueCapabilityRequest{
		Authorization: managementv1alpha1.IssuerAuthorization{
			PrincipalRef: request.PrincipalRef,
			Credential:   issuerCredential,
		},
		GrantRef:   request.GrantRef,
		ActorRef:   request.ActorRef,
		Audience:   request.Audience,
		Operations: request.Operations,
		TTLSeconds: request.TTLSeconds,
	})
	if err != nil {
		switch {
		case errors.Is(err, core.ErrCapabilityIssueInvalid):
			return memoryv1alpha1.RuntimeCapability{}, serviceError(memoryv1alpha1.ErrorCodeInvalidArgument, "Memory capability request is invalid", false)
		case errors.Is(err, core.ErrCapabilityIssueUnauthorized):
			return memoryv1alpha1.RuntimeCapability{}, serviceError(memoryv1alpha1.ErrorCodeUnauthorized, "Memory capability request is unauthorized", false)
		default:
			return memoryv1alpha1.RuntimeCapability{}, err
		}
	}
	return memoryv1alpha1.RuntimeCapability{Token: issued.Token, ExpiresAt: issued.ExpiresAt}, nil
}

// Close releases the database and owner lock.
func (r *Runtime) Close() error {
	if r == nil || r.store == nil {
		return nil
	}
	err := r.store.Close()
	r.store = nil
	return err
}

type embeddedStewardWorker struct {
	store *core.Store
}

func (w embeddedStewardWorker) Claim(ctx context.Context, leaseDuration time.Duration) (stewardv1alpha1.ClaimResponse, error) {
	if w.store == nil {
		return stewardv1alpha1.ClaimResponse{}, unavailableError("Memory runtime is closed")
	}
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

func (w embeddedStewardWorker) Apply(ctx context.Context, request stewardv1alpha1.ApplyRequest) (stewardv1alpha1.ApplyResponse, error) {
	if w.store == nil {
		return stewardv1alpha1.ApplyResponse{}, unavailableError("Memory runtime is closed")
	}
	result, err := w.store.ApplyStewardProposal(ctx, request.Lease, request.Proposal)
	if err != nil {
		return stewardv1alpha1.ApplyResponse{}, stewardServiceError(err)
	}
	return stewardv1alpha1.ApplyResponse{Result: result}, nil
}

func (w embeddedStewardWorker) Fail(ctx context.Context, request stewardv1alpha1.FailRequest) error {
	if w.store == nil {
		return unavailableError("Memory runtime is closed")
	}
	return stewardServiceError(w.store.ReportStewardFailure(ctx, request))
}

func stewardServiceError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, core.ErrStewardProposalInvalid):
		return serviceError(memoryv1alpha1.ErrorCodeInvalidArgument, "Steward proposal is invalid", false)
	case errors.Is(err, core.ErrStewardLeaseLost), errors.Is(err, core.ErrStewardConflict):
		return serviceError(memoryv1alpha1.ErrorCodeConflict, "Steward lease or proposal conflicted", false)
	case errors.Is(err, core.ErrStewardUnknownOutcome):
		return serviceError(memoryv1alpha1.ErrorCodeUnknownOutcome, "Steward apply outcome is unknown", true)
	default:
		return err
	}
}

func unavailableError(message string) error {
	return serviceError(memoryv1alpha1.ErrorCodeUnavailable, message, true)
}

func serviceError(code memoryv1alpha1.ErrorCode, message string, retryable bool) error {
	return &memoryv1alpha1.ServiceError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		RequestID: fmt.Sprintf("embedded-%s", code),
	}
}
