package appliance_test

import (
	"context"
	"testing"

	factsv1alpha1 "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
)

// deleteReceiptOperation and setIngestionPolicyOperation are single management
// operations a narrow plane must never become reachable through a type
// assertion.
type deleteReceiptOperation interface {
	DeleteReceipt(context.Context, managementv1alpha1.DeleteReceiptRequest) (managementv1alpha1.DeleteReceiptResponse, error)
}

type setIngestionPolicyOperation interface {
	SetIngestionPolicy(context.Context, factsv1alpha1.IngestionPolicy) error
}

// TestEmbeddedPlanesCannotBeAssertedIntoOwnerAuthority is the accidental-
// escalation guard: a caller handed only one plane must not type-assert it into
// another plane's authority. Before the adapters existed every plane returned
// the same dynamic internal Store, so a read-only facts.Reader holder could
// assert to facts.EvidenceService or appliance.Management and call
// SetIngestionPolicy or DeleteReceipt with no capability at all.
func TestEmbeddedPlanesCannotBeAssertedIntoOwnerAuthority(t *testing.T) {
	runtime, err := appliance.Open(context.Background(), appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	dataPlane := runtime.DataPlane()
	reader := runtime.Facts()
	evidence := runtime.Evidence()
	management := runtime.Management()

	// Positive: every plane still satisfies exactly the public interface it
	// promises, and the owner plane stays complete.
	if _, ok := dataPlane.(memoryv1alpha1.DataPlane); !ok {
		t.Fatalf("DataPlane() = %T, want memoryv1alpha1.DataPlane", dataPlane)
	}
	if _, ok := reader.(factsv1alpha1.Reader); !ok {
		t.Fatalf("Facts() = %T, want facts.Reader", reader)
	}
	if _, ok := evidence.(factsv1alpha1.EvidenceService); !ok {
		t.Fatalf("Evidence() = %T, want facts.EvidenceService", evidence)
	}
	if _, ok := management.(appliance.Management); !ok {
		t.Fatalf("Management() = %T, want appliance.Management", management)
	}

	// Negative: the data plane cannot become a reader, an owner ingestion
	// service, the Management facade, or one of its operations.
	if _, ok := dataPlane.(factsv1alpha1.Reader); ok {
		t.Fatal("the data plane was assertable to facts.Reader")
	}
	if _, ok := dataPlane.(factsv1alpha1.EvidenceService); ok {
		t.Fatal("the data plane was assertable to facts.EvidenceService")
	}
	if _, ok := dataPlane.(appliance.Management); ok {
		t.Fatal("the data plane was assertable to appliance.Management")
	}
	if _, ok := dataPlane.(deleteReceiptOperation); ok {
		t.Fatal("the data plane was assertable to a Management owner operation")
	}

	// Negative: the read-only fact reader cannot obtain any write or governance
	// authority.
	if _, ok := reader.(factsv1alpha1.EvidenceService); ok {
		t.Fatal("the fact reader was assertable to facts.EvidenceService")
	}
	if _, ok := reader.(memoryv1alpha1.DataPlane); ok {
		t.Fatal("the fact reader was assertable to memoryv1alpha1.DataPlane")
	}
	if _, ok := reader.(appliance.Management); ok {
		t.Fatal("the fact reader was assertable to appliance.Management")
	}
	if _, ok := reader.(setIngestionPolicyOperation); ok {
		t.Fatal("the fact reader was assertable to SetIngestionPolicy")
	}
	if _, ok := reader.(deleteReceiptOperation); ok {
		t.Fatal("the fact reader was assertable to a Management owner operation")
	}

	// Negative: the trusted ingestion plane is neither a reader nor a data or
	// governance plane.
	if _, ok := evidence.(factsv1alpha1.Reader); ok {
		t.Fatal("the evidence plane was assertable to facts.Reader")
	}
	if _, ok := evidence.(memoryv1alpha1.DataPlane); ok {
		t.Fatal("the evidence plane was assertable to memoryv1alpha1.DataPlane")
	}
	if _, ok := evidence.(appliance.Management); ok {
		t.Fatal("the evidence plane was assertable to appliance.Management")
	}
	if _, ok := evidence.(deleteReceiptOperation); ok {
		t.Fatal("the evidence plane was assertable to a Management owner operation")
	}
}

func TestEmbeddedPlanesStillForwardWithExactAuthority(t *testing.T) {
	runtime, err := appliance.Open(context.Background(), appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx := context.Background()

	// Each adapter forwards to the appliance, so validation and authorization
	// behave exactly as before.
	if _, err := runtime.Facts().Changes(ctx, memoryv1alpha1.CallAuthorization{}, factsv1alpha1.ChangesRequest{Limit: 1}); !memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeUnauthorized) {
		t.Fatalf("Facts().Changes unauthorized error = %v", err)
	}
	if _, err := runtime.Facts().Changes(ctx, memoryv1alpha1.CallAuthorization{}, factsv1alpha1.ChangesRequest{}); !memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeInvalidArgument) {
		t.Fatalf("Facts().Changes validation error = %v", err)
	}
	if err := runtime.Evidence().SetIngestionPolicy(ctx, factsv1alpha1.IngestionPolicy{}); !memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeInvalidArgument) {
		t.Fatalf("Evidence().SetIngestionPolicy validation error = %v", err)
	}
	if _, err := runtime.DataPlane().GetReceiptStatus(ctx, memoryv1alpha1.CallAuthorization{}, memoryv1alpha1.GetReceiptStatusRequest{}); !memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeInvalidArgument) {
		t.Fatalf("DataPlane().GetReceiptStatus validation error = %v", err)
	}
}

func TestEmbeddedPlanesAreNilAfterClose(t *testing.T) {
	runtime, err := appliance.Open(context.Background(), appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.DataPlane() != nil || runtime.Facts() != nil || runtime.Evidence() != nil || runtime.Management() != nil {
		t.Fatal("closed runtime still exposes a plane")
	}
}
