package local

import (
	"context"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// IssuerClient exchanges an owner-provided principal credential for temporary
// Runtime capabilities. It has no management or data-plane authority.
type IssuerClient struct {
	client     *Client
	credential string
}

// NewIssuerClient binds one principal issuer credential to a local memoryd.
func NewIssuerClient(socketPath, credential string) *IssuerClient {
	return &IssuerClient{client: NewClient(socketPath), credential: credential}
}

// IssueCapability issues or renews temporary authority for one immutable
// Runtime binding. Renewal is a fresh call with the same binding references.
func (c *IssuerClient) IssueCapability(ctx context.Context, request v1alpha1.CapabilityIssueRequest) (v1alpha1.RuntimeCapability, error) {
	var response v1alpha1.RuntimeCapability
	auth := v1alpha1.CallAuthorization{Capability: v1alpha1.CapabilityToken(c.credential)}
	if err := c.client.do(ctx, v1alpha1.LocalPathIssue, auth, request, &response); err != nil {
		return v1alpha1.RuntimeCapability{}, transportServiceError(err)
	}
	return response, nil
}

// CloseIdleConnections closes pooled local connections.
func (c *IssuerClient) CloseIdleConnections() {
	c.client.CloseIdleConnections()
}
