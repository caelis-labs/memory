// Package v1alpha1 defines the versioned owner-management contract for a
// Memory Appliance. Management authorization is an out-of-band bearer and is
// never interchangeable with issuer or Runtime authority.
package v1alpha1

import (
	"time"

	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const ProtocolVersion = "memory.management.v1alpha1"

// Realm is one appliance administrative root.
type Realm struct {
	ID memoryv1alpha1.RealmID `json:"id"`
}

// Identity is stable cognitive continuity within a Realm.
type Identity struct {
	ID      memoryv1alpha1.IdentityID `json:"id"`
	RealmID memoryv1alpha1.RealmID    `json:"realm_id"`
}

// Space is a durable storage and authorization boundary.
type Space struct {
	ID         memoryv1alpha1.SpaceID    `json:"id"`
	RealmID    memoryv1alpha1.RealmID    `json:"realm_id"`
	IdentityID memoryv1alpha1.IdentityID `json:"identity_id,omitempty"`
	Class      memoryv1alpha1.SpaceClass `json:"class"`
}

// ViewDefinition selects readable and writable Spaces independently from a
// principal delegation.
type ViewDefinition struct {
	ID                 memoryv1alpha1.ViewID     `json:"id"`
	RealmID            memoryv1alpha1.RealmID    `json:"realm_id"`
	ReadSpaceIDs       []memoryv1alpha1.SpaceID  `json:"read_space_ids"`
	WriteSpaceID       memoryv1alpha1.SpaceID    `json:"write_space_id,omitempty"`
	MaxDisclosureClass memoryv1alpha1.SpaceClass `json:"max_disclosure_class"`
	RecallPolicyRef    string                    `json:"recall_policy_ref,omitempty"`
	Version            uint64                    `json:"version"`
}

// Grant delegates one View to a principal and Runtime actor.
type Grant struct {
	ID                memoryv1alpha1.GrantID     `json:"id"`
	PrincipalRef      string                     `json:"principal_ref"`
	ActorRef          string                     `json:"actor_ref"`
	ViewRef           memoryv1alpha1.ViewID      `json:"view_ref"`
	AllowedOperations []memoryv1alpha1.Operation `json:"allowed_operations"`
	AllowedAudiences  []memoryv1alpha1.Audience  `json:"allowed_audiences"`
	ExpiresAt         time.Time                  `json:"expires_at"`
	Revoked           bool                       `json:"revoked,omitempty"`
	Version           uint64                     `json:"version"`
}

// BootstrapRequest creates a complete topology atomically. IssuerPrincipals
// names the principals that receive newly generated local issuer credentials.
type BootstrapRequest struct {
	Realms           []Realm          `json:"realms"`
	Identities       []Identity       `json:"identities"`
	Spaces           []Space          `json:"spaces"`
	Views            []ViewDefinition `json:"views"`
	Grants           []Grant          `json:"grants"`
	IssuerPrincipals []string         `json:"issuer_principals"`
}

// BootstrapResponse returns issuer credentials once. The caller stores them
// outside the appliance database in an owner-only file.
type BootstrapResponse struct {
	IssuerCredentials map[string]string `json:"issuer_credentials"`
}

// Inspection is a secret-free summary for operation and acceptance.
type Inspection struct {
	ProtocolVersion   string                `json:"protocol_version"`
	SchemaVersion     int                   `json:"schema_version"`
	Generation        string                `json:"generation"`
	RestorePending    bool                  `json:"restore_pending"`
	RollbackAvailable bool                  `json:"rollback_available"`
	Counts            map[string]int64      `json:"counts"`
	Spaces            []Space               `json:"spaces"`
	Storage           StorageDiagnostics    `json:"storage"`
	Receipts          ReceiptDiagnostics    `json:"receipts"`
	Projection        ProjectionDiagnostics `json:"projection"`
	Capabilities      CapabilityDiagnostics `json:"capabilities"`
	Steward           StewardDiagnostics    `json:"steward"`
	Lexicon           LexiconDiagnostics    `json:"lexicon"`
}

// LexiconDiagnostics reports private adaptive-index health without exposing
// learned terms or receipt text.
type LexiconDiagnostics struct {
	AlgorithmVersion string `json:"algorithm_version"`
	Spaces           int64  `json:"spaces"`
	GenerationSum    int64  `json:"generation_sum"`
	CandidateTerms   int64  `json:"candidate_terms"`
	ActiveTerms      int64  `json:"active_terms"`
	RetiredTerms     int64  `json:"retired_terms"`
	EvidenceLinks    int64  `json:"evidence_links"`
	PendingRebuilds  int64  `json:"pending_rebuilds"`
}

// ReceiptState describes whether canonical receipt text is active, shadowed by
// an authorized correction, or physically removed from the appliance.
type ReceiptState string

const (
	ReceiptStateActive    ReceiptState = "active"
	ReceiptStateCorrected ReceiptState = "corrected"
	ReceiptStateDeleted   ReceiptState = "deleted"
)

// Receipt contains management-visible evidence and audit metadata. It is
// intentionally never returned by the Agent data plane.
type Receipt struct {
	ReceiptID       memoryv1alpha1.ReceiptID       `json:"receipt_id"`
	SpaceID         memoryv1alpha1.SpaceID         `json:"space_id"`
	Text            string                         `json:"text"`
	SourceContext   memoryv1alpha1.SourceContext   `json:"source_context"`
	OccurredAt      *time.Time                     `json:"occurred_at,omitempty"`
	ReceivedAt      time.Time                      `json:"received_at"`
	CommitSequence  int64                          `json:"commit_sequence"`
	ProcessingState memoryv1alpha1.ProcessingState `json:"processing_state"`
	CorrectedBy     memoryv1alpha1.ReceiptID       `json:"corrected_by,omitempty"`
	CorrectionOf    memoryv1alpha1.ReceiptID       `json:"correction_of,omitempty"`
}

// Tombstone is content-free durable evidence that a receipt was removed. The
// retained effect digest prevents an old Remember retry from resurrecting it.
type Tombstone struct {
	TombstoneID string                   `json:"tombstone_id"`
	ReceiptID   memoryv1alpha1.ReceiptID `json:"receipt_id"`
	SpaceID     memoryv1alpha1.SpaceID   `json:"space_id"`
	DeletedAt   time.Time                `json:"deleted_at"`
	Reason      string                   `json:"reason"`
}

// SearchReceiptsRequest performs owner-authorized lexical search. A Space may
// be supplied to narrow the query; omission searches every Space.
type SearchReceiptsRequest struct {
	Query            string                 `json:"query"`
	SpaceID          memoryv1alpha1.SpaceID `json:"space_id,omitempty"`
	Limit            int                    `json:"limit"`
	IncludeCorrected bool                   `json:"include_corrected,omitempty"`
}

type SearchReceiptsResponse struct {
	Receipts  []Receipt `json:"receipts"`
	Truncated bool      `json:"truncated"`
}

type TraceReceiptRequest struct {
	ReceiptID memoryv1alpha1.ReceiptID `json:"receipt_id"`
}

// TraceReceiptResponse returns either management-visible receipt evidence or a
// content-free tombstone, plus the effective governance state.
type TraceReceiptResponse struct {
	State     ReceiptState `json:"state"`
	Receipt   *Receipt     `json:"receipt,omitempty"`
	Tombstone *Tombstone   `json:"tombstone,omitempty"`
}

// CorrectReceiptRequest appends replacement evidence in the same Space and
// shadows the original from baseline Recall. It never updates receipt payload.
type CorrectReceiptRequest struct {
	ReceiptID       memoryv1alpha1.ReceiptID `json:"receipt_id"`
	ReplacementText string                   `json:"replacement_text"`
	Reason          string                   `json:"reason"`
	IdempotencyKey  string                   `json:"idempotency_key"`
}

type CorrectReceiptResponse struct {
	OriginalReceiptID    memoryv1alpha1.ReceiptID        `json:"original_receipt_id"`
	ReplacementReceiptID memoryv1alpha1.ReceiptID        `json:"replacement_receipt_id"`
	ConsistencyToken     memoryv1alpha1.ConsistencyToken `json:"consistency_token"`
	DeduplicatedRetry    bool                            `json:"deduplicated_retry"`
}

// DeleteReceiptRequest physically removes appliance-owned receipt content and
// leaves a content-free tombstone. Host Session copies are a separate scope.
type DeleteReceiptRequest struct {
	ReceiptID      memoryv1alpha1.ReceiptID `json:"receipt_id"`
	Reason         string                   `json:"reason"`
	IdempotencyKey string                   `json:"idempotency_key"`
}

type DeleteReceiptResponse struct {
	Deleted             bool                     `json:"deleted"`
	ReceiptID           memoryv1alpha1.ReceiptID `json:"receipt_id"`
	TombstoneID         string                   `json:"tombstone_id"`
	DeduplicatedRetry   bool                     `json:"deduplicated_retry"`
	SessionCopyBoundary string                   `json:"session_copy_boundary"`
}

type RebuildFTSResponse struct {
	Rebuilt bool `json:"rebuilt"`
}

type CommitRestoreResponse struct {
	Committed bool `json:"committed"`
}

type RevokeGrantRequest struct {
	GrantID memoryv1alpha1.GrantID `json:"grant_id"`
}

type RevokeGrantResponse struct {
	Revoked bool `json:"revoked"`
}

type RotateIssuerRequest struct {
	PrincipalRef string `json:"principal_ref"`
}

type RotateManagementCredentialResponse struct {
	Rotated bool `json:"rotated"`
}

// IssuerAuthorization contains a newly rotated issuer bearer and must be
// written to an owner-only secret output. It is never Runtime authority.
type IssuerAuthorization struct {
	PrincipalRef string `json:"principal_ref"`
	Credential   string `json:"credential"`
}
