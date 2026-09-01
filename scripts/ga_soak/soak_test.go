package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSmallSoakCoversRestartReindexBackupAndRestore(t *testing.T) {
	root := t.TempDir()
	report, err := executeSoak(context.Background(), filepath.Join(root, "source"), filepath.Join(root, "restored"), options{
		spaces: 3, receipts: 30, records: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.ReceiptStatusReads != 30 || report.RecallSamples != 6 ||
		report.ProvenanceChecks != 3 || report.PrivateLeakChecks != 3 || report.PrivateLeaks != 0 ||
		report.RestoredReceiptStatusReads != 30 || report.RestoredRecallSamples != 6 ||
		report.RestoredProvenanceChecks != 3 || report.RestoredPrivateLeakChecks != 3 || report.RestoredPrivateLeaks != 0 ||
		report.Source.StoredReceipts != 30 || report.Restored.StoredReceipts != 30 ||
		report.Source.ActiveRecords != 6 || report.Restored.ActiveRecords != 6 {
		t.Fatalf("small soak report = %+v", report)
	}
}

func TestRunRejectsNonEmptyRetainedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sentinel"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{
		"-data-dir", root, "-spaces", "1", "-receipts", "1", "-records", "1",
	}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("run accepted a non-empty retained root")
	}
}
