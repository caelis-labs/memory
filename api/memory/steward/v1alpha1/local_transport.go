package v1alpha1

const (
	// LocalPathClaim leases one available Steward Job to an authenticated Worker.
	LocalPathClaim = "/memory.steward.v1alpha1/jobs/claim"
	// LocalPathApply validates and applies one Worker proposal.
	LocalPathApply = "/memory.steward.v1alpha1/jobs/apply"
	// LocalPathFail records one classified Worker failure.
	LocalPathFail = "/memory.steward.v1alpha1/jobs/fail"
)
