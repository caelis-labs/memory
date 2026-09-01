package reference

import (
	"fmt"
	"sync/atomic"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// Realm is the minimum reference configuration for an administrative root.
type Realm struct {
	ID v1alpha1.RealmID
}

// Identity is stable cognitive continuity within a Realm.
type Identity struct {
	ID      v1alpha1.IdentityID
	RealmID v1alpha1.RealmID
}

// Space is a reference storage and authorization boundary.
type Space struct {
	ID         v1alpha1.SpaceID
	RealmID    v1alpha1.RealmID
	IdentityID v1alpha1.IdentityID
	Class      v1alpha1.SpaceClass
}

// ViewDefinition is reference bootstrap state, not a v1alpha1 wire type.
type ViewDefinition struct {
	ID                 v1alpha1.ViewID
	RealmID            v1alpha1.RealmID
	ReadSpaceIDs       []v1alpha1.SpaceID
	WriteSpaceID       v1alpha1.SpaceID
	MaxDisclosureClass v1alpha1.SpaceClass
	RecallPolicyRef    string
	Version            uint64
}

// Grant is reference bootstrap state. It demonstrates the normative separation
// between View data scope and principal delegation without defining a public
// management wire format.
type Grant struct {
	ID                v1alpha1.GrantID
	PrincipalRef      string
	ActorRef          string
	ViewRef           v1alpha1.ViewID
	AllowedOperations []v1alpha1.Operation
	AllowedAudiences  []v1alpha1.Audience
	ExpiresAt         time.Time
	Revoked           bool
	Version           uint64
}

// IssuerAuthorization authenticates the principal redeeming a Grant. A Grant
// reference alone is never sufficient to issue a Runtime capability.
type IssuerAuthorization struct {
	PrincipalRef string
	Credential   string
}

type IssueCapabilityRequest struct {
	GrantRef   v1alpha1.GrantID
	ActorRef   string
	Audience   v1alpha1.Audience
	Operations []v1alpha1.Operation
	TTL        time.Duration
}

type RuntimeCapability struct {
	Token     v1alpha1.CapabilityToken
	ExpiresAt time.Time
}

// CreateRealm provisions reference state. It is a test/bootstrap operation, not
// part of the v1alpha1 data plane.
func (s *Server) CreateRealm(realm Realm) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if realm.ID == "" {
		return fmt.Errorf("realm ID is required")
	}
	if _, exists := s.realms[realm.ID]; exists {
		return fmt.Errorf("realm %q already exists", realm.ID)
	}
	s.realms[realm.ID] = realm
	return nil
}

// CreateIdentity provisions reference cognitive continuity.
func (s *Server) CreateIdentity(identity Identity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if identity.ID == "" || identity.RealmID == "" {
		return fmt.Errorf("identity and realm IDs are required")
	}
	if _, ok := s.realms[identity.RealmID]; !ok {
		return fmt.Errorf("realm %q does not exist", identity.RealmID)
	}
	if _, exists := s.identities[identity.ID]; exists {
		return fmt.Errorf("identity %q already exists", identity.ID)
	}
	s.identities[identity.ID] = identity
	return nil
}

// CreateSpace provisions one isolated reference Space.
func (s *Server) CreateSpace(space Space) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if space.ID == "" || space.RealmID == "" {
		return fmt.Errorf("space and realm IDs are required")
	}
	if _, ok := s.realms[space.RealmID]; !ok {
		return fmt.Errorf("realm %q does not exist", space.RealmID)
	}
	switch space.Class {
	case v1alpha1.SpaceClassPrivate:
		identity, ok := s.identities[space.IdentityID]
		if !ok || identity.RealmID != space.RealmID {
			return fmt.Errorf("private Space requires an Identity in the same Realm")
		}
	case v1alpha1.SpaceClassShared:
		if space.IdentityID != "" {
			return fmt.Errorf("shared Space cannot be owned by one Identity")
		}
	default:
		return fmt.Errorf("unsupported Space class %q", space.Class)
	}
	if _, exists := s.spaces[space.ID]; exists {
		return fmt.Errorf("space %q already exists", space.ID)
	}
	s.spaces[space.ID] = space
	s.candidateReads[space.ID] = &atomic.Uint64{}
	return nil
}

