package v1alpha1

const (
	// LocalTransportProtocol identifies the versioned Unix-socket HTTP binding.
	LocalTransportProtocol = "memory.local.v1alpha1"
	CoreProfile            = "memory.core.v1alpha1"

	LocalPathHealth        = "/healthz"
	LocalPathReady         = "/readyz"
	LocalPathCompatibility = "/memory.v1alpha1/compatibility"
	LocalPathIssue         = "/memory.v1alpha1/capabilities/issue"
	LocalPathRemember      = "/memory.v1alpha1/remember"
	LocalPathRecall        = "/memory.v1alpha1/recall"
	LocalPathReceiptStatus = "/memory.v1alpha1/receipt-status"

	LocalHeaderActor    = "X-Memory-Actor"
	LocalHeaderAudience = "X-Memory-Audience"
)
