// Package appliance implements the standalone durable Memory appliance.
package appliance

import (
	"errors"
	"io"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const (
	DatabaseFilename           = "memory.db"
	OwnerLockFilename          = "memoryd.lock"
	ManagementCredentialFile   = "management.token"
	SocketFilename             = v1alpha1.LocalSocketFilename
	CurrentSchemaVersion       = 2
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

type Realm = managementv1alpha1.Realm
type Identity = managementv1alpha1.Identity
type Space = managementv1alpha1.Space
type ViewDefinition = managementv1alpha1.ViewDefinition
type Grant = managementv1alpha1.Grant
type BootstrapRequest = managementv1alpha1.BootstrapRequest
type BootstrapResponse = managementv1alpha1.BootstrapResponse

type IssuerAuthorization = managementv1alpha1.IssuerAuthorization

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

type Inspection = managementv1alpha1.Inspection
