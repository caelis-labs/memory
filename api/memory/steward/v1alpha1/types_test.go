package v1alpha1

import (
	"strings"
	"testing"

	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestProposalShapeVocabularyAndBounds(t *testing.T) {
	valid := []Proposal{
		{Operation: OperationIgnore},
		{Operation: OperationAdd, Kind: "claim", Text: "the service uses Go", EvidenceRefs: []memoryv1alpha1.ReceiptID{"receipt-a"}},
		{Operation: OperationMerge, TargetRecordID: "record-a", ExpectedRevision: 1, Kind: "claim", Text: "merged", EvidenceRefs: []memoryv1alpha1.ReceiptID{"receipt-a", "receipt-b"}},
		{Operation: OperationSupersede, TargetRecordID: "record-a", ExpectedRevision: 2, Kind: "claim", Text: "current", EvidenceRefs: []memoryv1alpha1.ReceiptID{"receipt-c"}},
	}
	for _, proposal := range valid {
		if err := proposal.ValidateShape(); err != nil {
			t.Fatalf("ValidateShape(%+v) = %v", proposal, err)
		}
	}
	invalid := []Proposal{
		{},
		{Operation: "DELETE"},
		{Operation: OperationIgnore, Text: "mutation"},
		{Operation: OperationAdd, TargetRecordID: "record-a", Kind: "claim", Text: "text", EvidenceRefs: []memoryv1alpha1.ReceiptID{"receipt-a"}},
		{Operation: OperationMerge, Kind: "claim", Text: "text", EvidenceRefs: []memoryv1alpha1.ReceiptID{"receipt-a"}},
		{Operation: OperationAdd, Kind: " claim", Text: "text", EvidenceRefs: []memoryv1alpha1.ReceiptID{"receipt-a"}},
		{Operation: OperationAdd, Kind: "claim", Text: strings.Repeat("x", MaxRecordTextBytes+1), EvidenceRefs: []memoryv1alpha1.ReceiptID{"receipt-a"}},
		{Operation: OperationAdd, Kind: "claim", Text: "text", EvidenceRefs: []memoryv1alpha1.ReceiptID{"receipt-a", "receipt-a"}},
	}
	for _, proposal := range invalid {
		if err := proposal.ValidateShape(); err == nil {
			t.Fatalf("ValidateShape(%+v) succeeded", proposal)
		}
	}
}
