package v1alpha1

import "time"

// StorageDiagnostics contains byte counts only; it never includes paths or
// content names supplied by receipts.
type StorageDiagnostics struct {
	DatabaseBytes   uint64 `json:"database_bytes"`
	WALBytes        uint64 `json:"wal_bytes"`
	SHMBytes        uint64 `json:"shm_bytes"`
	RollbackBytes   uint64 `json:"rollback_bytes"`
	DataBytes       uint64 `json:"data_bytes"`
	FilesystemBytes uint64 `json:"filesystem_bytes"`
	AvailableBytes  uint64 `json:"available_bytes"`
}

// ReceiptDiagnostics summarizes canonical and processing state without text or
// evidence identifiers.
type ReceiptDiagnostics struct {
	Stored           int64      `json:"stored"`
	Active           int64      `json:"active"`
	Corrected        int64      `json:"corrected"`
	Deleted          int64      `json:"deleted"`
	Accepted         int64      `json:"accepted"`
	Processing       int64      `json:"processing"`
	Organized        int64      `json:"organized"`
	Failed           int64      `json:"failed"`
	OldestReceivedAt *time.Time `json:"oldest_received_at,omitempty"`
	NewestReceivedAt *time.Time `json:"newest_received_at,omitempty"`
}

// ProjectionDiagnostics compares disposable lexical entries with their
// canonical receipt authority. Status is ok, drift, or unavailable.
type ProjectionDiagnostics struct {
	Spaces          int64      `json:"spaces"`
	Entries         int64      `json:"entries"`
	ExpectedEntries int64      `json:"expected_entries"`
	Drift           int64      `json:"drift"`
	Healthy         bool       `json:"healthy"`
	Status          string     `json:"status"`
	LastRebuiltAt   *time.Time `json:"last_rebuilt_at,omitempty"`
}

// CapabilityDiagnostics contains authority counts without bearer digests or
// principal names.
type CapabilityDiagnostics struct {
	Stored           int64 `json:"stored"`
	Active           int64 `json:"active"`
	Inactive         int64 `json:"inactive"`
	RevokedGrants    int64 `json:"revoked_grants"`
	IssuerPrincipals int64 `json:"issuer_principals"`
}
