package conformance

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// DurableFixture is supplied by a real-storage, separate-process harness.
// CrashAndRestart must kill the process without graceful shutdown and return a
// client connected to a new process over the same durable authority.
type DurableFixture struct {
	Service         v1alpha1.DataPlane
	Authorization   v1alpha1.CallAuthorization
	CrashAndRestart func(*testing.T) v1alpha1.DataPlane
}

// DurableFactory starts a fresh separate-process fixture.
type DurableFactory func(*testing.T) DurableFixture

// RunDurable proves acknowledged durability and cross-restart idempotency. It
// complements RunSemantic; it never treats an in-process close/open as proof of
// INV-DUR-001.
func RunDurable(t *testing.T, factory DurableFactory) {
	t.Helper()
	fixture := factory(t)
	request := v1alpha1.RememberRequest{
		Text:           "acknowledged receipt survives immediate process kill",
		IdempotencyKey: "inv-dur-001",
	}
	first, err := fixture.Service.Remember(context.Background(), fixture.Authorization, request)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Accepted || first.ReceiptID == "" {
		t.Fatalf("Remember response = %+v, want durable acceptance", first)
	}
	restarted := fixture.CrashAndRestart(t)
	recalled, err := restarted.Recall(context.Background(), fixture.Authorization, v1alpha1.RecallRequest{
		Query:               "immediate process kill",
		MinConsistencyToken: first.ConsistencyToken,
		Budget: v1alpha1.RecallBudget{
			MaxFragments: 8,
			MaxBytes:     4096,
			DeadlineMS:   int((5 * time.Second) / time.Millisecond),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, fragment := range recalled.Fragments {
		if fragment.Text == request.Text {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Recall after crash = %+v, want acknowledged receipt", recalled.Fragments)
	}
	retry, err := restarted.Remember(context.Background(), fixture.Authorization, request)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ReceiptID != first.ReceiptID || !retry.DeduplicatedRetry {
		t.Fatalf("retry after crash = %+v, first = %+v", retry, first)
	}
}