// CreateView provisions a View definition without principal or operation data.
func (s *Server) CreateView(view ViewDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if view.ID == "" || view.RealmID == "" || view.Version == 0 {
		return fmt.Errorf("view ID, realm ID, and non-zero version are required")
	}
	if _, exists := s.views[view.ID]; exists {
		return fmt.Errorf("view %q already exists", view.ID)
	}
	if len(view.ReadSpaceIDs) == 0 {
		return fmt.Errorf("view requires at least one readable Space")
	}
	seen := make(map[v1alpha1.SpaceID]struct{}, len(view.ReadSpaceIDs))
	mostRestrictive := v1alpha1.SpaceClassShared
	for _, spaceID := range view.ReadSpaceIDs {
		space, ok := s.spaces[spaceID]
		if !ok || space.RealmID != view.RealmID {
			return fmt.Errorf("read Space %q is absent or belongs to another Realm", spaceID)
		}
		if _, duplicate := seen[spaceID]; duplicate {
			return fmt.Errorf("read Space %q is duplicated", spaceID)
		}
		seen[spaceID] = struct{}{}
		if space.Class == v1alpha1.SpaceClassPrivate {
			mostRestrictive = v1alpha1.SpaceClassPrivate
		}
	}
	if view.WriteSpaceID != "" {
		space, ok := s.spaces[view.WriteSpaceID]
		if !ok || space.RealmID != view.RealmID {
			return fmt.Errorf("write Space %q is absent or belongs to another Realm", view.WriteSpaceID)
		}
		if _, readable := seen[view.WriteSpaceID]; !readable {
			return fmt.Errorf("write Space %q must also be readable", view.WriteSpaceID)
		}
		if space.Class == v1alpha1.SpaceClassPrivate {
			mostRestrictive = v1alpha1.SpaceClassPrivate
		}
	}
	if view.MaxDisclosureClass != mostRestrictive {
		return fmt.Errorf("view disclosure class %q does not match accessible data class %q", view.MaxDisclosureClass, mostRestrictive)
	}
	s.views[view.ID] = cloneView(view)
	return nil
}

// CreateGrant provisions a principal and actor delegation for one View.
func (s *Server) CreateGrant(grant Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if grant.ID == "" || grant.PrincipalRef == "" || grant.ActorRef == "" || grant.ViewRef == "" || grant.Version == 0 {
		return fmt.Errorf("grant identity, principal, actor, View, and version are required")
	}
	if _, exists := s.grants[grant.ID]; exists {
		return fmt.Errorf("grant %q already exists", grant.ID)
	}
	view, ok := s.views[grant.ViewRef]
	if !ok {
		return fmt.Errorf("view %q does not exist", grant.ViewRef)
	}
	if !grant.ExpiresAt.After(s.now()) {
		return fmt.Errorf("grant expiration must be in the future")
	}
	if len(grant.AllowedOperations) == 0 || len(grant.AllowedAudiences) == 0 {
		return fmt.Errorf("grant requires operations and audiences")
	}
	for _, operation := range grant.AllowedOperations {
		if !validOperation(operation) {
			return fmt.Errorf("unsupported operation %q", operation)
		}
	}
	for _, audience := range grant.AllowedAudiences {
		if !validAudience(audience) {
			return fmt.Errorf("unsupported audience %q", audience)
		}
		if audience == v1alpha1.AudienceShared && view.MaxDisclosureClass == v1alpha1.SpaceClassPrivate {
			return fmt.Errorf("private View cannot be granted to shared audience")
		}
		if audience == v1alpha1.AudiencePrivate && containsOperation(grant.AllowedOperations, v1alpha1.OperationRemember) {
			writeSpace, ok := s.spaces[view.WriteSpaceID]
			if !ok || writeSpace.Class != v1alpha1.SpaceClassPrivate {
				return fmt.Errorf("private audience Remember requires a private write Space")
			}
		}
	}
	s.grants[grant.ID] = cloneGrant(grant)
	return nil
}

