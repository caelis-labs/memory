package v1alpha1

import (
	"encoding/json"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestForgettingWireFixtures(t *testing.T) {
	tested := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "deletion response reports invalidation and cleanup",
			value: DeleteReceiptResponse{
				Deleted: true, ReceiptID: "receipt-a", TombstoneID: "tombstone-a",
				SessionCopyBoundary: "external producers own their copies",
				InvalidationVersion: 7, Cleanup: CleanupStatePending,
			},
			want: `{"deleted":true,"receipt_id":"receipt-a","tombstone_id":"tombstone-a","deduplicated_retry":false,"session_copy_boundary":"external producers own their copies","invalidation_version":7,"cleanup":"pending"}`,
		},
		{
			name:  "cleanup status response",
			value: CleanupStatusResponse{ReceiptID: "receipt-a", State: CleanupStateCompleted, InvalidationVersion: 7},
			want:  `{"receipt_id":"receipt-a","state":"completed","invalidation_version":7,"pending_barriers":0}`,
		},
		{
			name: "trace record reports a content-free skeleton",
			value: TraceRecordResponse{
				State: RecordStateForgotten,
				Record: &stewardv1alpha1.Record{
					RecordID: "record-a", SpaceID: "space-a", Kind: "claim",
					Status: stewardv1alpha1.RecordStatusInvalidated, CurrentRevision: 2,
					CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC(),
				},
				Revisions: []stewardv1alpha1.Revision{{
					RecordID: "record-a", Revision: 2, SpaceID: "space-a", Kind: "claim",
					Operation: stewardv1alpha1.OperationAdd,
					Evidence:  []stewardv1alpha1.Evidence{{ReceiptID: "receipt-a", SpaceID: "space-a"}},
					CreatedAt: time.Unix(0, 0).UTC(),
				}},
				InvalidationVersion: 3, Cleanup: CleanupStateCompleted,
			},
			want: `{"state":"forgotten","record":{"record_id":"record-a","space_id":"space-a","kind":"claim","status":"invalidated","current_revision":2,"created_at":"1970-01-01T00:00:00Z","updated_at":"1970-01-01T00:00:00Z"},"revisions":[{"record_id":"record-a","revision":2,"space_id":"space-a","kind":"claim","text":"","operation":"ADD","job_id":"","evidence":[{"receipt_id":"receipt-a","space_id":"space-a"}],"created_at":"1970-01-01T00:00:00Z"}],"invalidation_version":3,"cleanup":"completed"}`,
		},
	}
	for _, test := range tested {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.want {
				t.Fatalf("wire fixture = %s, want %s", encoded, test.want)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, secretField := range []string{"authorization", "credential", "capability"} {
				if _, exists := fields[secretField]; exists {
					t.Fatalf("governance response contains %q", secretField)
				}
			}
		})
	}
}

func TestGovernanceRecordRequestValidation(t *testing.T) {
	if err := (ListRecordsRequest{SpaceID: "space-a", Limit: 1}).Validate(); err != nil {
		t.Fatalf("valid listing rejected: %v", err)
	}
	for name, request := range map[string]ListRecordsRequest{
		"missing Space":  {Limit: 10},
		"zero limit":     {SpaceID: "space-a"},
		"unbounded":      {SpaceID: "space-a", Limit: 101},
		"invalid cursor": {SpaceID: "space-a", Limit: 10, Cursor: "\xff"},
	} {
		if err := request.Validate(); err == nil {
			t.Fatalf("%s listing was accepted", name)
		}
	}
	if err := (TraceRecordRequest{RecordID: "record-a"}).Validate(); err != nil {
		t.Fatalf("valid trace rejected: %v", err)
	}
	if err := (TraceRecordRequest{}).Validate(); err == nil {
		t.Fatal("empty Record trace was accepted")
	}
	if err := (CleanupStatusRequest{ReceiptID: "receipt-a"}).Validate(); err != nil {
		t.Fatalf("valid cleanup status rejected: %v", err)
	}
	if err := (CleanupStatusRequest{}).Validate(); err == nil {
		t.Fatal("empty cleanup status was accepted")
	}
}

func TestInspectionGovernanceDiagnosticsAreSecretFree(t *testing.T) {
	encoded, err := json.Marshal(Inspection{Governance: GovernanceDiagnostics{
		Barriers: 2, PendingCleanups: 1, CompletedCleanups: 1, LastInvalidationVersion: 4, ClearedRevisions: 3,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Governance GovernanceDiagnostics `json:"governance"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Governance.Barriers != 2 || decoded.Governance.LastInvalidationVersion != 4 {
		t.Fatalf("governance diagnostics round trip = %+v", decoded.Governance)
	}
	if memoryv1alpha1.ProtocolVersion == "" {
		t.Fatal("Memory protocol version is empty")
	}
}
