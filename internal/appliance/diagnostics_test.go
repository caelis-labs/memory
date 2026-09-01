package appliance

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestInspectionReportsBoundedOperationalDiagnosticsWithoutSecrets(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	original, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "diagnostic secret receipt alpha", IdempotencyKey: "diagnostic-alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CorrectReceipt(t.Context(), managementv1alpha1.CorrectReceiptRequest{
		ReceiptID: original.ReceiptID, ReplacementText: "diagnostic replacement beta",
		Reason: "verified", IdempotencyKey: "diagnostic-correction",
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "diagnostic deleted gamma", IdempotencyKey: "diagnostic-gamma",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: deleted.ReceiptID, Reason: "verified erasure", IdempotencyKey: "diagnostic-deletion",
	}); err != nil {
		t.Fatal(err)
	}

	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Storage.DatabaseBytes == 0 || inspection.Storage.DataBytes == 0 ||
		inspection.Storage.FilesystemBytes == 0 || inspection.Storage.AvailableBytes == 0 {
		t.Fatalf("storage diagnostics = %+v", inspection.Storage)
	}
	if inspection.Receipts.Stored != 2 || inspection.Receipts.Active != 1 ||
		inspection.Receipts.Corrected != 1 || inspection.Receipts.Deleted != 1 ||
		inspection.Receipts.Accepted != 2 {
		t.Fatalf("receipt diagnostics = %+v", inspection.Receipts)
	}
	if inspection.Receipts.OldestReceivedAt == nil || inspection.Receipts.NewestReceivedAt == nil {
		t.Fatalf("receipt time range = %+v", inspection.Receipts)
	}
	if !inspection.Projection.Healthy || inspection.Projection.Status != "ok" ||
		inspection.Projection.Drift != 0 || inspection.Projection.Entries != 2 ||
		inspection.Projection.Spaces != 3 {
		t.Fatalf("projection diagnostics = %+v", inspection.Projection)
	}
	if inspection.Capabilities.Stored != 1 || inspection.Capabilities.Active != 1 ||
		inspection.Capabilities.Inactive != 0 || inspection.Capabilities.IssuerPrincipals != 4 {
		t.Fatalf("capability diagnostics = %+v", inspection.Capabilities)
	}

	encoded, err := json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"diagnostic secret receipt alpha",
		"diagnostic replacement beta",
		"diagnostic deleted gamma",
		strings.TrimSpace(string(credential)),
		string(auth.Capability),
	} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("inspection exposed secret %q: %s", secret, encoded)
		}
	}
}

func TestInspectionDetectsProjectionDriftAndRebuildRecordsRecovery(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	remembered, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "projection drift sentinel", IdempotencyKey: "projection-drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	tableName := spaceIndexTable("space-bot-a")
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM `+tableName+` WHERE receipt_id = ?`, remembered.ReceiptID); err != nil {
		t.Fatal(err)
	}
	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Projection.Healthy || inspection.Projection.Status != "drift" || inspection.Projection.Drift != -1 {
		t.Fatalf("drift diagnostics = %+v", inspection.Projection)
	}
	if err := store.RebuildFTS(t.Context()); err != nil {
		t.Fatal(err)
	}
	inspection, err = store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Projection.Healthy || inspection.Projection.Drift != 0 || inspection.Projection.LastRebuiltAt == nil {
		t.Fatalf("rebuilt diagnostics = %+v", inspection.Projection)
	}
}

func TestInspectionDiagnosesUnavailableProjectionWithoutLosingCanonicalTopology(t *testing.T) {
	store, _ := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.db.ExecContext(t.Context(), `DROP TABLE `+spaceIndexTable("space-bot-a")); err != nil {
		t.Fatal(err)
	}
	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Projection.Healthy || inspection.Projection.Status != "unavailable" {
		t.Fatalf("projection diagnostics = %+v", inspection.Projection)
	}
	if len(inspection.Spaces) != 3 || inspection.Counts["spaces"] != 3 {
		t.Fatalf("canonical topology was lost: %+v", inspection)
	}
}
