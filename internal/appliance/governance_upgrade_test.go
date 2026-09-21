package appliance

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

//go:embed testdata/v0.6.0-governance/memory.db testdata/v0.6.0-governance/*.json testdata/v0.6.0-governance/*.txt
var governanceUpgradeFixture embed.FS

func TestReleasedGovernanceProcessingUpgrade(t *testing.T) {
	const prefix = "testdata/v0.6.0-governance/"
	raw, err := governanceUpgradeFixture.ReadFile(prefix + "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SourceTag     string                   `json:"source_tag"`
		SourceCommit  string                   `json:"source_commit"`
		SchemaVersion int                      `json:"schema_version"`
		Clock         time.Time                `json:"clock"`
		Authorization memory.CallAuthorization `json:"test_only_authorization"`
		FilesSHA256   map[string]string        `json:"files_sha256"`
		Receipts      []struct {
			Action    string                      `json:"action"`
			Request   facts.SubmitEvidenceRequest `json:"request"`
			ReceiptID memory.ReceiptID            `json:"receipt_id"`
		} `json:"cancelled_receipts"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SourceTag != "v0.6.0" || manifest.SourceCommit != "f17b0293597dcdf9fad4fcba9ea19e20d1d91674" || manifest.SchemaVersion != 2 || len(manifest.Receipts) != 2 {
		t.Fatal("unexpected fixture provenance")
	}
	dir := t.TempDir()
	for _, name := range []string{"memory.db", "management.token.txt", "steward-worker.token.txt"} {
		raw, err := governanceUpgradeFixture.ReadFile(prefix + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != manifest.FilesSHA256[name] {
			t.Fatalf("fixture digest mismatch: %s", name)
		}
		if err := os.WriteFile(filepath.Join(dir, strings.TrimSuffix(name, ".txt")), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		s, err := Open(t.Context(), Options{DataDir: dir, Clock: func() time.Time { return manifest.Clock }})
		if err != nil {
			t.Fatal(err)
		}
		for _, receipt := range manifest.Receipts {
			reason := "receipt_deleted"
			if receipt.Action == "correct" {
				reason = "receipt_corrected"
			}
			status, err := s.GetReceiptStatus(t.Context(), manifest.Authorization, memory.GetReceiptStatusRequest{ReceiptID: receipt.ReceiptID})
			if err != nil || status.State != memory.ProcessingStateFailed || string(status.TerminalErrorCode) != reason {
				t.Fatalf("open %d repaired status=%+v %v", i, status, err)
			}
			retry, err := s.SubmitEvidence(t.Context(), manifest.Authorization, receipt.Request)
			if err != nil || retry.Organization != facts.OrganizationFailed || retry.ReceiptID != receipt.ReceiptID || !retry.Deduplicated {
				t.Fatalf("evidence retry=%+v %v", retry, err)
			}
			if state := governanceText(t, s, `SELECT state FROM steward_jobs WHERE receipt_id=?`, receipt.ReceiptID); state != "failed" {
				t.Fatalf("cancelled job resurrected: %s", state)
			}
			if text := governanceText(t, s, `SELECT text FROM receipts WHERE receipt_id=?`, receipt.ReceiptID); text != receipt.Request.Text {
				t.Fatal("immutable receipt changed")
			}
		}
		if marker := governanceText(t, s, `SELECT value FROM metadata WHERE key=?`, governanceProcessingMigration); marker != "1" {
			t.Fatal("missing repair marker")
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
