package appliance

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

type databaseExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type authorizedView struct {
	id                 v1alpha1.ViewID
	version            uint64
	readSpaceIDs       []v1alpha1.SpaceID
	writeSpaceID       v1alpha1.SpaceID
	maxDisclosureClass v1alpha1.SpaceClass
}

// Bootstrap creates a complete local topology in one transaction.
func (s *Store) Bootstrap(ctx context.Context, request BootstrapRequest) (BootstrapResponse, error) {
	if err := s.requireMutableGeneration(); err != nil {
		return BootstrapResponse{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BootstrapResponse{}, fmt.Errorf("begin bootstrap: %w", err)
	}
	rollback := func(err error) (BootstrapResponse, error) {
		_ = tx.Rollback()
		return BootstrapResponse{}, err
	}
	now := formatTime(s.now().UTC())
	for _, realm := range request.Realms {
		if realm.ID == "" {
			return rollback(fmt.Errorf("realm ID is required"))
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO realms(id, created_at) VALUES (?, ?)`, realm.ID, now); err != nil {
			return rollback(fmt.Errorf("create Realm %q: %w", realm.ID, err))
		}
	}
	for _, identity := range request.Identities {
		if identity.ID == "" || identity.RealmID == "" {
			return rollback(fmt.Errorf("identity and realm IDs are required"))
		}
		if !realmExists(ctx, tx, identity.RealmID) {
			return rollback(fmt.Errorf("Realm %q does not exist", identity.RealmID))
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO identities(id, realm_id, created_at) VALUES (?, ?, ?)`,
			identity.ID, identity.RealmID, now); err != nil {
			return rollback(fmt.Errorf("create Identity %q: %w", identity.ID, err))
		}
	}
	for _, space := range request.Spaces {
		if err := validateSpace(ctx, tx, space); err != nil {
			return rollback(err)
		}
		var identity any
		if space.IdentityID != "" {
			identity = space.IdentityID
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO spaces(id, realm_id, identity_id, class, created_at) VALUES (?, ?, ?, ?, ?)`,
			space.ID, space.RealmID, identity, space.Class, now); err != nil {
			return rollback(fmt.Errorf("create Space %q: %w", space.ID, err))
		}
		if err := createSpaceIndex(ctx, tx, space.ID); err != nil {
			return rollback(err)
		}
	}
	for _, view := range request.Views {
		if err := createView(ctx, tx, view, now); err != nil {
			return rollback(err)
		}
	}
	for _, grant := range request.Grants {
		if err := createGrant(ctx, tx, grant, now, s.now().UTC()); err != nil {
			return rollback(err)
		}
	}
	response := BootstrapResponse{IssuerCredentials: make(map[string]string, len(request.IssuerPrincipals))}
	for _, principal := range request.IssuerPrincipals {
		if principal == "" {
			return rollback(fmt.Errorf("issuer principal is required"))
		}
		credential, err := s.randomToken(32)
		if err != nil {
			return rollback(fmt.Errorf("create issuer credential: %w", err))
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO issuer_credentials(principal_ref, credential_digest, created_at) VALUES (?, ?, ?)`,
			principal, digestBytes(credential), now); err != nil {
			return rollback(fmt.Errorf("create issuer credential for %q: %w", principal, err))
		}
		response.IssuerCredentials[principal] = credential
	}
	if err := tx.Commit(); err != nil {
		return BootstrapResponse{}, fmt.Errorf("commit bootstrap: %w", err)
	}
	return response, nil
}

func realmExists(ctx context.Context, db databaseExecutor, realmID v1alpha1.RealmID) bool {
	var exists bool
	_ = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM realms WHERE id = ?)`, realmID).Scan(&exists)
	return exists
}

func validateSpace(ctx context.Context, db databaseExecutor, space Space) error {
	if space.ID == "" || space.RealmID == "" {
		return fmt.Errorf("space and realm IDs are required")
	}
	if !realmExists(ctx, db, space.RealmID) {
		return fmt.Errorf("Realm %q does not exist", space.RealmID)
	}
	switch space.Class {
	case v1alpha1.SpaceClassPrivate:
		if space.IdentityID == "" {
			return fmt.Errorf("private Space requires an Identity")
		}
		var realmID v1alpha1.RealmID
		if err := db.QueryRowContext(ctx,
			`SELECT realm_id FROM identities WHERE id = ?`, space.IdentityID).Scan(&realmID); err != nil || realmID != space.RealmID {
			return fmt.Errorf("private Space requires an Identity in the same Realm")
		}
	case v1alpha1.SpaceClassShared:
		if space.IdentityID != "" {
			return fmt.Errorf("shared Space cannot be owned by one Identity")
		}
	default:
		return fmt.Errorf("unsupported Space class %q", space.Class)
	}
	return nil
}

func createView(ctx context.Context, tx *sql.Tx, view ViewDefinition, now string) error {
	if view.ID == "" || view.RealmID == "" || view.Version == 0 {
		return fmt.Errorf("view ID, realm ID, and non-zero version are required")
	}
	if len(view.ReadSpaceIDs) == 0 {
		return fmt.Errorf("View requires at least one readable Space")
	}
	seen := make(map[v1alpha1.SpaceID]struct{}, len(view.ReadSpaceIDs))
	mostRestrictive := v1alpha1.SpaceClassShared
	for _, spaceID := range view.ReadSpaceIDs {
		class, realmID, err := readSpaceScope(ctx, tx, spaceID)
		if err != nil || realmID != view.RealmID {
			return fmt.Errorf("read Space %q is absent or belongs to another Realm", spaceID)
		}
		if _, duplicate := seen[spaceID]; duplicate {
			return fmt.Errorf("read Space %q is duplicated", spaceID)
		}
		seen[spaceID] = struct{}{}
		if class == v1alpha1.SpaceClassPrivate {
			mostRestrictive = v1alpha1.SpaceClassPrivate
		}
	}
	if view.WriteSpaceID != "" {
		class, realmID, err := readSpaceScope(ctx, tx, view.WriteSpaceID)
		if err != nil || realmID != view.RealmID {
			return fmt.Errorf("write Space %q is absent or belongs to another Realm", view.WriteSpaceID)
		}
		if _, readable := seen[view.WriteSpaceID]; !readable {
			return fmt.Errorf("write Space %q must also be readable", view.WriteSpaceID)
		}
		if class == v1alpha1.SpaceClassPrivate {
			mostRestrictive = v1alpha1.SpaceClassPrivate
		}
	}
	if view.MaxDisclosureClass != mostRestrictive {
		return fmt.Errorf("View disclosure class %q does not match accessible data class %q", view.MaxDisclosureClass, mostRestrictive)
	}
	var writeSpace any
	if view.WriteSpaceID != "" {
		writeSpace = view.WriteSpaceID
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO views(id, realm_id, write_space_id, max_disclosure_class, recall_policy_ref, version, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		view.ID, view.RealmID, writeSpace, view.MaxDisclosureClass, view.RecallPolicyRef, view.Version, now); err != nil {
		return fmt.Errorf("create View %q: %w", view.ID, err)
	}
	for ordinal, spaceID := range view.ReadSpaceIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO view_read_spaces(view_id, space_id, ordinal) VALUES (?, ?, ?)`,
			view.ID, spaceID, ordinal); err != nil {
			return fmt.Errorf("create View %q read Space: %w", view.ID, err)
		}
	}
	return nil
}

