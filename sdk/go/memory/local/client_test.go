package local

import (
	"context"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestTransportFailureClassification(t *testing.T) {
	client := NewClient(filepath.Join(t.TempDir(), "absent.sock"))
	if _, err := client.CheckCompatibility(context.Background(), CompatibilityExpectation{}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeInvalidArgument) {
		t.Fatalf("CheckCompatibility(empty pin) error = %v, want invalid_argument", err)
	}
	if _, err := client.Compatibility(context.Background()); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnavailable) {
		t.Fatalf("Compatibility() error = %v, want unavailable", err)
	}
	auth := v1alpha1.CallAuthorization{
		Capability: "opaque", ActorRef: "actor", Audience: v1alpha1.AudiencePrivate,
	}
	_, err := client.Remember(context.Background(), auth, v1alpha1.RememberRequest{
		Text: "effect with uncertain submission", IdempotencyKey: "stable-key",
	})
	if !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnknownOutcome) {
		t.Fatalf("Remember() error = %v, want unknown_outcome", err)
	}
	_, err = client.Recall(context.Background(), auth, v1alpha1.RecallRequest{
		Query: "fact", Budget: v1alpha1.RecallBudget{MaxFragments: 1, MaxBytes: 64, DeadlineMS: 100},
	})
	if !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnavailable) {
		t.Fatalf("Recall() error = %v, want unavailable", err)
	}
}
