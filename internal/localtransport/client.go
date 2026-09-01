package localtransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

// AdminClient is the owner-local M1 management client. It is deliberately
// internal because the management protocol is not yet a stable public API.
type AdminClient struct {
	http       *http.Client
	credential string
}

// NewAdminClient creates an M1 management client for one Unix socket.
func NewAdminClient(socketPath, credential string) *AdminClient {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}
	return &AdminClient{http: &http.Client{Transport: transport}, credential: credential}
}

func (c *AdminClient) Bootstrap(ctx context.Context, request appliance.BootstrapRequest) (appliance.BootstrapResponse, error) {
	var response appliance.BootstrapResponse
	err := c.do(ctx, http.MethodPost, AdminPathBootstrap, request, &response)
	return response, err
}

func (c *AdminClient) Inspect(ctx context.Context) (appliance.Inspection, error) {
	var response appliance.Inspection
	err := c.do(ctx, http.MethodGet, AdminPathInspect, nil, &response)
	return response, err
}

func (c *AdminClient) RebuildFTS(ctx context.Context) error {
	var response map[string]bool
	return c.do(ctx, http.MethodPost, AdminPathRebuild, struct{}{}, &response)
}

func (c *AdminClient) RevokeGrant(ctx context.Context, grantID v1alpha1.GrantID) error {
	var response map[string]bool
	return c.do(ctx, http.MethodPost, AdminPathRevoke, map[string]v1alpha1.GrantID{"grant_id": grantID}, &response)
}

func (c *AdminClient) RotateIssuerCredential(ctx context.Context, principalRef string) (appliance.IssuerAuthorization, error) {
	var response appliance.IssuerAuthorization
	err := c.do(ctx, http.MethodPost, AdminPathRotate, map[string]string{"principal_ref": principalRef}, &response)
	return response, err
}

func (c *AdminClient) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://memoryd"+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.credential)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope struct {
			Error string `json:"error"`
		}
		if err := decoder.Decode(&envelope); err != nil || envelope.Error == "" {
			return fmt.Errorf("memoryd management returned HTTP %d", response.StatusCode)
		}
		return fmt.Errorf("memoryd management: %s", envelope.Error)
	}
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode memoryd management response: %w", err)
	}
	return nil
}