// CreateIssuerAuthorization creates fixture-only principal authentication for
// the local reference issuer.
func (s *Server) CreateIssuerAuthorization(principalRef string) (IssuerAuthorization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if principalRef == "" {
		return IssuerAuthorization{}, fmt.Errorf("principal is required")
	}
	credential, err := s.randomToken()
	if err != nil {
		return IssuerAuthorization{}, err
	}
	auth := IssuerAuthorization{PrincipalRef: principalRef, Credential: string(credential)}
	s.issuerCredentials[principalRef] = auth.Credential
	return auth, nil
}

// IssueCapability creates a random opaque token backed by server-side state.
func (s *Server) IssueCapability(auth IssuerAuthorization, request IssueCapabilityRequest) (RuntimeCapability, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.grants[request.GrantRef]
	if !ok || grant.Revoked || !grant.ExpiresAt.After(s.now()) {
		return RuntimeCapability{}, fmt.Errorf("grant is absent, revoked, or expired")
	}
	credential, authenticated := s.issuerCredentials[auth.PrincipalRef]
	if !authenticated || credential == "" || credential != auth.Credential || auth.PrincipalRef != grant.PrincipalRef {
		return RuntimeCapability{}, fmt.Errorf("issuer principal is not authorized for grant")
	}
	if request.ActorRef != grant.ActorRef {
		return RuntimeCapability{}, fmt.Errorf("actor does not match grant")
	}
	if request.TTL <= 0 {
		return RuntimeCapability{}, fmt.Errorf("capability TTL must be positive")
	}
	if !containsAudience(grant.AllowedAudiences, request.Audience) {
		return RuntimeCapability{}, fmt.Errorf("audience is not granted")
	}
	if len(request.Operations) == 0 {
		return RuntimeCapability{}, fmt.Errorf("at least one operation is required")
	}
	for _, operation := range request.Operations {
		if !containsOperation(grant.AllowedOperations, operation) {
			return RuntimeCapability{}, fmt.Errorf("operation %q is not granted", operation)
		}
	}
	expiresAt := s.now().Add(request.TTL)
	if grant.ExpiresAt.Before(expiresAt) {
		expiresAt = grant.ExpiresAt
	}
	token, err := s.randomToken()
	if err != nil {
		return RuntimeCapability{}, err
	}
	view := s.views[grant.ViewRef]
	s.capabilities[token] = capabilityState{
		grantID:      grant.ID,
		principalRef: grant.PrincipalRef,
		viewVersion:  view.Version,
		actorRef:     request.ActorRef,
		audience:     request.Audience,
		operations:   append([]v1alpha1.Operation(nil), request.Operations...),
		expiresAt:    expiresAt,
	}
	return RuntimeCapability{Token: token, ExpiresAt: expiresAt}, nil
}

// RevokeGrant invalidates every capability derived from the Grant.
func (s *Server) RevokeGrant(grantID v1alpha1.GrantID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.grants[grantID]
	if !ok {
		return fmt.Errorf("grant %q does not exist", grantID)
	}
	grant.Revoked = true
	s.grants[grantID] = grant
	return nil
}

func cloneView(view ViewDefinition) ViewDefinition {
	view.ReadSpaceIDs = append([]v1alpha1.SpaceID(nil), view.ReadSpaceIDs...)
	return view
}

func cloneGrant(grant Grant) Grant {
	grant.AllowedOperations = append([]v1alpha1.Operation(nil), grant.AllowedOperations...)
	grant.AllowedAudiences = append([]v1alpha1.Audience(nil), grant.AllowedAudiences...)
	return grant
}

func validOperation(operation v1alpha1.Operation) bool {
	switch operation {
	case v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus:
		return true
	default:
		return false
	}
}

func validAudience(audience v1alpha1.Audience) bool {
	return audience == v1alpha1.AudiencePrivate || audience == v1alpha1.AudienceShared
}

func containsOperation(operations []v1alpha1.Operation, target v1alpha1.Operation) bool {
	for _, operation := range operations {
		if operation == target {
			return true
		}
	}
	return false
}

func containsAudience(audiences []v1alpha1.Audience, target v1alpha1.Audience) bool {
	for _, audience := range audiences {
		if audience == target {
			return true
		}
	}
	return false
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
