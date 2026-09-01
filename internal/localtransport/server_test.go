package localtransport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

func TestManagementAuthorizationIsSeparateAndFailClosed(t *testing.T) {
	store, err := appliance.Open(t.Context(), appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bootstrap, err := store.Bootstrap(t.Context(), appliance.BootstrapRequest{
		Realms:     []appliance.Realm{{ID: "realm-auth"}},
		Identities: []appliance.Identity{{ID: "identity-auth", RealmID: "realm-auth"}},
		Spaces: []appliance.Space{{
			ID: "space-auth", RealmID: "realm-auth", IdentityID: "identity-auth", Class: v1alpha1.SpaceClassPrivate,
		}},
		Views: []appliance.ViewDefinition{{
			ID: "view-auth", RealmID: "realm-auth", ReadSpaceIDs: []v1alpha1.SpaceID{"space-auth"},
			WriteSpaceID: "space-auth", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1,
		}},
		Grants: []appliance.Grant{{
			ID: "grant-auth", PrincipalRef: "principal:auth", ActorRef: "actor-auth", ViewRef: "view-auth",
			AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRecall},
			AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: time.Now().Add(time.Hour), Version: 1,
		}},
		IssuerPrincipals: []string{"principal:auth"},
	})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.IssueCapability(t.Context(), appliance.IssueCapabilityRequest{
		Authorization: appliance.IssuerAuthorization{PrincipalRef: "principal:auth", Credential: bootstrap.IssuerCredentials["principal:auth"]},
		GrantRef:      "grant-auth", ActorRef: "actor-auth", Audience: v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{v1alpha1.OperationRecall}, TTLSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Handler(store))
	t.Cleanup(server.Close)

	for name, bearer := range map[string]string{
		"Runtime": string(capability.Token),
		"issuer":  bootstrap.IssuerCredentials["principal:auth"],
	} {
		t.Run(name+" is not management authority", func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+managementv1alpha1.LocalPathInspect, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+bearer)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("wrong bearer status = %d, want 401", response.StatusCode)
			}
		})
	}

	credential, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+managementv1alpha1.LocalPathInspect, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(credential)))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("management bearer status = %d, want 200", response.StatusCode)
	}

	recallBody, err := json.Marshal(v1alpha1.RecallRequest{
		Query: "fact", Budget: v1alpha1.RecallBudget{MaxFragments: 1, MaxBytes: 64, DeadlineMS: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, bearer := range map[string]string{
		"management": strings.TrimSpace(string(credential)),
		"issuer":     bootstrap.IssuerCredentials["principal:auth"],
	} {
		t.Run(name+" is not Runtime authority", func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+v1alpha1.LocalPathRecall, bytes.NewReader(recallBody))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+bearer)
			request.Header.Set(v1alpha1.LocalHeaderActor, "actor-auth")
			request.Header.Set(v1alpha1.LocalHeaderAudience, string(v1alpha1.AudiencePrivate))
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.StatusCode)
			}
		})
	}

	publicIssueBody, err := json.Marshal(v1alpha1.CapabilityIssueRequest{
		PrincipalRef: "principal:auth", GrantRef: "grant-auth", ActorRef: "actor-auth",
		Audience: v1alpha1.AudiencePrivate, Operations: []v1alpha1.Operation{v1alpha1.OperationRecall}, TTLSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(publicIssueBody, []byte(bootstrap.IssuerCredentials["principal:auth"])) {
		t.Fatal("issuer credential entered the capability request body")
	}
	var firstToken v1alpha1.CapabilityToken
	for attempt := range 2 {
		request, err = http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+v1alpha1.LocalPathIssue, bytes.NewReader(publicIssueBody))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+bootstrap.IssuerCredentials["principal:auth"])
		response, err = http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var issued v1alpha1.RuntimeCapability
		if err := json.NewDecoder(response.Body).Decode(&issued); err != nil {
			_ = response.Body.Close()
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || issued.Token == "" {
			t.Fatalf("issuer plane attempt %d = %d/%+v", attempt, response.StatusCode, issued)
		}
		if attempt == 0 {
			firstToken = issued.Token
		} else if issued.Token == firstToken {
			t.Fatal("capability renewal reused the old bearer")
		}
	}
	for name, bearer := range map[string]string{
		"management": strings.TrimSpace(string(credential)),
		"Runtime":    string(capability.Token),
	} {
		t.Run(name+" is not issuer authority", func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+v1alpha1.LocalPathIssue, bytes.NewReader(publicIssueBody))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+bearer)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("wrong bearer status = %d, want 401", response.StatusCode)
			}
		})
	}
}

func TestCompatibilityHandshakeIsExactAndSecretFree(t *testing.T) {
	store, err := appliance.Open(t.Context(), appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server := httptest.NewServer(Handler(store, ServiceInfo{Version: "0.2.0-alpha.1", Revision: "abc123"}))
	t.Cleanup(server.Close)

	call := func(input v1alpha1.CompatibilityRequest) (*http.Response, []byte) {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+v1alpha1.LocalPathCompatibility, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var output bytes.Buffer
		if _, err := output.ReadFrom(response.Body); err != nil {
			t.Fatal(err)
		}
		return response, output.Bytes()
	}

	response, body := call(v1alpha1.CurrentCompatibilityRequest())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("handshake status = %d: %s", response.StatusCode, body)
	}
	var compatible v1alpha1.CompatibilityResponse
	if err := json.Unmarshal(body, &compatible); err != nil {
		t.Fatal(err)
	}
	if compatible.ServiceVersion != "0.2.0-alpha.1" || compatible.BuildRevision != "abc123" || compatible.SchemaVersion != appliance.CurrentSchemaVersion {
		t.Fatalf("handshake response = %+v", compatible)
	}
	wrong := v1alpha1.CurrentCompatibilityRequest()
	wrong.CoreProfile = "memory.core.future"
	response, body = call(wrong)
	if response.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("incompatible status = %d, want 426: %s", response.StatusCode, body)
	}
	var serviceErr v1alpha1.ServiceError
	if err := json.Unmarshal(body, &serviceErr); err != nil {
		t.Fatal(err)
	}
	if serviceErr.Code != v1alpha1.ErrorCodeIncompatible || strings.Contains(string(body), store.ManagementCredentialPath()) {
		t.Fatalf("incompatible response = %s", body)
	}
}
