package v1alpha1

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCompatibilityWireFixtures(t *testing.T) {
	request, err := json.Marshal(CurrentCompatibilityRequest())
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"protocol":"memory.local.v1alpha1","api_version":"memory.v1alpha1","core_profile":"memory.core.v1alpha1"}`; string(request) != want {
		t.Fatalf("CompatibilityRequest JSON = %s, want %s", request, want)
	}
	response, err := json.Marshal(CompatibilityResponse{
		Protocol: LocalTransportProtocol, APIVersion: ProtocolVersion, CoreProfile: CoreProfile,
		ServiceVersion: "0.2.0-alpha.1", BuildRevision: "abc123", SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"protocol":"memory.local.v1alpha1","api_version":"memory.v1alpha1","core_profile":"memory.core.v1alpha1","service_version":"0.2.0-alpha.1","build_revision":"abc123","schema_version":1}`; string(response) != want {
		t.Fatalf("CompatibilityResponse JSON = %s, want %s", response, want)
	}
}

func TestCapabilityIssueWireKeepsCredentialOutOfBody(t *testing.T) {
	authority, err := json.Marshal(CapabilityAuthorityRequest{
		PrincipalRef: "principal:bot-a", GrantRef: "grant-a", ViewRef: "view-a", ActorRef: "actor-a",
		Audience: AudiencePrivate, Operations: []Operation{OperationRemember, OperationRecall},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"principal_ref":"principal:bot-a","grant_ref":"grant-a","view_ref":"view-a","actor_ref":"actor-a","audience":"private","operations":["remember","recall"]}`; string(authority) != want {
		t.Fatalf("CapabilityAuthorityRequest JSON = %s, want %s", authority, want)
	}

	request, err := json.Marshal(CapabilityIssueRequest{
		PrincipalRef: "principal:bot-a", GrantRef: "grant-a", ActorRef: "actor-a",
		Audience: AudiencePrivate, Operations: []Operation{OperationRemember, OperationRecall}, TTLSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"principal_ref":"principal:bot-a","grant_ref":"grant-a","actor_ref":"actor-a","audience":"private","operations":["remember","recall"],"ttl_seconds":600}`; string(request) != want {
		t.Fatalf("CapabilityIssueRequest JSON = %s, want %s", request, want)
	}
	response, err := json.Marshal(RuntimeCapability{Token: "opaque", ExpiresAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"token":"opaque","expires_at":"2026-09-01T00:00:00Z"}`; string(response) != want {
		t.Fatalf("RuntimeCapability JSON = %s, want %s", response, want)
	}
}
