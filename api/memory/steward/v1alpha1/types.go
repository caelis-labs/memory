// Package v1alpha1 defines the versioned provider-neutral Worker, proposal, and
// semantic-record contract. Worker output is untrusted input; only the
// appliance may apply it.
package v1alpha1

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const (
	ProtocolVersion     = "memory.steward.v1alpha1"
	MaxRecordTextBytes  = 32 << 10
	MaxRecordKindBytes  = 64
	MaxProposalEvidence = 32
	MaxLexiconTerms     = 16
)

// RecordID identifies appliance-owned interpreted continuity.
type RecordID string

// JobID identifies one durable receipt-organization effect.
type JobID string

// ProfileID identifies a versioned appliance prompt-policy profile.
type ProfileID string

// ProfileSpec is immutable prompt-policy configuration stored by the appliance.
// Provider, model, endpoint, and credential configuration belong downstream.
type ProfileSpec struct {
	ProfileID         ProfileID `json:"profile_id"`
	Version           uint64    `json:"version"`
	SystemPrompt      string    `json:"system_prompt"`
	MaxContextRecords int       `json:"max_context_records"`
	MaxInputBytes     int       `json:"max_input_bytes"`
	MaxOutputBytes    int       `json:"max_output_bytes"`
}

// Validate rejects unbounded or ambiguous profile configuration.
func (p ProfileSpec) Validate() error {
	if !boundedReference(string(p.ProfileID), 128) || p.Version == 0 {
		return fmt.Errorf("profile ID and non-zero version are required")
	}
	if !utf8.ValidString(p.SystemPrompt) || strings.TrimSpace(p.SystemPrompt) == "" || len(p.SystemPrompt) > 32<<10 {
		return fmt.Errorf("system prompt must be 1..32768 UTF-8 bytes")
	}
	if p.MaxContextRecords < 0 || p.MaxContextRecords > 64 {
		return fmt.Errorf("max context Records must be 0..64")
	}
	if p.MaxInputBytes < 128<<10 || p.MaxInputBytes > 1<<20 {
		return fmt.Errorf("max input bytes must be 131072..1048576")
	}
	if p.MaxOutputBytes < 1024 || p.MaxOutputBytes > 128<<10 {
		return fmt.Errorf("max output bytes must be 1024..131072")
	}
	return nil
}

// Profile is one immutable ProfileSpec plus its appliance creation time.
type Profile struct {
	ProfileSpec
	CreatedAt time.Time `json:"created_at"`
}

// ReceiptInput is the single immutable receipt assigned to a Steward Job.
type ReceiptInput struct {
	ReceiptID  memoryv1alpha1.ReceiptID `json:"receipt_id"`
	Text       string                   `json:"text"`
	OccurredAt *time.Time               `json:"occurred_at,omitempty"`
	ReceivedAt time.Time                `json:"received_at"`
}

// RecordContext is one active same-Space head the Worker may target. Space
// identity is intentionally absent.
type RecordContext struct {
	RecordID     RecordID                   `json:"record_id"`
	Revision     uint64                     `json:"revision"`
	Kind         string                     `json:"kind"`
	Text         string                     `json:"text"`
	EvidenceRefs []memoryv1alpha1.ReceiptID `json:"evidence_refs"`
}

// LexiconCandidate is a same-Space, evidence-backed local term near the static
// activation boundary. The downstream model can recommend it, but cannot see
// Space identity, evidence text beyond the assigned receipt, or index state.
type LexiconCandidate struct {
	Term              string  `json:"term"`
	DocumentFrequency int     `json:"document_frequency"`
	OccurrenceCount   int     `json:"occurrence_count"`
	LeftDiversity     int     `json:"left_diversity"`
	RightDiversity    int     `json:"right_diversity"`
	Score             float64 `json:"score"`
}

