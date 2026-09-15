package appliance

import facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"

// Facts returns the versioned capability-authorized current/historical fact
// reader. Recall authority suffices; this plane grants no write or governance
// permission. Old DataPlane().Recall remains evidence search.
func (r *Runtime) Facts() facts.Reader {
	if r == nil || r.closed.Load() {
		return nil
	}
	return factsReaderPlane{store: r.store}
}

// Evidence returns the trusted Host ingestion and policy plane. It is not a
// model-facing tool surface. Source roles and structured mutations must come
// from trusted host/user interactions, never directly from generated proposals.
// SubmitEvidence also validates a Remember capability; policy is owner-only.
func (r *Runtime) Evidence() facts.EvidenceService {
	if r == nil || r.closed.Load() {
		return nil
	}
	return evidencePlane{store: r.store}
}
