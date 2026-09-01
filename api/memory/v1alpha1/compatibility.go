package v1alpha1

import "time"

// CompatibilityRequest is the exact Core Profile a host intends to use.
// memoryd rejects the request rather than negotiating a weaker profile.
type CompatibilityRequest struct {
	Protocol    string `json:"protocol"`
	APIVersion  string `json:"api_version"`
	CoreProfile string `json:"core_profile"`
}

// CompatibilityResponse identifies the running service after a successful
// exact-profile handshake. Artifact integrity is verified separately against
// the sidecar manifest before process launch.
type CompatibilityResponse struct {
	Protocol       string `json:"protocol"`
	APIVersion     string `json:"api_version"`
	CoreProfile    string `json:"core_profile"`
	ServiceVersion string `json:"service_version"`
	BuildRevision  string `json:"build_revision"`
	SchemaVersion  int    `json:"schema_version"`
}

// CapabilityIssueRequest asks the issuer plane for temporary Runtime
// authority. The issuer credential is carried as a bearer outside this body.
type CapabilityIssueRequest struct {
	PrincipalRef string      `json:"principal_ref"`
	GrantRef     GrantID     `json:"grant_ref"`
	ActorRef     string      `json:"actor_ref"`
	Audience     Audience    `json:"audience"`
	Operations   []Operation `json:"operations"`
	TTLSeconds   int64       `json:"ttl_seconds"`
}

// RuntimeCapability is the only authority returned to a Runtime. Hosts keep
// the token outside model input, Session history, and ordinary diagnostics.
type RuntimeCapability struct {
	Token     CapabilityToken `json:"token"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// CurrentCompatibilityRequest returns the exact profile implemented by this
// API package.
func CurrentCompatibilityRequest() CompatibilityRequest {
	return CompatibilityRequest{
		Protocol:    LocalTransportProtocol,
		APIVersion:  ProtocolVersion,
		CoreProfile: CoreProfile,
	}
}
