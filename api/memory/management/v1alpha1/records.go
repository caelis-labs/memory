package v1alpha1

import (
	"fmt"
	"unicode/utf8"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const (
	// maxRecordListLimit bounds one owner record listing page.
	maxRecordListLimit = 100
	// maxRecordCursorBytes bounds an opaque listing cursor.
	maxRecordCursorBytes = 512
)

// RecordState is the owner-visible governance state of one derived semantic
// Record. It separates a correction (history preserved, prior use invalidated)
// from a deletion (transitively derived contents cleared).
//
// Record listing, tracing, and cleanup polling are embedded-only owner
// capabilities: the request and response types below are versioned wire structs
// used directly through the in-process appliance.Management facade. They have no
// local transport route, SDK client method, or standalone command, and none is
// implied by these declarations.
type RecordState string

const (
	RecordStateActive      RecordState = "active"
	RecordStateInvalidated RecordState = "invalidated"
	// RecordStateForgetting means a forgetting barrier is committed and durable
	// but physical content cleansing has not completed. Contents are already
	// denied to every read path.
	RecordStateForgetting RecordState = "forgetting"
	RecordStateForgotten  RecordState = "forgotten"
)

// CleanupState reports whether managed history cleansing for one forgetting
// barrier has completed.
type CleanupState string

const (
	CleanupStateNone      CleanupState = "none"
	CleanupStatePending   CleanupState = "pending"
	CleanupStateCompleted CleanupState = "completed"
)

// RecordSummary is a semantic Record head plus its effective governance state.
// It carries no Revision text.
type RecordSummary struct {
	stewardv1alpha1.Record
	State RecordState `json:"state"`
}

// ListRecordsRequest pages owner-visible semantic Record heads within one
// exact Space. The cursor is opaque and stable across pages.
type ListRecordsRequest struct {
	SpaceID memoryv1alpha1.SpaceID `json:"space_id"`
	Limit   int                    `json:"limit"`
	Cursor  string                 `json:"cursor,omitempty"`
}

// ListRecordsResponse returns one page ordered by Record identity.
type ListRecordsResponse struct {
	Records    []RecordSummary `json:"records"`
	NextCursor string          `json:"next_cursor,omitempty"`
	Truncated  bool            `json:"truncated"`
}

// TraceRecordRequest asks for one Record head and its revision audit.
type TraceRecordRequest struct {
	RecordID stewardv1alpha1.RecordID `json:"record_id"`
}

// TraceRecordResponse reports the effective governance state. A forgotten or
// forgetting Record returns only content-free Revision skeletons and Evidence
// receipt identities; it never returns cleared or pending derived payload.
type TraceRecordResponse struct {
	State     RecordState                `json:"state"`
	Record    *stewardv1alpha1.Record    `json:"record,omitempty"`
	Revisions []stewardv1alpha1.Revision `json:"revisions"`
	// InvalidationVersion is the highest deletion barrier sequence that affects
	// this Record. Cleanup reports whether its content cleansing completed.
	InvalidationVersion uint64       `json:"invalidation_version,omitempty"`
	Cleanup             CleanupState `json:"cleanup"`
}

// CleanupStatusRequest polls managed history cleansing for one forgotten
// receipt. Receipt identity is globally unique, so no Space is required.
type CleanupStatusRequest struct {
	ReceiptID memoryv1alpha1.ReceiptID `json:"receipt_id"`
}

// CleanupStatusResponse reports the logical invalidation sequence and whether
// physical content cleansing has completed. It is safe to poll after an
// unreported deletion outcome.
type CleanupStatusResponse struct {
	ReceiptID           memoryv1alpha1.ReceiptID `json:"receipt_id"`
	State               CleanupState             `json:"state"`
	InvalidationVersion uint64                   `json:"invalidation_version,omitempty"`
	PendingBarriers     int64                    `json:"pending_barriers"`
	StartedAt           string                   `json:"started_at,omitempty"`
	CompletedAt         string                   `json:"completed_at,omitempty"`
}

func (r ListRecordsRequest) Validate() error {
	if r.SpaceID == "" {
		return fmt.Errorf("space_id is required")
	}
	if r.Limit < 1 || r.Limit > maxRecordListLimit {
		return fmt.Errorf("limit must be within 1..%d", maxRecordListLimit)
	}
	if !utf8.ValidString(r.Cursor) || len(r.Cursor) > maxRecordCursorBytes {
		return fmt.Errorf("cursor must be valid UTF-8 and at most %d bytes", maxRecordCursorBytes)
	}
	return nil
}

func (r TraceRecordRequest) Validate() error {
	if r.RecordID == "" {
		return fmt.Errorf("record_id is required")
	}
	return nil
}

func (r CleanupStatusRequest) Validate() error {
	if r.ReceiptID == "" {
		return fmt.Errorf("receipt_id is required")
	}
	return nil
}