func createGrant(ctx context.Context, tx *sql.Tx, grant Grant, createdAt string, now time.Time) error {
	if grant.ID == "" || grant.PrincipalRef == "" || grant.ActorRef == "" || grant.ViewRef == "" || grant.Version == 0 {
		return fmt.Errorf("grant identity, principal, actor, View, and version are required")
	}
	if !grant.ExpiresAt.After(now) {
		return fmt.Errorf("Grant expiration must be in the future")
	}
	if len(grant.AllowedOperations) == 0 || len(grant.AllowedAudiences) == 0 {
		return fmt.Errorf("Grant requires operations and audiences")
	}
	var disclosure v1alpha1.SpaceClass
	var writeSpace sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT max_disclosure_class, write_space_id FROM views WHERE id = ?`, grant.ViewRef).Scan(&disclosure, &writeSpace); err != nil {
		return fmt.Errorf("View %q does not exist", grant.ViewRef)
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
		if audience == v1alpha1.AudienceShared && disclosure == v1alpha1.SpaceClassPrivate {
			return fmt.Errorf("private View cannot be granted to shared audience")
		}
		if audience == v1alpha1.AudiencePrivate && slices.Contains(grant.AllowedOperations, v1alpha1.OperationRemember) {
			var class v1alpha1.SpaceClass
			if !writeSpace.Valid || tx.QueryRowContext(ctx, `SELECT class FROM spaces WHERE id = ?`, writeSpace.String).Scan(&class) != nil || class != v1alpha1.SpaceClassPrivate {
				return fmt.Errorf("private audience Remember requires a private write Space")
			}
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO grants(id, principal_ref, actor_ref, view_id, expires_at, revoked, version, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		grant.ID, grant.PrincipalRef, grant.ActorRef, grant.ViewRef, formatTime(grant.ExpiresAt), grant.Revoked, grant.Version, createdAt); err != nil {
		return fmt.Errorf("create Grant %q: %w", grant.ID, err)
	}
	for _, operation := range grant.AllowedOperations {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO grant_operations(grant_id, operation) VALUES (?, ?)`, grant.ID, operation); err != nil {
			return fmt.Errorf("create Grant %q operation: %w", grant.ID, err)
		}
	}
	for _, audience := range grant.AllowedAudiences {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO grant_audiences(grant_id, audience) VALUES (?, ?)`, grant.ID, audience); err != nil {
			return fmt.Errorf("create Grant %q audience: %w", grant.ID, err)
		}
	}
	return nil
}

func readSpaceScope(ctx context.Context, db databaseExecutor, spaceID v1alpha1.SpaceID) (v1alpha1.SpaceClass, v1alpha1.RealmID, error) {
	var class v1alpha1.SpaceClass
	var realmID v1alpha1.RealmID
	err := db.QueryRowContext(ctx, `SELECT class, realm_id FROM spaces WHERE id = ?`, spaceID).Scan(&class, &realmID)
	return class, realmID, err
}

// IssueCapability creates random bearer authority backed by durable server-side
// state. A Grant reference without principal authentication is insufficient.
func (s *Store) IssueCapability(ctx context.Context, request IssueCapabilityRequest) (RuntimeCapability, error) {
	if err := s.requireMutableGeneration(); err != nil {
		return RuntimeCapability{}, err
	}
	if request.Authorization.PrincipalRef == "" || request.Authorization.Credential == "" {
		return RuntimeCapability{}, fmt.Errorf("%w: issuer authorization is required", ErrCapabilityIssueInvalid)
	}
	const maxCapabilityTTL = 24 * time.Hour
	ttl := request.TTL
	if ttl == 0 && request.TTLSeconds > 0 {
		if request.TTLSeconds > int64(maxCapabilityTTL/time.Second) {
			return RuntimeCapability{}, fmt.Errorf("%w: capability TTL exceeds 24 hours", ErrCapabilityIssueInvalid)
		}
		ttl = time.Duration(request.TTLSeconds) * time.Second
	}
	if ttl <= 0 || ttl > maxCapabilityTTL {
		return RuntimeCapability{}, fmt.Errorf("%w: capability TTL must be between 1 nanosecond and 24 hours", ErrCapabilityIssueInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeCapability{}, fmt.Errorf("begin capability issuance: %w", err)
	}
	rollback := func(err error) (RuntimeCapability, error) {
		_ = tx.Rollback()
		return RuntimeCapability{}, err
	}
	var storedCredential []byte
	if err := tx.QueryRowContext(ctx,
		`SELECT credential_digest FROM issuer_credentials WHERE principal_ref = ?`,
		request.Authorization.PrincipalRef).Scan(&storedCredential); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rollback(ErrCapabilityIssueUnauthorized)
		}
		return rollback(fmt.Errorf("read issuer credential: %w", err))
	}
	if subtle.ConstantTimeCompare(storedCredential, digestBytes(request.Authorization.Credential)) != 1 {
		return rollback(ErrCapabilityIssueUnauthorized)
	}
	var principal, actor string
	var viewID v1alpha1.ViewID
	var grantExpiresRaw string
	var revoked bool
	if err := tx.QueryRowContext(ctx,
		`SELECT principal_ref, actor_ref, view_id, expires_at, revoked FROM grants WHERE id = ?`,
		request.GrantRef).Scan(&principal, &actor, &viewID, &grantExpiresRaw, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rollback(ErrCapabilityIssueUnauthorized)
		}
		return rollback(fmt.Errorf("read Grant: %w", err))
	}
	grantExpires, err := parseTime(grantExpiresRaw)
	if err != nil || revoked || !grantExpires.After(s.now()) || principal != request.Authorization.PrincipalRef {
		return rollback(ErrCapabilityIssueUnauthorized)
	}
	if request.ActorRef != actor {
		return rollback(ErrCapabilityIssueUnauthorized)
	}
	audienceGranted, err := rowExists(ctx, tx, `SELECT EXISTS(SELECT 1 FROM grant_audiences WHERE grant_id = ? AND audience = ?)`, request.GrantRef, request.Audience)
	if err != nil {
		return rollback(fmt.Errorf("read Grant audience: %w", err))
	}
	if !audienceGranted {
		return rollback(ErrCapabilityIssueUnauthorized)
	}
	if len(request.Operations) == 0 {
		return rollback(fmt.Errorf("%w: at least one operation is required", ErrCapabilityIssueInvalid))
	}
	for _, operation := range request.Operations {
		operationGranted, err := rowExists(ctx, tx,
			`SELECT EXISTS(SELECT 1 FROM grant_operations WHERE grant_id = ? AND operation = ?)`, request.GrantRef, operation)
		if err != nil {
			return rollback(fmt.Errorf("read Grant operation: %w", err))
		}
		if !validOperation(operation) || !operationGranted {
			return rollback(ErrCapabilityIssueUnauthorized)
		}
	}
	var viewVersion uint64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM views WHERE id = ?`, viewID).Scan(&viewVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rollback(ErrCapabilityIssueUnauthorized)
		}
		return rollback(fmt.Errorf("read View: %w", err))
	}
	expiresAt := s.now().Add(ttl)
	if grantExpires.Before(expiresAt) {
		expiresAt = grantExpires
	}
	token, err := s.randomToken(32)
	if err != nil {
		return rollback(fmt.Errorf("create capability: %w", err))
	}
	tokenDigest := digestBytes(token)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO capabilities(token_digest, grant_id, principal_ref, view_version, actor_ref, audience, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenDigest, request.GrantRef, principal, viewVersion, actor, request.Audience, formatTime(expiresAt), formatTime(s.now())); err != nil {
		return rollback(fmt.Errorf("store capability: %w", err))
	}
	for _, operation := range request.Operations {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO capability_operations(token_digest, operation) VALUES (?, ?)`, tokenDigest, operation); err != nil {
			return rollback(fmt.Errorf("store capability operation: %w", err))
		}
	}
	if err := tx.Commit(); err != nil {
		return RuntimeCapability{}, fmt.Errorf("commit capability: %w", err)
	}
	return RuntimeCapability{Token: v1alpha1.CapabilityToken(token), ExpiresAt: expiresAt.UTC()}, nil
}

// RotateIssuerCredential replaces one issuer digest and returns new raw
// authorization. Rotation is recoverable: if its response is lost, an
// authenticated manager can rotate the same principal again.
func (s *Store) RotateIssuerCredential(ctx context.Context, principalRef string) (IssuerAuthorization, error) {
	if err := s.requireMutableGeneration(); err != nil {
		return IssuerAuthorization{}, err
	}
	if principalRef == "" {
		return IssuerAuthorization{}, fmt.Errorf("issuer principal is required")
	}
	credential, err := s.randomToken(32)
	if err != nil {
		return IssuerAuthorization{}, fmt.Errorf("create issuer credential: %w", err)
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE issuer_credentials SET credential_digest = ?, created_at = ? WHERE principal_ref = ?`,
		digestBytes(credential), formatTime(s.now()), principalRef)
	if err != nil {
		return IssuerAuthorization{}, fmt.Errorf("rotate issuer credential: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return IssuerAuthorization{}, fmt.Errorf("inspect issuer credential rotation: %w", err)
	}
	if affected == 0 {
		return IssuerAuthorization{}, fmt.Errorf("issuer principal %q does not exist", principalRef)
	}
	return IssuerAuthorization{PrincipalRef: principalRef, Credential: credential}, nil
}

