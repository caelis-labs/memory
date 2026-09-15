package appliance

import (
	"context"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	core "github.com/caelis-labs/memory/internal/appliance"
)

// Delegated data/read/evidence planes never hand out the internal Store. Each
// is an unexported adapter whose method set is exactly the API interface it
// promises, so a caller that receives one plane cannot type-assert it into
// another plane and invoke an owner-only operation without a capability. Before
// this, every plane returned the same dynamic *core.Store, and a read-only
// holder of facts.Reader could assert to Management or EvidenceService and call
// SetIngestionPolicy or DeleteReceipt.
//
// Every method forwards verbatim to the Store, so authorization, exact service
// errors, and shutdown behavior are unchanged. Only API types appear in these
// signatures; the Store stays unexported state on the adapter.

// dataPlanePlane is the only authority behind Runtime.DataPlane. Its method set
// is exactly the three data-plane operations.
type dataPlanePlane struct {
	store *core.Store
}

func (p dataPlanePlane) Remember(
	ctx context.Context,
	auth memoryv1alpha1.CallAuthorization,
	request memoryv1alpha1.RememberRequest,
) (memoryv1alpha1.RememberResponse, error) {
	return p.store.Remember(ctx, auth, request)
}

func (p dataPlanePlane) Recall(
	ctx context.Context,
	auth memoryv1alpha1.CallAuthorization,
	request memoryv1alpha1.RecallRequest,
) (memoryv1alpha1.RecallResponse, error) {
	return p.store.Recall(ctx, auth, request)
}

func (p dataPlanePlane) GetReceiptStatus(
	ctx context.Context,
	auth memoryv1alpha1.CallAuthorization,
	request memoryv1alpha1.GetReceiptStatusRequest,
) (memoryv1alpha1.ReceiptStatus, error) {
	return p.store.GetReceiptStatus(ctx, auth, request)
}

// factsReaderPlane is the only authority behind Runtime.Facts. Recall authority
// suffices for its three reads; it carries no write or governance method.
type factsReaderPlane struct {
	store *core.Store
}

func (p factsReaderPlane) ReadFacts(
	ctx context.Context,
	auth memoryv1alpha1.CallAuthorization,
	request facts.ReadRequest,
) (facts.ReadResponse, error) {
	return p.store.ReadFacts(ctx, auth, request)
}

func (p factsReaderPlane) FactHistory(
	ctx context.Context,
	auth memoryv1alpha1.CallAuthorization,
	request facts.HistoryRequest,
) (facts.ReadResponse, error) {
	return p.store.FactHistory(ctx, auth, request)
}

func (p factsReaderPlane) Changes(
	ctx context.Context,
	auth memoryv1alpha1.CallAuthorization,
	request facts.ChangesRequest,
) (facts.ChangesResponse, error) {
	return p.store.Changes(ctx, auth, request)
}

// evidencePlane is the only authority behind Runtime.Evidence. It exposes the
// intended trusted-host service: capability-checked ingestion and owner-only
// ingestion policy. It is not model-facing and carries no data-plane, reader, or
// Management method.
type evidencePlane struct {
	store *core.Store
}

func (p evidencePlane) SubmitEvidence(
	ctx context.Context,
	auth memoryv1alpha1.CallAuthorization,
	request facts.SubmitEvidenceRequest,
) (facts.SubmitEvidenceResponse, error) {
	return p.store.SubmitEvidence(ctx, auth, request)
}

func (p evidencePlane) SetIngestionPolicy(ctx context.Context, policy facts.IngestionPolicy) error {
	return p.store.SetIngestionPolicy(ctx, policy)
}
