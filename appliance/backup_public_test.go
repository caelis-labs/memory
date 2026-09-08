package appliance_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/caelis-labs/memory/appliance"
)

func TestEmbeddedRuntimeBackupUsesOwnerSnapshot(t *testing.T) {
	runtime, err := appliance.Open(context.Background(), appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	var backup bytes.Buffer
	if err := runtime.Backup(context.Background(), &backup); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if !bytes.HasPrefix(backup.Bytes(), []byte("SQLite format 3\x00")) {
		t.Fatalf("Backup() header = %q, want SQLite", backup.Bytes()[:min(16, backup.Len())])
	}
	if backup.Len() == 0 {
		t.Fatal("Backup() returned an empty snapshot")
	}
	if err := runtime.Management().Backup(context.Background(), &backup); err != nil {
		t.Fatalf("Management().Backup() error = %v", err)
	}
}