// WorkRequest is the bounded structured input passed to a downstream Generator.
// It deliberately contains no Job, Space, lease, bearer, or provider config.
type WorkRequest struct {
	Protocol          string             `json:"protocol"`
	Profile           ProfileSpec        `json:"profile"`
	Receipt           ReceiptInput       `json:"receipt"`
	Records           []RecordContext    `json:"records"`
	LexiconCandidates []LexiconCandidate `json:"lexicon_candidates,omitempty"`
}

// EncodedSize returns the exact JSON request size used for profile input
// budgeting.
func (r WorkRequest) EncodedSize() (int, error) {
	value, err := json.Marshal(r)
	return len(value), err
}

// Operation is the complete M4 proposal vocabulary.
type Operation string

const (
	OperationAdd       Operation = "ADD"
	OperationMerge     Operation = "MERGE"
	OperationSupersede Operation = "SUPERSEDE"
	OperationIgnore    Operation = "IGNORE"
)

// RecordStatus is canonical appliance state, never model-selected state.
type RecordStatus string

const (
	RecordStatusActive      RecordStatus = "active"
	RecordStatusInvalidated RecordStatus = "invalidated"
)

// Proposal is an untrusted candidate mutation. Job and Space identity are
// deliberately absent because they come from the durable lease.
type Proposal struct {
	Operation        Operation                  `json:"operation"`
	TargetRecordID   RecordID                   `json:"target_record_id,omitempty"`
	ExpectedRevision uint64                     `json:"expected_revision,omitempty"`
	Kind             string                     `json:"kind,omitempty"`
	Text             string                     `json:"text,omitempty"`
	EvidenceRefs     []memoryv1alpha1.ReceiptID `json:"evidence_refs,omitempty"`
	LexiconTerms     []string                   `json:"lexicon_terms,omitempty"`
}

// ValidateShape rejects unsupported operations and fields before canonical
// state, evidence, or authorization data are read.
func (p Proposal) ValidateShape() error {
	switch p.Operation {
	case OperationIgnore:
		if p.TargetRecordID != "" || p.ExpectedRevision != 0 || p.Kind != "" || p.Text != "" || len(p.EvidenceRefs) != 0 {
			return fmt.Errorf("IGNORE cannot contain mutation fields")
		}
	case OperationAdd:
		if p.TargetRecordID != "" || p.ExpectedRevision != 0 {
			return fmt.Errorf("ADD cannot target an existing Record")
		}
	case OperationMerge, OperationSupersede:
		if p.TargetRecordID == "" || p.ExpectedRevision == 0 {
			return fmt.Errorf("%s requires a target Record and expected revision", p.Operation)
		}
	default:
		return fmt.Errorf("unsupported proposal operation %q", p.Operation)
	}
	if p.Operation != OperationIgnore {
		if !utf8.ValidString(p.Kind) || p.Kind == "" || strings.TrimSpace(p.Kind) != p.Kind || len(p.Kind) > MaxRecordKindBytes || strings.ContainsAny(p.Kind, "\r\n\t") {
			return fmt.Errorf("record kind must be bounded non-whitespace UTF-8")
		}
		if !utf8.ValidString(p.Text) || strings.TrimSpace(p.Text) == "" || len(p.Text) > MaxRecordTextBytes {
			return fmt.Errorf("record text must be 1..%d UTF-8 bytes", MaxRecordTextBytes)
		}
		if len(p.EvidenceRefs) == 0 || len(p.EvidenceRefs) > MaxProposalEvidence {
			return fmt.Errorf("proposal evidence count must be 1..%d", MaxProposalEvidence)
		}
		seen := make(map[memoryv1alpha1.ReceiptID]struct{}, len(p.EvidenceRefs))
		for _, receiptID := range p.EvidenceRefs {
			if receiptID == "" {
				return fmt.Errorf("proposal evidence reference is empty")
			}
			if _, exists := seen[receiptID]; exists {
				return fmt.Errorf("proposal evidence references must be unique")
			}
			seen[receiptID] = struct{}{}
		}
	}
	if len(p.LexiconTerms) > MaxLexiconTerms {
		return fmt.Errorf("lexicon term count must be 0..%d", MaxLexiconTerms)
	}
	seenTerms := make(map[string]struct{}, len(p.LexiconTerms))
	for _, term := range p.LexiconTerms {
		if !utf8.ValidString(term) || strings.TrimSpace(term) != term || term == "" || len(term) > 128 || strings.ContainsAny(term, "\r\n\t ") {
			return fmt.Errorf("lexicon term must be bounded non-whitespace UTF-8")
		}
		if _, found := seenTerms[term]; found {
			return fmt.Errorf("lexicon terms must be unique")
		}
		seenTerms[term] = struct{}{}
	}
	return nil
}

