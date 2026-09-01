//go:build windows

package v1alpha1

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// DefaultLocalEndpoint derives a stable named pipe without exposing the data
// directory in the global pipe namespace.
func DefaultLocalEndpoint(dataDir string) LocalEndpoint {
	canonical, err := filepath.Abs(dataDir)
	if err != nil {
		canonical = filepath.Clean(dataDir)
	}
	digest := sha256.Sum256([]byte(strings.ToLower(canonical)))
	return LocalEndpoint{
		Network: LocalNetworkNamedPipe,
		Address: `\\.\pipe\caelis-memory-` + hex.EncodeToString(digest[:8]),
	}
}
