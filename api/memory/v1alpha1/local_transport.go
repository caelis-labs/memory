package v1alpha1

const (
	// LocalTransportProtocol identifies the M1 Unix-socket HTTP binding.
	LocalTransportProtocol = "memory.local.v1alpha1"

	LocalPathHealth        = "/healthz"
	LocalPathReady         = "/readyz"
	LocalPathRemember      = "/memory.v1alpha1/remember"
	LocalPathRecall        = "/memory.v1alpha1/recall"
	LocalPathReceiptStatus = "/memory.v1alpha1/receipt-status"

	LocalHeaderActor    = "X-Memory-Actor"
	LocalHeaderAudience = "X-Memory-Audience"
)