func boundedReference(value string, limit int) bool {
	return utf8.ValidString(value) && value != "" && strings.TrimSpace(value) == value &&
		len(value) <= limit && !strings.ContainsAny(value, "\r\n\t")
}

// Evidence records the Space proven when the proposal was applied. A later
// receipt tombstone can invalidate active use without erasing revision audit.
type Evidence struct {
	ReceiptID memoryv1alpha1.ReceiptID `json:"receipt_id"`
	SpaceID   memoryv1alpha1.SpaceID   `json:"space_id"`
}

// Record is the mutable head pointer for immutable Revisions.
type Record struct {
	RecordID          RecordID               `json:"record_id"`
	SpaceID           memoryv1alpha1.SpaceID `json:"space_id"`
	Kind              string                 `json:"kind"`
	Status            RecordStatus           `json:"status"`
	CurrentRevision   uint64                 `json:"current_revision"`
	InvalidatedReason string                 `json:"invalidated_reason,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// Revision is immutable interpreted content with same-Space Evidence.
type Revision struct {
	RecordID  RecordID               `json:"record_id"`
	Revision  uint64                 `json:"revision"`
	SpaceID   memoryv1alpha1.SpaceID `json:"space_id"`
	Kind      string                 `json:"kind"`
	Text      string                 `json:"text"`
	Operation Operation              `json:"operation"`
	JobID     JobID                  `json:"job_id"`
	Evidence  []Evidence             `json:"evidence"`
	CreatedAt time.Time              `json:"created_at"`
}

// ApplyResult is durably stored with a completed job so an unknown response
// outcome can replay the exact semantic effect.
type ApplyResult struct {
	Operation         Operation `json:"operation"`
	RecordID          RecordID  `json:"record_id,omitempty"`
	Revision          uint64    `json:"revision,omitempty"`
	LexiconActivated  int       `json:"lexicon_activated,omitempty"`
	DeduplicatedRetry bool      `json:"deduplicated_retry"`
}

// Lease is opaque Worker authority for exactly one claimed Job. It must never
// be passed to a model or included in a Proposal.
type Lease struct {
	JobID JobID  `json:"job_id"`
	Token string `json:"token"`
}

// ClaimRequest asks for at most one currently available Job.
type ClaimRequest struct {
	LeaseSeconds int64 `json:"lease_seconds"`
}

// ClaimResponse carries lease authority beside, not inside, model-facing work.
type ClaimResponse struct {
	Found   bool         `json:"found"`
	Lease   *Lease       `json:"lease,omitempty"`
	Attempt int          `json:"attempt,omitempty"`
	Work    *WorkRequest `json:"work,omitempty"`
}

// ApplyRequest submits one untrusted proposal under its opaque lease.
type ApplyRequest struct {
	Lease    Lease    `json:"lease"`
	Proposal Proposal `json:"proposal"`
}

// ApplyResponse reports the canonical, durably stored application result.
type ApplyResponse struct {
	Result ApplyResult `json:"result"`
}

// FailRequest reports a stable, non-sensitive Generator failure. The appliance
// owns retry delay and the terminal-attempt ceiling.
type FailRequest struct {
	Lease     Lease  `json:"lease"`
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

// FailResponse confirms that failure disposition was durably recorded.
type FailResponse struct {
	Accepted bool `json:"accepted"`
}
