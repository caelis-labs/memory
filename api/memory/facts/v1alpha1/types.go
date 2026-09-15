// Package v1alpha1 owns the current-fact, trusted-source and bounded-background
// contracts. It does not change memory.v1alpha1 evidence Recall semantics.
package v1alpha1

import (
	"context"
	"time"

	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const ProtocolVersion = "memory.facts.v1alpha1"
const MaxMutations = 8

// Source is asserted by the trusted host, never parsed from model output.
// Identity is (Space, exact LabelSet, Producer, EventID, Revision, Fragment).
// Subject and FactKey are attribution/hints, not permission selectors.
type Source struct {
	Producer string `json:"producer"`
	EventID  string `json:"event_id"`
	Revision string `json:"revision"`
	Fragment string `json:"fragment"`
	Subject  string `json:"subject"`
	FactKey  string `json:"fact_key,omitempty"`
	Role     Role   `json:"role"`
}
type Role string

const (
	RoleUserQuote    Role = "user_quote"
	RoleConfirmation Role = "structured_confirmation"
	RoleObservation  Role = "observation"
	RoleInference    Role = "inference"
)

type Adoption string

const (
	AdoptionPending   Adoption = "pending"
	AdoptionConfirmed Adoption = "confirmed"
	AdoptionDenied    Adoption = "denied"
)

type Transition string

const (
	TransitionEstablish Transition = "establish"
	TransitionChange    Transition = "change"
	TransitionException Transition = "exception"
	TransitionCorrect   Transition = "correct"
	TransitionConfirm   Transition = "confirm"
	TransitionDeny      Transition = "deny"
)

// Condition is exact host-context equality. Missing context never matches.
// No free-text conditions, expressions or model evaluation are supported.
type Condition struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Metadata belongs to one immutable semantic Revision. Unknown onset stays nil;
// it is usable now but not evidence that the fact held at an arbitrary past time.
// ValidUntil is exclusive. Change requires an explicit ValidFrom. Exception
// requires both endpoints and never changes the underlying long-term revision.
type Metadata struct {
	Subject         string      `json:"subject"`
	Key             string      `json:"key,omitempty"`
	Adoption        Adoption    `json:"adoption"`
	ValidFrom       *time.Time  `json:"valid_from,omitempty"`
	ValidUntil      *time.Time  `json:"valid_until,omitempty"`
	Conditions      []Condition `json:"conditions,omitempty"`
	Transition      Transition  `json:"transition"`
	RelatedRecordID string      `json:"related_record_id,omitempty"`
}

// Mutation is an explicit host/user edit, NOT a Steward/model proposal. Change,
// correct, confirm and deny require an exact target revision. Exception creates
// a separate Record linked to the target; all mutations apply atomically.
type Mutation struct {
	Transition       Transition  `json:"transition"`
	TargetRecordID   string      `json:"target_record_id,omitempty"`
	ExpectedRevision uint64      `json:"expected_revision,omitempty"`
	Subject          string      `json:"subject"`
	Key              string      `json:"key,omitempty"`
	Text             string      `json:"text"`
	ValidFrom        *time.Time  `json:"valid_from,omitempty"`
	ValidUntil       *time.Time  `json:"valid_until,omitempty"`
	Conditions       []Condition `json:"conditions,omitempty"`
}
type SubmitEvidenceRequest struct {
	Source         Source     `json:"source"`
	Text           string     `json:"text"`
	OccurredAt     *time.Time `json:"occurred_at,omitempty"`
	IdempotencyKey string     `json:"idempotency_key"`
	Mutations      []Mutation `json:"mutations,omitempty"`
}
type OrganizationState string

const (
	OrganizationDisabled   OrganizationState = "disabled"
	OrganizationPending    OrganizationState = "pending"
	OrganizationProcessing OrganizationState = "processing"
	OrganizationApplied    OrganizationState = "applied"
	OrganizationFailed     OrganizationState = "failed"
)

type SubmitEvidenceResponse struct {
	Accepted         bool                    `json:"accepted"`
	ReceiptID        memory.ReceiptID        `json:"receipt_id,omitempty"`
	ConsistencyToken memory.ConsistencyToken `json:"consistency_token,omitempty"`
	Deduplicated     bool                    `json:"deduplicated"`
	RejectionReason  string                  `json:"rejection_reason,omitempty"`
	Organization     OrganizationState       `json:"organization"`
	Facts            []Fact                  `json:"facts,omitempty"`
}

// Scope is selected by the owner for policy, never by model arguments.
type Scope struct {
	SpaceID memory.SpaceID  `json:"space_id"`
	Labels  memory.LabelSet `json:"labels,omitempty"`
}
type IngestionPolicy struct {
	Scope    Scope  `json:"scope"`
	Producer string `json:"producer"`
	Subject  string `json:"subject"`
	Deny     bool   `json:"deny"`
	Reason   string `json:"reason,omitempty"`
}

// EvidenceService is a trusted embedding plane. Do not give it to model tools
// or read-only workers. Submit additionally requires Remember capability;
// policy changes require possession of this owner-only facade.
type EvidenceService interface {
	SubmitEvidence(context.Context, memory.CallAuthorization, SubmitEvidenceRequest) (SubmitEvidenceResponse, error)
	SetIngestionPolicy(context.Context, IngestionPolicy) error
}
type Evidence struct {
	ReceiptID memory.ReceiptID `json:"receipt_id"`
	Source    *Source          `json:"source,omitempty"`
}
type Fact struct {
	RecordID string         `json:"record_id"`
	Revision uint64         `json:"revision"`
	SpaceID  memory.SpaceID `json:"space_id"`
	Text     string         `json:"text"`
	Metadata Metadata       `json:"metadata"`
	Evidence []Evidence     `json:"evidence"`
	// HistoricalState distinguishes a past habit from a corrected/denied error.
	HistoricalState string `json:"historical_state,omitempty"`
}
type Budget struct {
	MaxFacts int `json:"max_facts"`
	MaxBytes int `json:"max_bytes"`
}
type ReadRequest struct {
	Subject string `json:"subject"`
	Key     string `json:"key,omitempty"`
	Query   string `json:"query,omitempty"`
	// AsOf selects historical adoption, not current personalization. Nil = now.
	AsOf    *time.Time        `json:"as_of,omitempty"`
	Context map[string]string `json:"context,omitempty"`
	Budget  Budget            `json:"budget"`
}

// Cursor is bound to an authorized read scope and storage generation. Changes
// are retained in this release. A generation/scope mismatch requires full
// refresh; callers must never reuse context across such a reset.
type Cursor struct {
	Generation string `json:"generation"`
	Scope      string `json:"scope"`
	Sequence   uint64 `json:"sequence"`
}
type ReadResponse struct {
	Facts      []Fact `json:"facts"`
	Background string `json:"background"`
	Cursor     Cursor `json:"cursor"`
	// RefreshAt is the next time boundary even if no mutation/change occurs.
	RefreshAt *time.Time `json:"refresh_at,omitempty"`
	Truncated bool       `json:"truncated"`
	BytesUsed int        `json:"bytes_used"`
	// NextRevision is the history continuation (last returned revision).
	NextRevision uint64 `json:"next_revision,omitempty"`
}
type HistoryRequest struct {
	Subject       string `json:"subject"`
	RecordID      string `json:"record_id"`
	AfterRevision uint64 `json:"after_revision,omitempty"`
	Budget        Budget `json:"budget"`
}
type Change struct {
	Sequence    uint64           `json:"sequence"`
	Kind        string           `json:"kind"`
	ReceiptID   memory.ReceiptID `json:"receipt_id,omitempty"`
	RecordID    string           `json:"record_id,omitempty"`
	EffectiveAt time.Time        `json:"effective_at"`
}
type ChangesRequest struct {
	After Cursor `json:"after"`
	Limit int    `json:"limit"`
}
type ChangesResponse struct {
	Changes       []Change `json:"changes"`
	Cursor        Cursor   `json:"cursor"`
	ResetRequired bool     `json:"reset_required"`
	HasMore       bool     `json:"has_more"`
}

// Reader uses Recall capability; possession does not grant writes/governance.
// Every candidate stream is selected only after Space and LabelSet authorization.
type Reader interface {
	ReadFacts(context.Context, memory.CallAuthorization, ReadRequest) (ReadResponse, error)
	FactHistory(context.Context, memory.CallAuthorization, HistoryRequest) (ReadResponse, error)
	Changes(context.Context, memory.CallAuthorization, ChangesRequest) (ChangesResponse, error)
}
