package localhttp

import (
	"context"
	"net"
	"net/http"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// NewClient creates an HTTP client over one validated OS-local endpoint.
func NewClient(endpoint v1alpha1.LocalEndpoint) *http.Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if err := endpoint.Validate(); err != nil {
			return nil, err
		}
		return dialContext(ctx, endpoint)
	}}
	return &http.Client{Transport: transport}
}
