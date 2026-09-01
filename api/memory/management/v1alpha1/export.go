package v1alpha1

import (
	"time"

	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const ExportFormat = "memory.export.v1"

type ExportRequest struct {
	SpaceID          memoryv1alpha1.SpaceID `json:"space_id,omitempty"`
	IncludeCorrected bool                   `json:"include_corrected,omitempty"`
	IncludeDeleted   bool                   `json:"include_deleted,omitempty"`
}

type ExportRecordKind string

const (
	ExportRecordHeader    ExportRecordKind = "header"
	ExportRecordReceipt   ExportRecordKind = "receipt"
	ExportRecordTombstone ExportRecordKind = "tombstone"
)

// ExportRecord is one line in the versioned NDJSON management export. Exactly
// one payload matching Kind is present after the header.
type ExportRecord struct {
	Kind       ExportRecordKind `json:"kind"`
	Format     string           `json:"format,omitempty"`
	Generation string           `json:"generation,omitempty"`
	CreatedAt  *time.Time       `json:"created_at,omitempty"`
	Receipt    *Receipt         `json:"receipt,omitempty"`
	Tombstone  *Tombstone       `json:"tombstone,omitempty"`
}

// BackupRequest is intentionally empty in v1. The response is a consistent
// SQLite snapshot over the owner-only local transport; clients encrypt it
// before durable output.
type BackupRequest struct{}
