// Package buildinfo owns immutable identity injected into the memoryd binary.
package buildinfo

var (
	// ServiceVersion is set by the sidecar build through -ldflags -X.
	ServiceVersion = "devel"
	// BuildRevision is the exact source revision used by the sidecar build.
	BuildRevision = "unknown"
)
