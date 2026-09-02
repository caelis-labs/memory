package stewardworker

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
)

const (
	builtInProfileID      stewardv1alpha1.ProfileID = "memory-default"
	builtInProfileVersion uint64                    = 1
	maxEnvelopeOverhead                             = 4 << 10
)

// GenerationRequest is the provider-neutral model contract prepared by
// Memory. Instructions contain the complete response shape, so correctness
// never depends on a provider implementing native JSON Schema output.
type GenerationRequest struct {
	Instructions   string
	Input          string
	JSONSchema     map[string]any
	MaxOutputBytes int
}

// GenerationResponse is untrusted provider text plus its envelope mode. Memory
// owns extraction, decoding, shape validation, and canonical application.
type GenerationResponse struct {
	Text      string
	ParseMode ParseMode
}

// ParseMode controls only provider-envelope compatibility. Proposal shape and
// authority validation are identical in every mode.
type ParseMode string

const (
	// ParseModeStrict requires one standalone JSON object.
	ParseModeStrict ParseMode = "strict"
	// ParseModeText accepts exactly one unambiguous top-level JSON object inside
	// a plain-text provider response.
	ParseModeText ParseMode = "text"
)

// BuiltInProfile returns Memory's default organization policy. Hosts select a
// model and execute prepared requests but do not redefine this policy.
func BuiltInProfile() stewardv1alpha1.ProfileSpec {
	return stewardv1alpha1.ProfileSpec{
		ProfileID: builtInProfileID,
		Version:   builtInProfileVersion,
		SystemPrompt: `Preserve durable facts, preferences, decisions, and commitments as compact recall-oriented records.
Use ADD unless one supplied record clearly describes the same subject and attribute. Use MERGE only for compatible additions, and SUPERSEDE only when the new receipt explicitly replaces an earlier value. Use IGNORE for transient chatter or content with no durable fact.
The assigned receipt is input.receipt.receipt_id. Every ADD, MERGE, or SUPERSEDE response must copy that exact value into evidence_refs; never omit or substitute it.
Write self-contained record text. Preserve exact names and values. Improve future lexical Recall by adding at most two commonplace, unambiguous search aliases without changing the claim: expand a recognized technical abbreviation, add an established Chinese/English equivalent, or add one immediate technical category. Retain the original term in parentheses. General language and technical knowledge may supply an alias, but never a new fact, value, cause, recommendation, or explanation.
Examples of the intended normalization:
- "接口使用 JWT。" becomes "接口使用 JSON Web Token (JWT) 作为访问凭证。"
- "服务每周一发布。" becomes "服务每星期一（周一）发布。"
- "下游异常时启用熔断。" becomes "下游异常时启用熔断（circuit breaker）机制。"
For ADD, cite the assigned receipt. For MERGE or SUPERSEDE, use the exact supplied target and revision and cite only the assigned receipt plus evidence references from that target. Return exactly one JSON object and no prose.`,
		MaxContextRecords: 16,
		MaxInputBytes:     128 << 10,
		MaxOutputBytes:    4 << 10,
	}
}

type generationInput struct {
	Protocol          string                             `json:"protocol"`
	ProfileID         stewardv1alpha1.ProfileID          `json:"profile_id"`
	ProfileVersion    uint64                             `json:"profile_version"`
	Receipt           stewardv1alpha1.ReceiptInput       `json:"receipt"`
	Records           []stewardv1alpha1.RecordContext    `json:"records"`
	LexiconCandidates []stewardv1alpha1.LexiconCandidate `json:"lexicon_candidates,omitempty"`
}

// PrepareGeneration renders the complete Memory-owned prompt, bounded model
// input, and optional provider optimization for one claimed WorkRequest.
func PrepareGeneration(request stewardv1alpha1.WorkRequest) (GenerationRequest, error) {
	if request.Protocol != stewardv1alpha1.ProtocolVersion {
		return GenerationRequest{}, fmt.Errorf("unsupported Steward protocol %q", request.Protocol)
	}
	if err := request.Profile.Validate(); err != nil {
		return GenerationRequest{}, fmt.Errorf("invalid Steward profile: %w", err)
	}
	input, err := json.Marshal(generationInput{
		Protocol:          request.Protocol,
		ProfileID:         request.Profile.ProfileID,
		ProfileVersion:    request.Profile.Version,
		Receipt:           request.Receipt,
		Records:           request.Records,
		LexiconCandidates: request.LexiconCandidates,
	})
	if err != nil {
		return GenerationRequest{}, fmt.Errorf("encode Steward generation input: %w", err)
	}
	hasLexiconCandidates := len(request.LexiconCandidates) > 0
	return GenerationRequest{
		Instructions:   generationInstructions(request.Profile.SystemPrompt, hasLexiconCandidates),
		Input:          string(input),
		JSONSchema:     proposalJSONSchema(hasLexiconCandidates),
		MaxOutputBytes: request.Profile.MaxOutputBytes,
	}, nil
}

