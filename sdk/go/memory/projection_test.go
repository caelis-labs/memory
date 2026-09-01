package memory

import (
	"encoding/json"
	"errors"
	"testing"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestModelToolInputsExposeOnlyNarrowArguments(t *testing.T) {
	remember, err := json.Marshal(RememberToolInput{Text: "fact"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"text":"fact"}`; string(remember) != want {
		t.Fatalf("Remember input = %s, want %s", remember, want)
	}
	recall, err := json.Marshal(RecallToolInput{Query: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"query":"preference"}`; string(recall) != want {
		t.Fatalf("Recall input = %s, want %s", recall, want)
	}
}

func TestProjectRememberGolden(t *testing.T) {
	got, err := ProjectRemember(v1alpha1.RememberResponse{
		Accepted:         true,
		ReceiptID:        "receipt-secret",
		ConsistencyToken: "token-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"accepted":true}`; string(got) != want {
		t.Fatalf("ProjectRemember() = %s, want %s", got, want)
	}
}

func TestProjectRecallGolden(t *testing.T) {
	got, err := ProjectRecall(v1alpha1.RecallResponse{
		Fragments: []v1alpha1.RecallFragment{
			{FragmentID: "hidden-1", Text: "first fact", EvidenceRefs: []v1alpha1.ReceiptID{"receipt-1"}},
			{FragmentID: "hidden-2", Text: "second fact", SpaceClass: v1alpha1.SpaceClassPrivate},
		},
		ConsistencyToken: "hidden-token",
		Degraded:         true,
	}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"fragments":["first fact","second fact"]}`; string(got) != want {
		t.Fatalf("ProjectRecall() = %s, want %s", got, want)
	}
}

func TestProjectRecallEmptyUsesArray(t *testing.T) {
	got, err := ProjectRecall(v1alpha1.RecallResponse{}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"fragments":[]}`; string(got) != want {
		t.Fatalf("ProjectRecall() = %s, want %s", got, want)
	}
}

func TestProjectRecallChecksEscapedBytes(t *testing.T) {
	response := v1alpha1.RecallResponse{
		Fragments: []v1alpha1.RecallFragment{{Text: "\n\n\n\n"}},
	}
	projected, err := ProjectRecall(response, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) <= len(response.Fragments[0].Text) {
		t.Fatalf("test fixture did not expand when JSON encoded: %s", projected)
	}
	_, err = ProjectRecall(response, len(projected)-1)
	if !errors.Is(err, ErrProjectionBudgetExceeded) {
		t.Fatalf("ProjectRecall() error = %v", err)
	}
}
