package steward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
)

// HTTPProvider sends the versioned structured Steward protocol to one fixed
// endpoint. It never follows redirects that could disclose its bearer.
type HTTPProvider struct {
	endpoint   string
	credential string
	client     *http.Client
}

// NewHTTPProvider validates an egress endpoint and freezes its credential.
// Plain HTTP is allowed only for an explicit loopback host.
func NewHTTPProvider(endpoint, credential string, timeout time.Duration) (*HTTPProvider, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return nil, fmt.Errorf("Steward endpoint is invalid")
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return nil, fmt.Errorf("plain HTTP Steward endpoint must be loopback")
		}
	default:
		return nil, fmt.Errorf("Steward endpoint must use HTTPS or loopback HTTP")
	}
	if timeout < time.Second || timeout > 5*time.Minute {
		return nil, fmt.Errorf("Steward provider timeout must be within 1s..5m")
	}
	return &HTTPProvider{
		endpoint: endpoint, credential: credential,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Propose performs one bounded provider exchange. Response bodies and request
// content are never included in returned errors.
func (p *HTTPProvider) Propose(ctx context.Context, request stewardv1alpha1.WorkRequest) (stewardv1alpha1.Proposal, error) {
	if request.Protocol != stewardv1alpha1.ProtocolVersion {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "protocol_invalid", Retryable: false}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "request_invalid", Retryable: false}
	}
	if len(encoded) > request.Profile.MaxInputBytes {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "request_too_large", Retryable: false}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "request_invalid", Retryable: false}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if p.credential != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+p.credential)
	}
	response, err := p.client.Do(httpRequest)
	if err != nil {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "provider_unavailable", Retryable: true}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return stewardv1alpha1.Proposal{}, &ProviderError{
			Code: "provider_http_error", Retryable: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500,
		}
	}
	limit := int64(request.Profile.MaxOutputBytes) + 1
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "provider_response_invalid", Retryable: true}
	}
	if len(body) > request.Profile.MaxOutputBytes {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "provider_response_too_large", Retryable: false}
	}
	var envelope stewardv1alpha1.ProviderResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "provider_response_invalid", Retryable: false}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "provider_response_invalid", Retryable: false}
	}
	if envelope.Protocol != stewardv1alpha1.ProtocolVersion {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "protocol_invalid", Retryable: false}
	}
	if err := envelope.Proposal.ValidateShape(); err != nil {
		return stewardv1alpha1.Proposal{}, &ProviderError{Code: "proposal_invalid", Retryable: false}
	}
	return envelope.Proposal, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func readOwnerOnlyFile(path, description string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", description, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s must be an owner-only regular file", description)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", description, err)
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", description, err)
	}
	if int64(len(value)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", description, maxBytes)
	}
	return value, nil
}
