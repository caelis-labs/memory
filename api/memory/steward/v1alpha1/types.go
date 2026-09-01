// Package v1alpha1 defines the versioned proposal and semantic-record contract
// between the Memory Appliance and a replaceable Steward provider. Provider
// output is untrusted input; only the appliance may apply it.
package v1alpha1

import (
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
