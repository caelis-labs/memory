//go:build !windows

package localhttp

import (
	"context"
	"fmt"
	"net"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func dialContext(ctx context.Context, endpoint v1alpha1.LocalEndpoint) (net.Conn, error) {
	if endpoint.Network != v1alpha1.LocalNetworkUnix {
		return nil, fmt.Errorf("local endpoint %q is unavailable on this platform", endpoint.Network)
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", endpoint.Address)
}
