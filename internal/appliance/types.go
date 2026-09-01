// Package appliance implements the standalone durable Memory appliance.
package appliance

import (
	"errors"
	"io"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const (
	DatabaseFilename           = "memory.db"
	OwnerLockFilename          = "memoryd.lock"
	ManagementCredentialFile   = "management.token"
	SocketFilename             = "memoryd.sock"
	CurrentSchemaVersion       = 1
	defaultSQLiteBusyTimeoutMS = 2_000
)

// Options controls local durable storage. Faults are test-only failure points
// and must remain nil in production composition.
type Options struct {
	DataDir       string
	Clock         func() time.Time
	Random        io.Reader
	BusyTimeoutMS int
	Faults        Faults
	CandidateRead func(v1alpha1.SpaceID)
}

// Faults injects failures around the Remember commit boundary.
type Faults struct {
	BeforeRememberCommit func() error
	AfterRememberCommit  func() error
}

// ErrOwnerLocked means another memoryd owns the data directory.
var ErrOwnerLocked = errors.New("memory data directory is already owned")

// ErrCapabilityIssueInvalid classifies a malformed issuer-plane request.
var ErrCapabilityIssueInvalid = errors.New("capability issue request is invalid")

// ErrCapabilityIssueUnauthorized classifies a principal, Grant, actor,
// audience, operation, or View authorization mismatch without identifying it.
var ErrCapabilityIssueUnauthorized = errors.New("capability issuer is unauthorized")

// Realm is one appliance administrative root.
type Realm struct {
	ID v1alpha1.RealmID `json:"id"`
}

// Identity is stable cognitive continuity within a Realm.
type Identity struct {
	ID      v1alpha1.IdentityID `json:"id"`
	RealmID v1alpha1.RealmID    `json:"realm_id"`
}

// Space is a durable storage and authorization boundary.
type Space struct {
	ID         v1alpha1.SpaceID    `json:"id"`
	RealmID    v1alpha1.RealmID    `json:"realm_id"`
	IdentityID v1alpha1.IdentityID `json:"identity_id,omitempty"`
	Class      v1alpha1.SpaceClass `json:"class"`
}

// ViewDefinition selects readable and writable Spaces independently from a
// principal delegation.
type ViewDefinition struct {
	ID                 v1alpha1.ViewID     `json:"id"`
	RealmID            v1alpha1.RealmID    `json:"realm_id"`
	ReadSpaceIDs       []v1alpha1.SpaceID  `json:"read_space_ids"`
	WriteSpaceID       v1alpha1.SpaceID    `json:"write_space_id,omitempty"`
	MaxDisclosureClass v1alpha1.SpaceClass `json:"max_disclosure_class"`
	RecallPolicyRef    string              `json:"recall_policy_ref,omitempty"`
	Version            uint64              `json:"version"`
}

// Grant delegates one View to a principal and Runtime actor.
type Grant struct {
	ID                v1alpha1.GrantID     `json:"id"`
	PrincipalRef      string               `json:"principal_ref"`
	ActorRef          string               `json:"actor_ref"`
	ViewRef           v1alpha1.ViewID      `json:"view_ref"`
	AllowedOperations []v1alpha1.Operation `json:"allowed_operations"`
	AllowedAudiences  []v1alpha1.Audience  `json:"allowed_audiences"`
	ExpiresAt         time.Time            `json:"expires_at"`
	Revoked           bool                 `json:"revoked,omitempty"`
	Version           uint64               `json:"version"`
}

// BootstrapRequest creates a topology atomically. IssuerPrincipals names the
// principals that receive newly generated local issuer credentials.
type BootstrapRequest struct {
	Realms           []Realm          `json:"realms"`
	Identities       []Identity       `json:"identities"`
	Spaces           []Space          `json:"spaces"`
	Views            []ViewDefinition `json:"views"`
	Grants           []Grant          `json:"grants"`
	IssuerPrincipals []string         `json:"issuer_principals"`
}

// BootstrapResponse returns issuer credentials once. The caller must store
// them in an owner-only file; the appliance persists only their digests.
type BootstrapResponse struct {
	IssuerCredentials map[string]string `json:"issuer_credentials"`
}

// IssuerAuthorization authenticates the principal redeeming a Grant.
type IssuerAuthorization struct {
	PrincipalRef string `json:"principal_ref"`
	Credential   string `json:"credential"`
}

// IssueCapabilityRequest asks the local issuer for bounded Runtime authority.
type IssueCapabilityRequest struct {
	Authorization IssuerAuthorization  `json:"authorization"`
	GrantRef      v1alpha1.GrantID     `json:"grant_ref"`
	ActorRef      string               `json:"actor_ref"`
	Audience      v1alpha1.Audience    `json:"audience"`
	Operations    []v1alpha1.Operation `json:"operations"`
	TTL           time.Duration        `json:"-"`
	TTLSeconds    int64                `json:"ttl_seconds"`
}

// RuntimeCapability is the only authority returned to a Runtime.
type RuntimeCapability struct {
	Token     v1alpha1.CapabilityToken `json:"token"`
	ExpiresAt time.Time                `json:"expires_at"`
}

// Inspection is a secret-free summary for local operation and acceptance.
type Inspection struct {
	SchemaVersion int              `json:"schema_version"`
	Generation    string           `json:"generation"`
	Counts        map[string]int64 `json:"counts"`
	Spaces        []Space          `json:"spaces"`
}
