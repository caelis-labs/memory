//go:build !windows

package v1alpha1

import "path/filepath"

// DefaultLocalEndpoint derives the fixed Unix socket below one data directory.
func DefaultLocalEndpoint(dataDir string) LocalEndpoint {
	return LocalEndpoint{Network: LocalNetworkUnix, Address: filepath.Join(dataDir, LocalSocketFilename)}
}