func generationInstructions(profilePrompt string, hasLexiconCandidates bool) string {
	keys := "operation, target_record_id, expected_revision, kind, text, and evidence_refs"
	if hasLexiconCandidates {
		keys += ", plus the optional lexicon_terms field described below"
	}
	instructions := "Memory appliance policy for this job:\n" + strings.TrimSpace(profilePrompt)
	instructions += `

The response may use only these top-level keys: ` + keys + `. Return one of these exact JSON shapes with no Markdown fence, prose, comments, or unused fields:
ADD: {"operation":"ADD","kind":"fact","text":"...","evidence_refs":["assigned-receipt-id"]}
MERGE: {"operation":"MERGE","target_record_id":"supplied-record-id","expected_revision":1,"kind":"supplied-kind","text":"...","evidence_refs":["assigned-receipt-id","retained-target-evidence-id"]}
SUPERSEDE: {"operation":"SUPERSEDE","target_record_id":"supplied-record-id","expected_revision":1,"kind":"fact","text":"...","evidence_refs":["assigned-receipt-id","supported-target-evidence-id"]}
IGNORE: {"operation":"IGNORE"}`
	if !hasLexiconCandidates {
		return instructions
	}
	return instructions + `
The input contains lexicon_candidates. The response may additionally contain lexicon_terms with only exact candidate term values that are meaningful local compound names. Never invent or normalize a term; omit lexicon_terms when no candidate should be approved.`
}

func proposalJSONSchema(hasLexiconCandidates bool) map[string]any {
	properties := map[string]any{
		"operation": map[string]any{
			"type": "string", "enum": []any{"ADD", "MERGE", "SUPERSEDE", "IGNORE"},
		},
		"target_record_id":  map[string]any{"type": "string"},
		"expected_revision": map[string]any{"type": "integer", "minimum": 0},
		"kind":              map[string]any{"type": "string"},
		"text":              map[string]any{"type": "string"},
		"evidence_refs": map[string]any{
			"type": "array", "items": map[string]any{"type": "string"},
		},
	}
	if hasLexiconCandidates {
		properties["lexicon_terms"] = map[string]any{
			"type": "array", "maxItems": stewardv1alpha1.MaxLexiconTerms,
			"items": map[string]any{"type": "string"},
		}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             []any{"operation"},
	}
}

// ParseProposal extracts and strictly validates one untrusted model proposal.
func ParseProposal(text string, mode ParseMode) (stewardv1alpha1.Proposal, error) {
	if len(text) > stewardv1alpha1.MaxRecordTextBytes+maxEnvelopeOverhead {
		return stewardv1alpha1.Proposal{}, fmt.Errorf("Steward output exceeds local parse limit")
	}
	candidate := strings.TrimSpace(text)
	switch mode {
	case ParseModeStrict:
	case ParseModeText:
		var err error
		candidate, err = singleJSONObject(candidate)
		if err != nil {
			return stewardv1alpha1.Proposal{}, err
		}
	default:
		return stewardv1alpha1.Proposal{}, fmt.Errorf("unsupported Steward parse mode %q", mode)
	}
	decoder := json.NewDecoder(strings.NewReader(candidate))
	decoder.DisallowUnknownFields()
	var proposal stewardv1alpha1.Proposal
	if err := decoder.Decode(&proposal); err != nil {
		return stewardv1alpha1.Proposal{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return stewardv1alpha1.Proposal{}, fmt.Errorf("multiple JSON values")
		}
		return stewardv1alpha1.Proposal{}, err
	}
	if err := proposal.ValidateShape(); err != nil {
		return stewardv1alpha1.Proposal{}, err
	}
	return proposal, nil
}

func singleJSONObject(text string) (string, error) {
	var candidates []string
	start, depth := -1, 0
	inString, escaped := false, false
	for index := 0; index < len(text); index++ {
		char := text[index]
		if depth == 0 {
			switch char {
			case '{':
				start, depth = index, 1
			case '}':
				return "", fmt.Errorf("Steward output contains unbalanced JSON braces")
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch char {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				candidates = append(candidates, text[start:index+1])
				start = -1
			}
		}
	}
	if depth != 0 || inString {
		return "", fmt.Errorf("Steward output contains incomplete JSON")
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("Steward output contains no JSON object")
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("Steward output contains more than one JSON object")
	}
}
