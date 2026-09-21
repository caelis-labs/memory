package stewardworker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
)

func TestBuiltInProfileAndPromptOwnExactTextContract(t *testing.T) {
	profile := BuiltInProfile()
	if profile.ProfileID != "memory-default" || profile.Version != 2 || profile.MaxContextRecords != 16 ||
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
	if _, err := ParseProposal(strings.Repeat("x", maxEnvelopeBytes()+1), ParseModeText); err == nil ||
		!strings.Contains(err.Error(), "parse limit") {
		t.Fatalf("oversized envelope error = %v", err)
	}
}

func TestParseBoundedBatchProposal(t *testing.T) {
	value := `{"policy":"bounded_batch","ops":[{"operation":"ADD","kind":"fact","text":"a","evidence_refs":["receipt-1"]},{"operation":"IGNORE"}]}`
	proposal, err := ParseProposal(value, ParseModeStrict)
	if err != nil || !proposal.IsBatch() || len(proposal.Ops) != 2 {
		t.Fatalf("ParseProposal(batch) = %+v, %v", proposal, err)
	}
	prepared, err := PrepareGeneration(testWorkRequest(BuiltInProfile()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prepared.Instructions, `"policy":"bounded_batch"`) {
		t.Fatalf("prepared instructions omitted the bounded batch shape: %s", prepared.Instructions)
	}
	properties := prepared.JSONSchema["properties"].(map[string]any)
	if properties["policy"] == nil || properties["ops"] == nil {
		t.Fatal("prepared schema omitted bounded batch fields")
	}
	if _, err := ParseProposal(`{"ops":[{"operation":"IGNORE"}]}`, ParseModeStrict); err == nil {
		t.Fatal("parser accepted a batch without the explicit policy marker")
	}
	if _, err := ParseProposal(`{"policy":"bounded_batch","ops":[{"operation":"IGNORE","extra":1}]}`, ParseModeStrict); err == nil {
		t.Fatal("parser accepted an unknown field inside a batch op")
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

// Released specs are frozen independently of BuiltInProfile. v0.5.2 and v0.6.0
// accidentally shipped different specs at version 1; retain both as evidence.
// Changing any current policy field requires a new version and snapshot.
func TestBuiltInProfileMatchesReleasedSpecification(t *testing.T) {
	files, err := filepath.Glob("testdata/released_profiles/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatal("released profile snapshots are missing")
	}
	var latest stewardv1alpha1.ProfileSpec
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			Release      string                      `json:"release"`
			SourceCommit string                      `json:"source_commit"`
			Profile      stewardv1alpha1.ProfileSpec `json:"profile"`
		}
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.Release == "" {
			t.Fatalf("missing release provenance: %s", path)
		}
		if err := snapshot.Profile.Validate(); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if snapshot.Profile.Version > latest.Version {
			latest = snapshot.Profile
		}
	}
	if got := BuiltInProfile(); got != latest {
		t.Fatal("built-in policy differs from its frozen release specification; allocate a new immutable version")
	}
}
