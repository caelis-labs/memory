// Package local implements the versioned local memoryd transport.
package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const maxResponseBytes = 1 << 20

// Client carries memory.v1alpha1 over owner-local HTTP on a Unix socket.
type Client struct {
	http *http.Client
}

// NewClient binds a client to one memoryd Unix socket.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{http: &http.Client{Transport: transport}}
}

// Remember implements the data-plane Remember operation.
func (c *Client) Remember(ctx context.Context, auth v1alpha1.CallAuthorization, request v1alpha1.RememberRequest) (v1alpha1.RememberResponse, error) {
	var response v1alpha1.RememberResponse
	if err := c.do(ctx, v1alpha1.LocalPathRemember, auth, request, &response); err != nil {
		var serviceErr *v1alpha1.ServiceError
		if errors.As(err, &serviceErr) {
			return v1alpha1.RememberResponse{}, err
		}
		return v1alpha1.RememberResponse{}, &v1alpha1.ServiceError{
			Code:      v1alpha1.ErrorCodeUnknownOutcome,
			Message:   "Remember transport outcome is unknown; retry the same effect identity",
			Retryable: true,
			RequestID: "local-transport",
		}
	}
	return response, nil
}

// Recall implements the data-plane Recall operation.
func (c *Client) Recall(ctx context.Context, auth v1alpha1.CallAuthorization, request v1alpha1.RecallRequest) (v1alpha1.RecallResponse, error) {
	var response v1alpha1.RecallResponse
	if err := c.do(ctx, v1alpha1.LocalPathRecall, auth, request, &response); err != nil {
		return v1alpha1.RecallResponse{}, transportServiceError(err)
	}
	return response, nil
}

// GetReceiptStatus implements the data-plane receipt-status operation.
func (c *Client) GetReceiptStatus(ctx context.Context, auth v1alpha1.CallAuthorization, request v1alpha1.GetReceiptStatusRequest) (v1alpha1.ReceiptStatus, error) {
	var response v1alpha1.ReceiptStatus
	if err := c.do(ctx, v1alpha1.LocalPathReceiptStatus, auth, request, &response); err != nil {
		return v1alpha1.ReceiptStatus{}, transportServiceError(err)
	}
	return response, nil
}

// Health verifies process liveness and the local transport version.
func (c *Client) Health(ctx context.Context) error {
	var response struct {
		Protocol string `json:"protocol"`
		Status   string `json:"status"`
	}
	if err := c.get(ctx, v1alpha1.LocalPathHealth, &response); err != nil {
		return err
	}
	if response.Protocol != v1alpha1.LocalTransportProtocol || response.Status != "ok" {
		return fmt.Errorf("incompatible memoryd health response")
	}
	return nil
}

// Ready verifies that memoryd can reach its durable authority.
func (c *Client) Ready(ctx context.Context) error {
	var response struct {
		Protocol string `json:"protocol"`
		Status   string `json:"status"`
	}
	if err := c.get(ctx, v1alpha1.LocalPathReady, &response); err != nil {
		return err
	}
	if response.Protocol != v1alpha1.LocalTransportProtocol || response.Status != "ready" {
		return fmt.Errorf("memoryd is not ready")
	}
	return nil
}

// CloseIdleConnections closes pooled local connections.
func (c *Client) CloseIdleConnections() {
	c.http.CloseIdleConnections()
}

func (c *Client) do(ctx context.Context, path string, auth v1alpha1.CallAuthorization, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://memoryd"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+string(auth.Capability))
	request.Header.Set(v1alpha1.LocalHeaderActor, auth.ActorRef)
	request.Header.Set(v1alpha1.LocalHeaderAudience, string(auth.Audience))
	return c.send(request, output)
}

func (c *Client) get(ctx context.Context, path string, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://memoryd"+path, nil)
	if err != nil {
		return err
	}
	return c.send(request, output)
}

func (c *Client) send(request *http.Request, output any) error {
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var serviceErr v1alpha1.ServiceError
		if err := decoder.Decode(&serviceErr); err != nil {
			return fmt.Errorf("memoryd returned HTTP %d", response.StatusCode)
		}
		return &serviceErr
	}
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode memoryd response: %w", err)
	}
	return nil
}

func transportServiceError(err error) error {
	var serviceErr *v1alpha1.ServiceError
	if errors.As(err, &serviceErr) {
		return err
	}
	code := v1alpha1.ErrorCodeUnavailable
	message := "memoryd local transport is unavailable"
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		code = v1alpha1.ErrorCodeDeadline
		message = "memoryd local transport deadline exceeded"
	}
	return &v1alpha1.ServiceError{
		Code:      code,
		Message:   message,
		Retryable: true,
		RequestID: "local-transport-" + time.Now().UTC().Format("150405.000000000"),
	}
}
