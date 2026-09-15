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
	// MaxProposalOps bounds one additive bounded-batch proposal so a Worker
	// cannot turn a single leased Job into an unbounded write.
	MaxProposalOps = 8
	// PolicyBoundedBatch is the explicit policy marker a Worker must set before
	// Memory will accept a multi-op Proposal. A Proposal without this marker
	// keeps the original one-op v1alpha1 meaning.
	PolicyBoundedBatch = "bounded_batch"
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

// SourceRole is host-authored attribution copied verbatim from evidence_sources.
// Memory never lets a model invent or widen it.
type SourceRole string

const (
	SourceRoleUserQuote              SourceRole = "user_quote"
	SourceRoleStructuredConfirmation SourceRole = "structured_confirmation"
	SourceRoleObservation            SourceRole = "observation"
	SourceRoleInference              SourceRole = "inference"
)

// SourceRef is immutable host source attribution attached to evidence. Subject
// and FactKey are host hints that may seed structured fact metadata; a model may
// copy them but must never author new values.
type SourceRef struct {
	Producer string     `json:"producer"`
	EventID  string     `json:"event_id"`
	Revision string     `json:"revision,omitempty"`
	Fragment string     `json:"fragment,omitempty"`
	Subject  string     `json:"subject,omitempty"`
	FactKey  string     `json:"fact_key,omitempty"`
	Role     SourceRole `json:"role"`
}

// ReceiptInput is the single immutable receipt assigned to a Steward Job. Host
// subject/key hints and trusted sources are attribution, not model instructions.
type ReceiptInput struct {
	ReceiptID  memoryv1alpha1.ReceiptID `json:"receipt_id"`
	Text       string                   `json:"text"`
	OccurredAt *time.Time               `json:"occurred_at,omitempty"`
	ReceivedAt time.Time                `json:"received_at"`
	Subject    string                   `json:"subject,omitempty"`
	FactKey    string                   `json:"fact_key,omitempty"`
	Sources    []SourceRef              `json:"sources,omitempty"`
}

