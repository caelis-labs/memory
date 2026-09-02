package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"

	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestProposalShapeVocabularyAndBounds(t *testing.T) {
	valid := []Proposal{
		{Operation: OperationIgnore},
		{Operation: OperationIgnore, LexiconTerms: []string{"量子织网"}},
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
		{Operation: OperationIgnore, LexiconTerms: []string{"bad term"}},
		{Operation: OperationIgnore, LexiconTerms: []string{"量子织网", "量子织网"}},
	}
	for _, proposal := range invalid {
		if err := proposal.ValidateShape(); err == nil {
			t.Fatalf("ValidateShape(%+v) succeeded", proposal)
		}
	}
}

func TestProfileAndWorkRequestBounds(t *testing.T) {
	profile := ProfileSpec{
		ProfileID: "profile-a", Version: 1,
		SystemPrompt: "organize supplied evidence", MaxContextRecords: 16,
		MaxInputBytes: 128 << 10, MaxOutputBytes: 16 << 10,
	}
	if err := profile.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]ProfileSpec{
		"empty":      {},
		"mutable ID": func() ProfileSpec { value := profile; value.ProfileID = " profile-a"; return value }(),
		"prompt too big": func() ProfileSpec {
			value := profile
			value.SystemPrompt = strings.Repeat("x", (32<<10)+1)
			return value
		}(),
		"input too low":  func() ProfileSpec { value := profile; value.MaxInputBytes = 1024; return value }(),
		"output too big": func() ProfileSpec { value := profile; value.MaxOutputBytes = (128 << 10) + 1; return value }(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := invalid.Validate(); err == nil {
				t.Fatalf("ProfileSpec %+v passed validation", invalid)
			}
		})
	}
	request := WorkRequest{
		Protocol: ProtocolVersion, Profile: profile,
		Receipt: ReceiptInput{ReceiptID: "receipt-a", Text: "fact"}, Records: []RecordContext{},
	}
	size, err := request.EncodedSize()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if size != len(encoded) || strings.Contains(string(encoded), "space_id") || strings.Contains(string(encoded), "job_id") || strings.Contains(string(encoded), "lease") {
		t.Fatalf("WorkRequest size=%d JSON=%s", size, encoded)
	}
}
