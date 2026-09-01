//go:build !windows

package localtransport

import (
	"fmt"
	"net"
	"os"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// Listen creates the owner-only local listener for this platform.
func Listen(endpoint v1alpha1.LocalEndpoint) (net.Listener, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	if endpoint.Network != v1alpha1.LocalNetworkUnix {
		return nil, fmt.Errorf("local endpoint %q is unavailable on this platform", endpoint.Network)
	}
	return ListenUnix(endpoint.Address)
}

// ListenUnix replaces only a stale socket node, then creates an owner-only
// local listener. A regular file is never removed.
func ListenUnix(socketPath string) (net.Listener, error) {
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("local transport path exists and is not a socket")
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale local socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect local socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on local socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("secure local socket: %w", err)
	}
	return &socketListener{Listener: listener, path: socketPath}, nil
}

type socketListener struct {
	net.Listener
	path string
}

func (l *socketListener) Close() error {
	err := l.Listener.Close()
	removeErr := os.Remove(l.path)
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}
	if err != nil {
		return err
	}
	return removeErr
}