// RecordContext is one active same-Space head the Worker may target. Space
// identity is intentionally absent. Subject and FactKey expose only host
// structured-fact hints already stored on the head.
type RecordContext struct {
	RecordID     RecordID                   `json:"record_id"`
	Revision     uint64                     `json:"revision"`
	Kind         string                     `json:"kind"`
	Text         string                     `json:"text"`
	EvidenceRefs []memoryv1alpha1.ReceiptID `json:"evidence_refs"`
	Subject      string                     `json:"subject,omitempty"`
	FactKey      string                     `json:"fact_key,omitempty"`
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

// WorkRequest is the bounded structured input used by the Memory Worker SDK to
// prepare one downstream model request. It deliberately contains no Job,
// Space, lease, bearer, or provider config.
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

// ProposalOp is one bounded mutation inside a PolicyBoundedBatch Proposal. Its
// fields mirror the single-op Proposal so shape validation stays identical. It
// deliberately carries no host attribution: a model may not author source,
// subject, or fact-key values.
type ProposalOp struct {
	Operation        Operation                  `json:"operation"`
	TargetRecordID   RecordID                   `json:"target_record_id,omitempty"`
	ExpectedRevision uint64                     `json:"expected_revision,omitempty"`
	Kind             string                     `json:"kind,omitempty"`
	Text             string                     `json:"text,omitempty"`
	EvidenceRefs     []memoryv1alpha1.ReceiptID `json:"evidence_refs,omitempty"`
}

// Proposal is an untrusted candidate mutation. Job and Space identity are
// deliberately absent because they come from the durable lease. A Proposal is
// either the original one-op shape or, when Policy is PolicyBoundedBatch and
// Ops is set, one explicit bounded batch.
type Proposal struct {
	Operation        Operation                  `json:"operation"`
	TargetRecordID   RecordID                   `json:"target_record_id,omitempty"`
	ExpectedRevision uint64                     `json:"expected_revision,omitempty"`
	Kind             string                     `json:"kind,omitempty"`
	Text             string                     `json:"text,omitempty"`
	EvidenceRefs     []memoryv1alpha1.ReceiptID `json:"evidence_refs,omitempty"`
	LexiconTerms     []string                   `json:"lexicon_terms,omitempty"`
	Policy           string                     `json:"policy,omitempty"`
	Ops              []ProposalOp               `json:"ops,omitempty"`
}

// IsBatch reports whether this Proposal uses the additive bounded-batch policy.
func (p Proposal) IsBatch() bool { return len(p.Ops) > 0 }

// BatchOps returns the ordered ops of a batch Proposal, or the single op as a
// one-element batch. It lets Apply treat both shapes uniformly.
func (p Proposal) BatchOps() []ProposalOp {
	if p.IsBatch() {
		return p.Ops
	}
	return []ProposalOp{{
		Operation: p.Operation, TargetRecordID: p.TargetRecordID, ExpectedRevision: p.ExpectedRevision,
		Kind: p.Kind, Text: p.Text, EvidenceRefs: p.EvidenceRefs,
	}}
}

// ValidateShape rejects unsupported operations and fields before canonical
// state, evidence, or authorization data are read.
func (p Proposal) ValidateShape() error {
	if p.IsBatch() {
		return p.validateBatchShape()
	}
	if p.Policy != "" {
		return fmt.Errorf("proposal policy %q requires a bounded ops batch", p.Policy)
	}
	if err := validateProposalMutation(p.Operation, p.TargetRecordID, p.ExpectedRevision, p.Kind, p.Text, p.EvidenceRefs); err != nil {
		return err
	}
	return validateLexiconTerms(p.LexiconTerms)
}

func (p Proposal) validateBatchShape() error {
	if p.Policy != PolicyBoundedBatch {
		return fmt.Errorf("multi-op proposal requires policy %q", PolicyBoundedBatch)
	}
	if p.Operation != "" || p.TargetRecordID != "" || p.ExpectedRevision != 0 || p.Kind != "" ||
		p.Text != "" || len(p.EvidenceRefs) != 0 {
		return fmt.Errorf("bounded batch cannot also carry single-op fields")
	}
	if len(p.Ops) == 0 || len(p.Ops) > MaxProposalOps {
		return fmt.Errorf("bounded batch op count must be 1..%d", MaxProposalOps)
	}
	targets := make(map[RecordID]struct{}, len(p.Ops))
	for index, op := range p.Ops {
		if err := validateProposalMutation(op.Operation, op.TargetRecordID, op.ExpectedRevision, op.Kind, op.Text, op.EvidenceRefs); err != nil {
			return fmt.Errorf("op %d: %w", index, err)
		}
		if op.TargetRecordID != "" {
			if _, exists := targets[op.TargetRecordID]; exists {
				return fmt.Errorf("op %d: bounded batch may target each Record once", index)
			}
			targets[op.TargetRecordID] = struct{}{}
		}
	}
	return validateLexiconTerms(p.LexiconTerms)
}

func validateProposalMutation(
	operation Operation,
	targetRecordID RecordID,
	expectedRevision uint64,
	kind string,
	text string,
	evidenceRefs []memoryv1alpha1.ReceiptID,
) error {
	switch operation {
	case OperationIgnore:
		if targetRecordID != "" || expectedRevision != 0 || kind != "" || text != "" || len(evidenceRefs) != 0 {
			return fmt.Errorf("IGNORE cannot contain mutation fields")
		}
	case OperationAdd:
		if targetRecordID != "" || expectedRevision != 0 {
			return fmt.Errorf("ADD cannot target an existing Record")
		}
	case OperationMerge, OperationSupersede:
		if targetRecordID == "" || expectedRevision == 0 {
			return fmt.Errorf("%s requires a target Record and expected revision", operation)
		}
	default:
		return fmt.Errorf("unsupported proposal operation %q", operation)
	}
	if operation == OperationIgnore {
		return nil
	}
	if !utf8.ValidString(kind) || kind == "" || strings.TrimSpace(kind) != kind || len(kind) > MaxRecordKindBytes || strings.ContainsAny(kind, "\r\n\t") {
		return fmt.Errorf("record kind must be bounded non-whitespace UTF-8")
	}
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || len(text) > MaxRecordTextBytes {
		return fmt.Errorf("record text must be 1..%d UTF-8 bytes", MaxRecordTextBytes)
	}
	if len(evidenceRefs) == 0 || len(evidenceRefs) > MaxProposalEvidence {
		return fmt.Errorf("proposal evidence count must be 1..%d", MaxProposalEvidence)
	}
	seen := make(map[memoryv1alpha1.ReceiptID]struct{}, len(evidenceRefs))
	for _, receiptID := range evidenceRefs {
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

func validateLexiconTerms(terms []string) error {
	if len(terms) > MaxLexiconTerms {
		return fmt.Errorf("lexicon term count must be 0..%d", MaxLexiconTerms)
	}
	seenTerms := make(map[string]struct{}, len(terms))
	for _, term := range terms {
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
	RecordID          RecordID                `json:"record_id"`
	SpaceID           memoryv1alpha1.SpaceID  `json:"space_id"`
	Labels            memoryv1alpha1.LabelSet `json:"labels,omitempty"`
	Kind              string                  `json:"kind"`
	Status            RecordStatus            `json:"status"`
	CurrentRevision   uint64                  `json:"current_revision"`
	InvalidatedReason string                  `json:"invalidated_reason,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
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

// ApplyResultOp is one applied bounded-batch op. A single-op Proposal leaves
// Ops empty and reports only the top-level result fields.
type ApplyResultOp struct {
	Operation Operation `json:"operation"`
	RecordID  RecordID  `json:"record_id,omitempty"`
	Revision  uint64    `json:"revision,omitempty"`
}

// ApplyResult is durably stored with a completed job so an unknown response
// outcome can replay the exact semantic effect.
type ApplyResult struct {
	Operation         Operation       `json:"operation"`
	RecordID          RecordID        `json:"record_id,omitempty"`
	Revision          uint64          `json:"revision,omitempty"`
	Ops               []ApplyResultOp `json:"ops,omitempty"`
	LexiconActivated  int             `json:"lexicon_activated,omitempty"`
	DeduplicatedRetry bool            `json:"deduplicated_retry"`
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

// FailRequest reports a stable, non-sensitive model-generation failure. The appliance
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
