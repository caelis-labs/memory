// Package v1alpha1 defines the versioned proposal and semantic-record contract
// between the Memory Appliance and a replaceable Steward provider. Provider
// output is untrusted input; only the appliance may apply it.
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
)

// RecordID identifies appliance-owned interpreted continuity.
type RecordID string

// JobID identifies one durable receipt-organization effect.
type JobID string

// ProfileID identifies a versioned model and prompt-policy profile.
type ProfileID string

// ProfileSpec is immutable model and prompt-policy configuration stored by the
// appliance. Provider credentials remain out-of-band process configuration.
type ProfileSpec struct {
	ProfileID         ProfileID `json:"profile_id"`
	Version           uint64    `json:"version"`
	ProviderRef       string    `json:"provider_ref"`
	Model             string    `json:"model"`
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
	if !boundedReference(p.ProviderRef, 256) || !boundedReference(p.Model, 256) {
		return fmt.Errorf("provider and model references must be bounded")
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

// RecordContext is one active same-Space head the provider may target. Space
// identity is intentionally absent.
type RecordContext struct {
	RecordID     RecordID                   `json:"record_id"`
	Revision     uint64                     `json:"revision"`
	Kind         string                     `json:"kind"`
	Text         string                     `json:"text"`
	EvidenceRefs []memoryv1alpha1.ReceiptID `json:"evidence_refs"`
}

// WorkRequest is the bounded structured provider input. SystemPrompt is data
// for the dedicated provider adapter, not text concatenated with a receipt.
type WorkRequest struct {
	Protocol string          `json:"protocol"`
	Profile  ProfileSpec     `json:"profile"`
	Receipt  ReceiptInput    `json:"receipt"`
	Records  []RecordContext `json:"records"`
}

// EncodedSize returns the exact JSON request size used for profile input
// budgeting.
func (r WorkRequest) EncodedSize() (int, error) {
	value, err := json.Marshal(r)
	return len(value), err
}

// ProviderResponse is the strict response envelope for a dedicated Steward
// model endpoint.
type ProviderResponse struct {
	Protocol string   `json:"protocol"`
	Proposal Proposal `json:"proposal"`
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
}

// ValidateShape rejects unsupported operations and fields before canonical
// state, evidence, or authorization data are read.
func (p Proposal) ValidateShape() error {
	switch p.Operation {
	case OperationIgnore:
		if p.TargetRecordID != "" || p.ExpectedRevision != 0 || p.Kind != "" || p.Text != "" || len(p.EvidenceRefs) != 0 {
			return fmt.Errorf("IGNORE cannot contain mutation fields")
		}
		return nil
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
	DeduplicatedRetry bool      `json:"deduplicated_retry"`
}
