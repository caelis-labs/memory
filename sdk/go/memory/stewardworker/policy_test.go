package stewardworker

import (
	"strings"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
)

func TestBuiltInProfileAndPromptOwnExactTextContract(t *testing.T) {
	profile := BuiltInProfile()
	if profile.ProfileID != "memory-default" || profile.Version != 1 || profile.MaxContextRecords != 16 ||
		profile.MaxInputBytes != 128<<10 || profile.MaxOutputBytes != 4<<10 {
		t.Fatalf("BuiltInProfile() = %+v", profile)
	}
	prepared, err := PrepareGeneration(testWorkRequest(profile))
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{`"operation":"ADD"`, `"operation":"MERGE"`, `"operation":"SUPERSEDE"`, `"operation":"IGNORE"`, "with no Markdown fence"} {
		if !strings.Contains(prepared.Instructions, phrase) {
			t.Fatalf("prepared instructions are missing %q", phrase)
		}
	}
	if strings.Contains(prepared.Instructions, "lexicon_terms") {
		t.Fatal("ordinary built-in prompt exposed experimental lexicon terms")
	}
	properties := prepared.JSONSchema["properties"].(map[string]any)
	if properties["lexicon_terms"] != nil {
		t.Fatal("ordinary built-in schema exposed experimental lexicon terms")
	}
	if !strings.Contains(prepared.Input, `"profile_id":"memory-default"`) || strings.Contains(prepared.Input, profile.SystemPrompt) {
		t.Fatalf("prepared input has wrong policy projection: %s", prepared.Input)
	}
}

func TestPrepareGenerationIncludesBoundedExperimentalLexiconContract(t *testing.T) {
	request := testWorkRequest(BuiltInProfile())
	request.LexiconCandidates = []stewardv1alpha1.LexiconCandidate{{Term: "量子织网", DocumentFrequency: 3}}
	prepared, err := PrepareGeneration(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prepared.Instructions, "lexicon_terms") || !strings.Contains(prepared.Input, `"term":"量子织网"`) {
		t.Fatalf("experimental lexicon contract = %+v", prepared)
	}
	properties := prepared.JSONSchema["properties"].(map[string]any)
	if properties["lexicon_terms"] == nil {
		t.Fatal("experimental schema omitted lexicon_terms")
	}
}

func TestParseProposalDoesNotDependOnNativeSchemaOutput(t *testing.T) {
	value := `{"operation":"ADD","kind":"fact","text":"durable","evidence_refs":["receipt-1"]}`
	proposal, err := ParseProposal("Result:\n```json\n"+value+"\n```", ParseModeText)
	if err != nil || proposal.Operation != stewardv1alpha1.OperationAdd || proposal.Text != "durable" {
		t.Fatalf("ParseProposal(text) = %+v, %v", proposal, err)
	}
	if _, err := ParseProposal("Result: "+value, ParseModeStrict); err == nil {
		t.Fatal("strict parser accepted a provider text envelope")
	}
	if _, err := ParseProposal(value+"\n"+`{"operation":"IGNORE"}`, ParseModeText); err == nil {
		t.Fatal("text parser accepted ambiguous JSON objects")
	}
	if _, err := ParseProposal(`{"operation":"IGNORE","extra":true}`, ParseModeText); err == nil {
		t.Fatal("parser accepted an unknown field")
	}
	if _, err := ParseProposal(strings.Repeat("x", stewardv1alpha1.MaxRecordTextBytes+maxEnvelopeOverhead+1), ParseModeText); err == nil ||
		!strings.Contains(err.Error(), "parse limit") {
		t.Fatalf("oversized envelope error = %v", err)
	}
}

func testWorkRequest(profile stewardv1alpha1.ProfileSpec) stewardv1alpha1.WorkRequest {
	return stewardv1alpha1.WorkRequest{
		Protocol: stewardv1alpha1.ProtocolVersion,
		Profile:  profile,
		Receipt: stewardv1alpha1.ReceiptInput{
			ReceiptID: "receipt-1", Text: "durable", ReceivedAt: time.Unix(1, 0).UTC(),
		},
		Records: []stewardv1alpha1.RecordContext{},
	}
}
