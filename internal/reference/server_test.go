package reference

import (
	"testing"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/conformance"
)

func TestConformance(t *testing.T) {
	conformance.RunSemantic(t, newFixture)
}

func newFixture(t *testing.T) conformance.Fixture {
	t.Helper()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	server, err := New(Options{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	must(t, server.CreateRealm(Realm{ID: "realm-default"}))
	must(t, server.CreateIdentity(Identity{ID: "identity-bot-a", RealmID: "realm-default"}))
	must(t, server.CreateIdentity(Identity{ID: "identity-bot-b", RealmID: "realm-default"}))
	must(t, server.CreateSpace(Space{ID: "space-shared", RealmID: "realm-default", Class: v1alpha1.SpaceClassShared}))
	must(t, server.CreateSpace(Space{
		ID:         "space-bot-a",
		RealmID:    "realm-default",
		IdentityID: "identity-bot-a",
		Class:      v1alpha1.SpaceClassPrivate,
	}))
	must(t, server.CreateSpace(Space{
		ID:         "space-bot-b",
		RealmID:    "realm-default",
		IdentityID: "identity-bot-b",
		Class:      v1alpha1.SpaceClassPrivate,
	}))
	must(t, server.CreateView(ViewDefinition{
		ID:                 "view-bot-a-private",
		RealmID:            "realm-default",
		ReadSpaceIDs:       []v1alpha1.SpaceID{"space-shared", "space-bot-a"},
		WriteSpaceID:       "space-bot-a",
		MaxDisclosureClass: v1alpha1.SpaceClassPrivate,
		Version:            1,
	}))
	must(t, server.CreateView(ViewDefinition{
		ID:                 "view-bot-b-private",
		RealmID:            "realm-default",
		ReadSpaceIDs:       []v1alpha1.SpaceID{"space-shared", "space-bot-b"},
		WriteSpaceID:       "space-bot-b",
		MaxDisclosureClass: v1alpha1.SpaceClassPrivate,
		Version:            1,
	}))
	must(t, server.CreateView(ViewDefinition{
		ID:                 "view-shared",
		RealmID:            "realm-default",
		ReadSpaceIDs:       []v1alpha1.SpaceID{"space-shared"},
		WriteSpaceID:       "space-shared",
		MaxDisclosureClass: v1alpha1.SpaceClassShared,
		Version:            1,
	}))

	allOperations := []v1alpha1.Operation{
		v1alpha1.OperationRemember,
		v1alpha1.OperationRecall,
		v1alpha1.OperationReceiptStatus,
	}
	// Issue one capability and then advance the reference clock so only this
	// token is expired. All normal Grants are created afterward.
	must(t, server.CreateGrant(Grant{
		ID:                "grant-expired",
		PrincipalRef:      "principal:actor-bot-a",
		ActorRef:          "actor-bot-a",
		ViewRef:           "view-bot-a-private",
		AllowedOperations: allOperations,
		AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate},
		ExpiresAt:         now.Add(time.Hour),
		Version:           1,
	}))
	expired := issue(t, server, "grant-expired", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations)
	now = now.Add(2 * time.Hour)

	createGrant := func(id, actor string, view v1alpha1.ViewID, audience v1alpha1.Audience, operations []v1alpha1.Operation) {
		must(t, server.CreateGrant(Grant{
			ID:                v1alpha1.GrantID(id),
			PrincipalRef:      "principal:" + actor,
			ActorRef:          actor,
			ViewRef:           view,
			AllowedOperations: operations,
			AllowedAudiences:  []v1alpha1.Audience{audience},
			ExpiresAt:         now.Add(24 * time.Hour),
			Version:           1,
		}))
	}
	createGrant("grant-bot-a", "actor-bot-a", "view-bot-a-private", v1alpha1.AudiencePrivate, allOperations)
	createGrant("grant-bot-b", "actor-bot-b", "view-bot-b-private", v1alpha1.AudiencePrivate, allOperations)
	createGrant("grant-shared-a", "actor-shared-a", "view-shared", v1alpha1.AudienceShared, allOperations)
	createGrant("grant-shared-b", "actor-shared-b", "view-shared", v1alpha1.AudienceShared, allOperations)
	createGrant("grant-recall-only", "actor-bot-a", "view-bot-a-private", v1alpha1.AudiencePrivate, []v1alpha1.Operation{v1alpha1.OperationRecall})
	createGrant("grant-revoked", "actor-bot-a", "view-bot-a-private", v1alpha1.AudiencePrivate, allOperations)

	botA := issue(t, server, "grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations)
	botARenewed := issue(t, server, "grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations)
	botALabeled := issueLabels(t, server, "grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations,
		v1alpha1.LabelSet{"workspace:demo"})
	botAOther := issueLabels(t, server, "grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations,
		v1alpha1.LabelSet{"workspace:caelis"})
	botB := issue(t, server, "grant-bot-b", "actor-bot-b", v1alpha1.AudiencePrivate, allOperations)
	sharedA := issue(t, server, "grant-shared-a", "actor-shared-a", v1alpha1.AudienceShared, allOperations)
	sharedB := issue(t, server, "grant-shared-b", "actor-shared-b", v1alpha1.AudienceShared, allOperations)
	recallOnly := issue(t, server, "grant-recall-only", "actor-bot-a", v1alpha1.AudiencePrivate, []v1alpha1.Operation{v1alpha1.OperationRecall})
	revoked := issue(t, server, "grant-revoked", "actor-bot-a", v1alpha1.AudiencePrivate, allOperations)
	must(t, server.RevokeGrant("grant-revoked"))
	// Inject an impossible configuration to prove the data plane independently
	// rejects private-audience Remember into a shared write Space even if a
	// future provisioning bug bypasses CreateGrant validation.
	server.mu.Lock()
	server.grants["grant-private-shared-injected"] = Grant{
		ID:                "grant-private-shared-injected",
		PrincipalRef:      "principal:actor-bot-a",
		ActorRef:          "actor-bot-a",
		ViewRef:           "view-shared",
		AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRemember},
		AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate},
		ExpiresAt:         now.Add(time.Hour),
		Version:           1,
	}
	server.capabilities["cap-private-shared-injected"] = capabilityState{
		grantID:      "grant-private-shared-injected",
		principalRef: "principal:actor-bot-a",
		viewVersion:  1,
		actorRef:     "actor-bot-a",
		audience:     v1alpha1.AudiencePrivate,
		operations:   []v1alpha1.Operation{v1alpha1.OperationRemember},
		expiresAt:    now.Add(time.Hour),
	}
	server.mu.Unlock()

	return conformance.Fixture{
		Service:            server,
		BotAPrivate:        authorization(botA, "actor-bot-a", v1alpha1.AudiencePrivate),
		BotAPrivateRenewed: authorization(botARenewed, "actor-bot-a", v1alpha1.AudiencePrivate),
		BotAPrivateLabeled: authorization(botALabeled, "actor-bot-a", v1alpha1.AudiencePrivate),
		BotAPrivateOther:   authorization(botAOther, "actor-bot-a", v1alpha1.AudiencePrivate),
		BotBPrivate:        authorization(botB, "actor-bot-b", v1alpha1.AudiencePrivate),
		SharedA:            authorization(sharedA, "actor-shared-a", v1alpha1.AudienceShared),
		SharedB:            authorization(sharedB, "actor-shared-b", v1alpha1.AudienceShared),
		RecallOnly:         authorization(recallOnly, "actor-bot-a", v1alpha1.AudiencePrivate),
		Expired:            authorization(expired, "actor-bot-a", v1alpha1.AudiencePrivate),
		Revoked:            authorization(revoked, "actor-bot-a", v1alpha1.AudiencePrivate),
		PrivateSharedWrite: v1alpha1.CallAuthorization{
			Capability: "cap-private-shared-injected",
			ActorRef:   "actor-bot-a",
			Audience:   v1alpha1.AudiencePrivate,
		},
		BotAPrivateSpace: "space-bot-a",
		BotBPrivateSpace: "space-bot-b",
		SharedSpace:      "space-shared",
		CandidateReads:   server.CandidateReadCount,
		SetAvailable:     server.SetAvailable,
	}
}

func issue(
	t *testing.T,
	server *Server,
	grant v1alpha1.GrantID,
	actor string,
	audience v1alpha1.Audience,
	operations []v1alpha1.Operation,
) RuntimeCapability {
	return issueLabels(t, server, grant, actor, audience, operations, nil)
}

func issueLabels(
	t *testing.T,
	server *Server,
	grant v1alpha1.GrantID,
	actor string,
	audience v1alpha1.Audience,
	operations []v1alpha1.Operation,
	labels v1alpha1.LabelSet,
) RuntimeCapability {
	t.Helper()
	issuer, err := server.CreateIssuerAuthorization("principal:" + actor)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := server.IssueCapability(issuer, IssueCapabilityRequest{
		GrantRef:   grant,
		ActorRef:   actor,
		Audience:   audience,
		Operations: operations,
		Labels:     labels,
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return capability
}

func authorization(
	capability RuntimeCapability,
	actor string,
	audience v1alpha1.Audience,
) v1alpha1.CallAuthorization {
	return v1alpha1.CallAuthorization{
		Capability: capability.Token,
		ActorRef:   actor,
		Audience:   audience,
	}
}

func TestGrantReferenceCannotIssueCapability(t *testing.T) {
	fixture := newFixture(t)
	server := fixture.Service.(*Server)
	_, err := server.IssueCapability(IssuerAuthorization{}, IssueCapabilityRequest{
		GrantRef:   "grant-bot-a",
		ActorRef:   "actor-bot-a",
		Audience:   v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{v1alpha1.OperationRecall},
		TTL:        time.Hour,
	})
	if err == nil {
		t.Fatal("Grant reference issued a capability without principal authentication")
	}
}

func TestPrivateAudienceCannotReceiveSharedWriteGrant(t *testing.T) {
	fixture := newFixture(t)
	server := fixture.Service.(*Server)
	err := server.CreateGrant(Grant{
		ID:                "grant-private-to-shared",
		PrincipalRef:      "principal:actor-bot-a",
		ActorRef:          "actor-bot-a",
		ViewRef:           "view-shared",
		AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRemember},
		AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate},
		ExpiresAt:         time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Version:           1,
	})
	if err == nil {
		t.Fatal("private audience received a shared-write Grant")
	}
}

func TestRecallDeadlineStopsLongScan(t *testing.T) {
	fixture := newFixture(t)
	server := fixture.Service.(*Server)
	server.recallStep = func() { time.Sleep(5 * time.Millisecond) }
	if _, err := server.Remember(t.Context(), fixture.SharedA, v1alpha1.RememberRequest{
		Text:           "slow searchable fact",
		IdempotencyKey: "slow-fact",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := server.Recall(t.Context(), fixture.SharedA, v1alpha1.RecallRequest{
		Query: "searchable",
		Budget: v1alpha1.RecallBudget{
			MaxFragments: 8,
			MaxBytes:     4096,
			DeadlineMS:   1,
		},
	})
	if !v1alpha1.IsCode(err, v1alpha1.ErrorCodeDeadline) {
		t.Fatalf("Recall() error = %v, want deadline", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
