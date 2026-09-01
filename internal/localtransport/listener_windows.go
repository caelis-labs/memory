//go:build windows

package localtransport

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"golang.org/x/sys/windows"
)

// Listen creates an owner-restricted named-pipe listener on Windows.
func Listen(endpoint v1alpha1.LocalEndpoint) (net.Listener, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	if endpoint.Network != v1alpha1.LocalNetworkNamedPipe {
		return nil, fmt.Errorf("local endpoint %q is unavailable on this platform", endpoint.Network)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("resolve local transport owner: %w", err)
	}
	securityDescriptor := "D:P(A;;GA;;;" + user.User.Sid.String() + ")"
	listener, err := winio.ListenPipe(endpoint.Address, &winio.PipeConfig{
		SecurityDescriptor: securityDescriptor,
		InputBufferSize:    64 << 10,
		OutputBufferSize:   64 << 10,
	})
	if err != nil {
		return nil, fmt.Errorf("listen on local named pipe: %w", err)
	}
	return listener, nil
}

// ListenUnix exists only for source compatibility and always fails on Windows.
func ListenUnix(string) (net.Listener, error) {
	return nil, fmt.Errorf("Unix sockets are not the Memory local transport on Windows")
}
