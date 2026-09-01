// Package memory provides the host-side Go SDK for memory.v1alpha1.
package memory

import (
	"context"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// CapabilitySource supplies authority out of band for one operation. A
// production implementation may renew a capability before returning it.
type CapabilitySource interface {
	Authorization(context.Context, v1alpha1.Operation) (v1alpha1.CallAuthorization, error)
}

// StaticCapabilitySource is intended for tests and bounded reference use.
type StaticCapabilitySource struct {
	AuthorizationValue v1alpha1.CallAuthorization
}

func (s StaticCapabilitySource) Authorization(context.Context, v1alpha1.Operation) (v1alpha1.CallAuthorization, error) {
	return s.AuthorizationValue, nil
}

// Client binds hidden host context to the narrow Agent operations.
type Client struct {
	service      v1alpha1.DataPlane
	capabilities CapabilitySource
	source       v1alpha1.SourceContext
	budget       v1alpha1.RecallBudget
}

// NewClient constructs a bound client. Source context and budget are host
// controls and are never accepted from Agent tool input.
func NewClient(
	service v1alpha1.DataPlane,
	capabilities CapabilitySource,
	source v1alpha1.SourceContext,
	budget v1alpha1.RecallBudget,
) *Client {
	return &Client{
		service:      service,
		capabilities: capabilities,
		source:       source,
		budget:       budget,
	}
}

// Remember submits fact text with a host-generated stable idempotency key.
func (c *Client) Remember(
	ctx context.Context,
	text string,
	idempotencyKey string,
	occurredAt *time.Time,
) (v1alpha1.RememberResponse, error) {
	auth, err := c.capabilities.Authorization(ctx, v1alpha1.OperationRemember)
	if err != nil {
		return v1alpha1.RememberResponse{}, err
	}
	return c.service.Remember(ctx, auth, v1alpha1.RememberRequest{
		Text:           text,
		SourceContext:  c.source,
		OccurredAt:     occurredAt,
		IdempotencyKey: idempotencyKey,
	})
}

// Recall queries memory with host-selected bounds and an optional causal cursor.
func (c *Client) Recall(
	ctx context.Context,
	query string,
	minConsistencyToken v1alpha1.ConsistencyToken,
) (v1alpha1.RecallResponse, error) {
	auth, err := c.capabilities.Authorization(ctx, v1alpha1.OperationRecall)
	if err != nil {
		return v1alpha1.RecallResponse{}, err
	}
	return c.service.Recall(ctx, auth, v1alpha1.RecallRequest{
		Query:               query,
		SourceContext:       c.source,
		MinConsistencyToken: minConsistencyToken,
		Budget:              c.budget,
	})
}

// GetReceiptStatus returns authorized processing state for one receipt.
func (c *Client) GetReceiptStatus(
	ctx context.Context,
	receiptID v1alpha1.ReceiptID,
) (v1alpha1.ReceiptStatus, error) {
	auth, err := c.capabilities.Authorization(ctx, v1alpha1.OperationReceiptStatus)
	if err != nil {
		return v1alpha1.ReceiptStatus{}, err
	}
	return c.service.GetReceiptStatus(ctx, auth, v1alpha1.GetReceiptStatusRequest{
		ReceiptID: receiptID,
	})
}
