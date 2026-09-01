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
	"net/http"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/sdk/go/memory/internal/localhttp"
)

const maxResponseBytes = 8 << 20

// Client binds one management bearer to one owner-only local Socket.
type Client struct {
	http       *http.Client
	credential string
}

func NewClient(socketPath, credential string) *Client {
	return NewClientForEndpoint(memoryv1alpha1.LocalEndpoint{Network: memoryv1alpha1.LocalNetworkUnix, Address: socketPath}, credential)
}

// NewClientForEndpoint binds a management bearer to one OS-local endpoint.
func NewClientForEndpoint(endpoint memoryv1alpha1.LocalEndpoint, credential string) *Client {
	return &Client{http: localhttp.NewClient(endpoint), credential: credential}
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

// Export writes the sensitive plaintext NDJSON export to output. The caller is
// responsible for an owner-only destination and removal of partial output.
func (c *Client) Export(ctx context.Context, request managementv1alpha1.ExportRequest, output io.Writer) error {
	return c.doStream(ctx, managementv1alpha1.LocalPathExport, request, output)
}

// Backup writes a consistent plaintext SQLite snapshot received over the
// owner-only Socket. Callers must encrypt it before durable storage.
func (c *Client) Backup(ctx context.Context, output io.Writer) error {
	return c.doStream(ctx, managementv1alpha1.LocalPathBackup, managementv1alpha1.BackupRequest{}, output)
}

func (c *Client) CommitRestore(ctx context.Context) error {
	var response managementv1alpha1.CommitRestoreResponse
	return c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathCommitRestore, struct{}{}, &response)
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

func (c *Client) RotateManagementCredential(ctx context.Context) error {
	var response managementv1alpha1.RotateManagementCredentialResponse
	return c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathRotateManagement, struct{}{}, &response)
}

// PutStewardProfile creates or resolves one immutable profile version.
func (c *Client) PutStewardProfile(ctx context.Context, request managementv1alpha1.PutStewardProfileRequest) (managementv1alpha1.PutStewardProfileResponse, error) {
	var response managementv1alpha1.PutStewardProfileResponse
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathPutStewardProfile, request, &response)
	return response, err
}

// BindStewardProfile selects a profile for future Jobs in the listed Spaces.
func (c *Client) BindStewardProfile(ctx context.Context, request managementv1alpha1.BindStewardProfileRequest) (managementv1alpha1.BindStewardProfileResponse, error) {
	var response managementv1alpha1.BindStewardProfileResponse
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathBindStewardProfile, request, &response)
	return response, err
}

// DisableSteward stops future work and cancels outstanding Jobs for Spaces.
func (c *Client) DisableSteward(ctx context.Context, request managementv1alpha1.DisableStewardRequest) (managementv1alpha1.DisableStewardResponse, error) {
	var response managementv1alpha1.DisableStewardResponse
	err := c.do(ctx, http.MethodPost, managementv1alpha1.LocalPathDisableSteward, request, &response)
	return response, err
}

// StewardConfiguration returns owner-visible profiles and Space bindings.
func (c *Client) StewardConfiguration(ctx context.Context) (managementv1alpha1.StewardConfiguration, error) {
	var response managementv1alpha1.StewardConfiguration
	err := c.do(ctx, http.MethodGet, managementv1alpha1.LocalPathStewardConfiguration, nil, &response)
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

func (c *Client) doStream(ctx context.Context, path string, input any, output io.Writer) error {
	if output == nil {
		return fmt.Errorf("Memory management stream output is required")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode Memory management request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://memoryd"+path, bytes.NewReader(encoded))
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
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var serviceErr memoryv1alpha1.ServiceError
		if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&serviceErr); err == nil && serviceErr.Code != "" {
			return &serviceErr
		}
		return fmt.Errorf("Memory management returned HTTP %d", response.StatusCode)
	}
	if _, err := io.Copy(output, response.Body); err != nil {
		return fmt.Errorf("copy Memory management response: %w", err)
	}
	return nil
}
