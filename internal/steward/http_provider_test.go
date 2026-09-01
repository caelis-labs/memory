package steward

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestHTTPProviderSendsBoundedStructuredInputAndBearerOutOfBand(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Errorf("provider request method=%s authorization=%q", request.Method, request.Header.Get("Authorization"))
		}
		var err error
		body, err = io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(writer).Encode(stewardv1alpha1.ProviderResponse{
			Protocol: stewardv1alpha1.ProtocolVersion,
			Proposal: stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore},
		})
	}))
	defer server.Close()
	provider, err := NewHTTPProvider(server.URL, "provider-secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	request := validWorkRequest()
	proposal, err := provider.Propose(t.Context(), request)
	if err != nil || proposal.Operation != stewardv1alpha1.OperationIgnore {
		t.Fatalf("Propose = %+v, error = %v", proposal, err)
	}
	for _, forbidden := range []string{"provider-secret", "space-private", "job-private", "lease-private", "actor-private"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("provider request body contains %q: %s", forbidden, body)
		}
	}
	var decoded stewardv1alpha1.WorkRequest
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Protocol != stewardv1alpha1.ProtocolVersion || decoded.Receipt.Text != request.Receipt.Text || decoded.Profile.SystemPrompt != request.Profile.SystemPrompt {
		t.Fatalf("provider WorkRequest = %+v", decoded)
	}
}

func TestHTTPProviderRejectsRedirectOversizeAndNonStrictResponse(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		code    string
	}{
		{
			name: "redirect",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Location", "https://example.invalid/steal")
				writer.WriteHeader(http.StatusTemporaryRedirect)
			},
			code: "provider_http_error",
		},
		{
			name: "oversize",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(strings.Repeat("x", 2048)))
			},
			code: "provider_response_too_large",
		},
		{
			name: "unknown field",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(`{"protocol":"memory.steward.v1alpha1","proposal":{"operation":"IGNORE"},"authority":"space-private"}`))
			},
			code: "provider_response_invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			provider, err := NewHTTPProvider(server.URL, "secret", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			request := validWorkRequest()
			if test.name == "oversize" {
				request.Profile.MaxOutputBytes = 1024
			}
			_, err = provider.Propose(t.Context(), request)
			var providerErr *ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != test.code {
				t.Fatalf("Propose error = %#v, want %q", err, test.code)
			}
		})
	}
	if _, err := NewHTTPProvider("http://example.com/steward", "", time.Second); err == nil {
		t.Fatal("non-loopback plain HTTP endpoint accepted")
	}
}

func TestLoadConfigRequiresOwnerOnlyStrictFiles(t *testing.T) {
	directory := t.TempDir()
	credentialPath := filepath.Join(directory, "provider.token")
	if err := os.WriteFile(credentialPath, []byte("provider-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	configPath := filepath.Join(directory, "providers.json")
	config := `{"workers":2,"lease_seconds":30,"poll_ms":50,"retry_base_ms":100,"max_attempts":4,"providers":[{"ref":"provider-test","endpoint":"` + server.URL + `","credential_file":"` + credentialPath + `","timeout_ms":1000}]}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, providers, options, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Workers != 2 || len(providers) != 1 || options.MaxAttempts != 4 {
		t.Fatalf("loaded Config=%+v providers=%d options=%+v", loaded, len(providers), options)
	}
	if err := os.Chmod(configPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := LoadConfig(configPath); err == nil {
		t.Fatal("group-readable provider configuration accepted")
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(credentialPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := LoadConfig(configPath); err == nil {
		t.Fatal("group-readable provider credential accepted")
	}
}

func validWorkRequest() stewardv1alpha1.WorkRequest {
	return stewardv1alpha1.WorkRequest{
		Protocol: stewardv1alpha1.ProtocolVersion,
		Profile: stewardv1alpha1.ProfileSpec{
			ProfileID: "profile-test", Version: 1, ProviderRef: "provider-test", Model: "model-test",
			SystemPrompt: "organize evidence", MaxContextRecords: 8, MaxInputBytes: 128 << 10, MaxOutputBytes: 16 << 10,
		},
		Receipt: stewardv1alpha1.ReceiptInput{
			ReceiptID: "receipt-test", Text: "the project uses Go", ReceivedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		Records: []stewardv1alpha1.RecordContext{{
			RecordID: "record-test", Revision: 1, Kind: "claim", Text: "prior claim",
			EvidenceRefs: []v1alpha1.ReceiptID{"receipt-prior"},
		}},
	}
}
