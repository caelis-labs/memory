package v1alpha1

import "time"

const (
	ProtocolVersion          = "memory.v1alpha1"
	MinRecallProjectionBytes = len(`{"fragments":[]}`)
)

type (
	RealmID          string
	IdentityID       string
	SpaceID          string
	ViewID           string
	GrantID          string
	ReceiptID        string
	CapabilityToken  string
	ConsistencyToken string
)

// Audience is the disclosure class of the Runtime receiving recalled data.
type Audience string

const (
	AudiencePrivate Audience = "private"
	AudienceShared  Audience = "shared"
)

// SpaceClass is the access class of a Memory Space in the Core Profile.
type SpaceClass string

const (
	SpaceClassPrivate SpaceClass = "private"
	SpaceClassShared  SpaceClass = "shared"
)

// Operation is a capability-controlled data-plane operation.
type Operation string

const (
	OperationRemember      Operation = "remember"
	OperationRecall        Operation = "recall"
	OperationReceiptStatus Operation = "receipt_status"
)

// SourceContext is bounded, untrusted audit metadata. None of its fields grant
// authority or select Memory domain objects.
type SourceContext struct {
	ActorRef        string            `json:"actor_ref,omitempty"`
	SessionRef      string            `json:"session_ref,omitempty"`
	WorkspaceRef    string            `json:"workspace_ref,omitempty"`
	TaskRef         string            `json:"task_ref,omitempty"`
	ToolCallRef     string            `json:"tool_call_ref,omitempty"`
	SourceType      string            `json:"source_type,omitempty"`
	ExtensionLabels map[string]string `json:"extension_labels,omitempty"`
}

// RememberRequest contains effect-bearing Remember input. The capability and
// Runtime binding are supplied out of band through CallAuthorization.
type RememberRequest struct {
	Text           string        `json:"text"`
	SourceContext  SourceContext `json:"source_context,omitempty"`
	OccurredAt     *time.Time    `json:"occurred_at,omitempty"`
	IdempotencyKey string        `json:"idempotency_key"`
}

// ProcessingState reports optional downstream interpretation independently of
// the immutable accepted receipt payload.
type ProcessingState string

const (
	ProcessingStateAccepted   ProcessingState = "accepted"
	ProcessingStateProcessing ProcessingState = "processing"
	ProcessingStateOrganized  ProcessingState = "organized"
	ProcessingStateFailed     ProcessingState = "failed"
)

// RememberResponse confirms an accepted receipt.
type RememberResponse struct {
	Accepted          bool             `json:"accepted"`
	ReceiptID         ReceiptID        `json:"receipt_id"`
	ConsistencyToken  ConsistencyToken `json:"consistency_token"`
	DeduplicatedRetry bool             `json:"deduplicated_retry"`
	ProcessingState   ProcessingState  `json:"processing_state"`
}

// GetReceiptStatusRequest identifies one receipt. Authorization still derives
// from the call capability, not this reference.
type GetReceiptStatusRequest struct {
	ReceiptID ReceiptID `json:"receipt_id"`
}

// ReceiptStatus describes mutable processing without changing receipt evidence.
type ReceiptStatus struct {
	ReceiptID          ReceiptID       `json:"receipt_id"`
	State              ProcessingState `json:"state"`
	AcceptedAt         time.Time       `json:"accepted_at"`
	LastAttemptAt      *time.Time      `json:"last_attempt_at,omitempty"`
	TerminalErrorCode  ErrorCode       `json:"terminal_error_code,omitempty"`
	SemanticGeneration string          `json:"semantic_generation,omitempty"`
}

// RecallBudget is selected by the host and hidden from model arguments.
type RecallBudget struct {
	MaxFragments int `json:"max_fragments"`
	MaxBytes     int `json:"max_bytes"`
	DeadlineMS   int `json:"deadline_ms"`
}

// RecallRequest contains a query and host-selected hidden controls.
type RecallRequest struct {
	Query               string           `json:"query"`
	SourceContext       SourceContext    `json:"source_context,omitempty"`
	MinConsistencyToken ConsistencyToken `json:"min_consistency_token,omitempty"`
	Budget              RecallBudget     `json:"budget"`
}

// RecallFragment is an extractive result with evidence references.
type RecallFragment struct {
	FragmentID   string      `json:"fragment_id"`
	Text         string      `json:"text"`
	EvidenceRefs []ReceiptID `json:"evidence_refs"`
	RecordRefs   []string    `json:"record_refs,omitempty"`
	SpaceClass   SpaceClass  `json:"space_class"`
}

// RecallResponse distinguishes a successful empty result from a service error.
type RecallResponse struct {
	Fragments        []RecallFragment `json:"fragments"`
	ConsistencyToken ConsistencyToken `json:"consistency_token,omitempty"`
	Degraded         bool             `json:"degraded"`
	Truncated        bool             `json:"truncated"`
}

// CallAuthorization is transport authentication context. It is never part of
// the model-visible or persisted request body.
type CallAuthorization struct {
	Capability CapabilityToken
	ActorRef   string
	Audience   Audience
}
