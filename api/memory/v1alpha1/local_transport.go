package v1alpha1

import "fmt"

const (
	// LocalTransportProtocol identifies the versioned owner-local HTTP binding.
	LocalTransportProtocol = "memory.local.v1alpha1"
	CoreProfile            = "memory.core.v1alpha1"

	LocalPathHealth        = "/healthz"
	LocalPathReady         = "/readyz"
	LocalPathCompatibility = "/memory.v1alpha1/compatibility"
	LocalPathIssue         = "/memory.v1alpha1/capabilities/issue"
	LocalPathRemember      = "/memory.v1alpha1/remember"
	LocalPathRecall        = "/memory.v1alpha1/recall"
	LocalPathReceiptStatus = "/memory.v1alpha1/receipt-status"
	// LocalSocketFilename is the fixed socket name below a host-selected,
	// owner-only appliance data directory.
	LocalSocketFilename = "memoryd.sock"

	LocalHeaderActor    = "X-Memory-Actor"
	LocalHeaderAudience = "X-Memory-Audience"
)

// LocalNetwork identifies the OS-local byte-stream transport below the HTTP
// application protocol.
type LocalNetwork string

const (
	LocalNetworkUnix      LocalNetwork = "unix"
	LocalNetworkNamedPipe LocalNetwork = "npipe"
)

// LocalEndpoint is transport-neutral endpoint configuration for SDK clients
// and memoryd composition. It contains no authority.
type LocalEndpoint struct {
	Network LocalNetwork `json:"network"`
	Address string       `json:"address"`
}

// Validate rejects incomplete or unknown endpoint kinds.
func (e LocalEndpoint) Validate() error {
	if e.Address == "" {
		return fmt.Errorf("local endpoint address is required")
	}
	switch e.Network {
	case LocalNetworkUnix, LocalNetworkNamedPipe:
		return nil
	default:
		return fmt.Errorf("unsupported local endpoint network %q", e.Network)
	}
}
