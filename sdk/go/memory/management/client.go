// Package management provides the owner-authorized local management client.
// Its credential cannot be used for Runtime calls or capability issuance.
package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const maxResponseBytes = 8 << 20

// Client binds one management bearer to one owner-only local Socket.
type Client struct {
	http       *http.Client
	credential string
}

func NewClient(socketPath, credential string) *Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}
	return &Client{http: &http.Client{Transport: transport}, credential: credential}
}

func (c *Client) Bootstrap(ctx context.Context, request managementv1alpha1.BootstrapRequest) (managementv1alpha1.BootstrapResponse, error) {
	var response managementv1alpha1.BootstrapResponse
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathBootstrap, request, &response)
	return response, err
}

func (c *Client) Inspect(ctx context.Context) (managementv1alpha1.Inspection, error) {
	var response managementv1alpha1.Inspection
	err := c.do(ctx, http.MethodGet, managementv1alpha1.LocalPathInspect, nil, &response)
	return response, err
}

func (c *Client) SearchReceipts(ctx context.Context, request managementv1alpha1.SearchReceiptsRequest) (managementv1alpha1.SearchReceiptsResponse, error) {
	var response managementv1alpha1.SearchReceiptsResponse
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathSearch, request, &response)
	return response, err
}

func (c *Client) TraceReceipt(ctx context.Context, request managementv1alpha1.TraceReceiptRequest) (managementv1alpha1.TraceReceiptResponse, error) {
	var response managementv1alpha1.TraceReceiptResponse
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathTrace, request, &response)
	return response, err
}

func (c *Client) CorrectReceipt(ctx context.Context, request managementv1alpha1.CorrectReceiptRequest) (managementv1alpha1.CorrectReceiptResponse, error) {
	var response managementv1alpha1.CorrectReceiptResponse
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathCorrect, request, &response)
	return response, err
}

func (c *Client) DeleteReceipt(ctx context.Context, request managementv1alpha1.DeleteReceiptRequest) (managementv1alpha1.DeleteReceiptResponse, error) {
	var response managementv1alpha1.DeleteReceiptResponse
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathDelete, request, &response)
	return response, err
}

func (c *Client) RebuildFTS(ctx context.Context) error {
	var response managementv1alpha1.RebuildFTSResponse
	return c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathRebuild, struct{}{}, &response)
}

func (c *Client) RevokeGrant(ctx context.Context, grantID memoryv1alpha1.GrantID) error {
	var response managementv1alpha1.RevokeGrantResponse
	return c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathRevokeGrant,
		managementv1alpha1.RevokeGrantRequest{GrantID: grantID}, &response)
}

func (c *Client) RotateIssuerCredential(ctx context.Context, principalRef string) (managementv1alpha1.IssuerAuthorization, error) {
	var response managementv1alpha1.IssuerAuthorization
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathRotateIssuer,
		managementv1alpha1.RotateIssuerRequest{PrincipalRef: principalRef}, &response)
	return response, err
}

func (c *Client) CloseIdleConnections() {
	c.http.CloseIdleConnections()
}

func (c *Client) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode Memory management request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://memoryd"+path, body)
	if err != nil {
		return fmt.Errorf("create Memory management request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.credential)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call Memory management: %w", err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var serviceErr memoryv1alpha1.ServiceError
		if err := decoder.Decode(&serviceErr); err == nil && serviceErr.Code != "" {
			return &serviceErr
		}
		return fmt.Errorf("Memory management returned HTTP %d", response.StatusCode)
	}
	if err := decoder.Decode(output); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("decode Memory management response: %w", err)
	}
	return nil
}