// RevokeGrant durably invalidates all capabilities derived from one Grant.
func (s *Store) RevokeGrant(ctx context.Context, grantID v1alpha1.GrantID) error {
	if err := s.requireMutableGeneration(); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE grants SET revoked = 1 WHERE id = ?`, grantID)
	if err != nil {
		return fmt.Errorf("revoke Grant: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect Grant revocation: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("Grant %q does not exist", grantID)
	}
	return nil
}

func (s *Store) authorize(ctx context.Context, db databaseExecutor, auth v1alpha1.CallAuthorization, operation v1alpha1.Operation) (authorizedView, error) {
	if auth.Capability == "" {
		return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability is missing or unknown", false)
	}
	var grantID v1alpha1.GrantID
	var principal, actor string
	var viewVersion uint64
	var capabilityAudience v1alpha1.Audience
	var capabilityExpiresRaw string
	if err := db.QueryRowContext(ctx,
		`SELECT grant_id, principal_ref, view_version, actor_ref, audience, expires_at
		 FROM capabilities WHERE token_digest = ?`, digestBytes(string(auth.Capability))).Scan(
		&grantID, &principal, &viewVersion, &actor, &capabilityAudience, &capabilityExpiresRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability is missing or unknown", false)
		}
		return authorizedView{}, s.databaseError("read capability", err)
	}
	capabilityExpires, err := parseTime(capabilityExpiresRaw)
	if err != nil || !capabilityExpires.After(s.now()) {
		return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability is expired or revoked", false)
	}
	var grantPrincipal, grantActor string
	var viewID v1alpha1.ViewID
	var grantExpiresRaw string
	var revoked bool
	if err := db.QueryRowContext(ctx,
		`SELECT principal_ref, actor_ref, view_id, expires_at, revoked FROM grants WHERE id = ?`, grantID).Scan(
		&grantPrincipal, &grantActor, &viewID, &grantExpiresRaw, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability is expired or revoked", false)
		}
		return authorizedView{}, s.databaseError("read Grant", err)
	}
	grantExpires, err := parseTime(grantExpiresRaw)
	if err != nil || revoked || !grantExpires.After(s.now()) || principal != grantPrincipal {
		return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability is expired or revoked", false)
	}
	if auth.ActorRef != actor || auth.ActorRef != grantActor {
		return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability actor mismatch", false)
	}
	audienceGranted, err := rowExists(ctx, db,
		`SELECT EXISTS(SELECT 1 FROM grant_audiences WHERE grant_id = ? AND audience = ?)`, grantID, auth.Audience)
	if err != nil {
		return authorizedView{}, s.databaseError("read Grant audience", err)
	}
	if auth.Audience != capabilityAudience || !audienceGranted {
		return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability audience mismatch", false)
	}
	capabilityOperation, err := rowExists(ctx, db,
		`SELECT EXISTS(SELECT 1 FROM capability_operations WHERE token_digest = ? AND operation = ?)`, digestBytes(string(auth.Capability)), operation)
	if err != nil {
		return authorizedView{}, s.databaseError("read capability operation", err)
	}
	grantOperation, err := rowExists(ctx, db,
		`SELECT EXISTS(SELECT 1 FROM grant_operations WHERE grant_id = ? AND operation = ?)`, grantID, operation)
	if err != nil {
		return authorizedView{}, s.databaseError("read Grant operation", err)
	}
	if !capabilityOperation || !grantOperation {
		return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability operation mismatch", false)
	}
	var view authorizedView
	view.id = viewID
	var writeSpace sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT version, write_space_id, max_disclosure_class FROM views WHERE id = ?`, viewID).Scan(
		&view.version, &writeSpace, &view.maxDisclosureClass); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability View is stale", false)
		}
		return authorizedView{}, s.databaseError("read View", err)
	}
	if view.version != viewVersion {
		return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability View is stale", false)
	}
	if writeSpace.Valid {
		view.writeSpaceID = v1alpha1.SpaceID(writeSpace.String)
	}
	rows, err := db.QueryContext(ctx,
		`SELECT space_id FROM view_read_spaces WHERE view_id = ? ORDER BY ordinal`, viewID)
	if err != nil {
		return authorizedView{}, s.databaseError("read View Spaces", err)
	}
	defer rows.Close()
	for rows.Next() {
		var spaceID v1alpha1.SpaceID
		if err := rows.Scan(&spaceID); err != nil {
			return authorizedView{}, s.databaseError("read View Space", err)
		}
		view.readSpaceIDs = append(view.readSpaceIDs, spaceID)
	}
	if err := rows.Err(); err != nil {
		return authorizedView{}, s.databaseError("read View Spaces", err)
	}
	if auth.Audience == v1alpha1.AudienceShared && view.maxDisclosureClass == v1alpha1.SpaceClassPrivate {
		return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "private View is incompatible with shared audience", false)
	}
	if operation == v1alpha1.OperationRemember && auth.Audience == v1alpha1.AudiencePrivate {
		class, _, err := readSpaceScope(ctx, db, view.writeSpaceID)
		if err != nil || class != v1alpha1.SpaceClassPrivate {
			return authorizedView{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "private Runtime cannot write shared memory", false)
		}
	}
	return view, nil
}

func rowExists(ctx context.Context, db databaseExecutor, query string, args ...any) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, query, args...).Scan(&exists)
	return exists, err
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
