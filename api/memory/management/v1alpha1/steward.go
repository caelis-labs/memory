package v1alpha1

import (
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// PutStewardProfileRequest creates one immutable Steward profile version.
type PutStewardProfileRequest struct {
	Profile stewardv1alpha1.ProfileSpec `json:"profile"`
}

// PutStewardProfileResponse reports the canonical stored profile.
type PutStewardProfileResponse struct {
	Profile stewardv1alpha1.Profile `json:"profile"`
	Created bool                    `json:"created"`
}

// BindStewardProfileRequest selects the profile snapshot captured by future
// Jobs in each listed Space.
type BindStewardProfileRequest struct {
	ProfileID stewardv1alpha1.ProfileID `json:"profile_id"`
	Version   uint64                    `json:"version"`
	SpaceIDs  []memoryv1alpha1.SpaceID  `json:"space_ids"`
}

// BindStewardProfileResponse reports how many Spaces were bound.
type BindStewardProfileResponse struct {
	Bound int `json:"bound"`
}

// DisableStewardRequest removes Steward bindings from the listed Spaces.
type DisableStewardRequest struct {
	SpaceIDs []memoryv1alpha1.SpaceID `json:"space_ids"`
}

// DisableStewardResponse reports removed bindings and canceled work.
type DisableStewardResponse struct {
	Disabled     int `json:"disabled"`
	CanceledJobs int `json:"canceled_jobs"`
}

// StewardBinding is the current future-Job policy for one Space.
type StewardBinding struct {
	SpaceID        memoryv1alpha1.SpaceID    `json:"space_id"`
	ProfileID      stewardv1alpha1.ProfileID `json:"profile_id"`
	ProfileVersion uint64                    `json:"profile_version"`
	BoundAt        time.Time                 `json:"bound_at"`
}

// StewardConfiguration is owner-visible profile and binding state. Provider
// credentials remain process configuration and are never returned here.
type StewardConfiguration struct {
	Profiles []stewardv1alpha1.Profile `json:"profiles"`
	Bindings []StewardBinding          `json:"bindings"`
}
