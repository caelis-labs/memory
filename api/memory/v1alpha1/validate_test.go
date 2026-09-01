package v1alpha1

import (
	"strings"
	"testing"
	"time"
)

func TestSourceContextRejectsOversizedScalar(t *testing.T) {
	err := (SourceContext{ActorRef: strings.Repeat("a", 257)}).Validate()
	if err == nil {
		t.Fatal("Validate() accepted oversized actor_ref")
	}
}

func TestRememberRequestRejectsUnencodableOccurredAt(t *testing.T) {
	occurredAt := time.Date(10_000, 1, 1, 0, 0, 0, 0, time.UTC)
	request := RememberRequest{Text: "fact", IdempotencyKey: "key", OccurredAt: &occurredAt}
	if err := request.Validate(); err == nil {
		t.Fatal("Validate() accepted occurred_at outside RFC 3339")
	}
}

func TestRememberRequestRequiresTextAndKey(t *testing.T) {
	for name, request := range map[string]RememberRequest{
		"text": {IdempotencyKey: "key"},
		"key":  {Text: "fact"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := request.Validate(); err == nil {
				t.Fatal("Validate() accepted an incomplete request")
			}
		})
	}
}

func TestRecallBudgetBounds(t *testing.T) {
	valid := RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 2000}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid budget rejected: %v", err)
	}
	invalid := valid
	invalid.MaxFragments = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() accepted zero fragments")
	}
}
