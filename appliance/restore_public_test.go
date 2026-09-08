package appliance_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/caelis-labs/memory/appliance"
)

func TestOwnedOfflineRestoreLifecycleKeepsCredentialInsideAppliance(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	runtime, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot bytes.Buffer
	if err := runtime.Backup(ctx, &snapshot); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	prepared, err := appliance.PrepareUpgradeOwned(ctx, dataDir)
	if err != nil {
		t.Fatalf("PrepareUpgradeOwned() error = %v", err)
	}
	if !prepared.RollbackAvailable {
		t.Fatal("PrepareUpgradeOwned() did not retain rollback")
	}
	if _, err := appliance.RollbackRestoreOwned(ctx, dataDir, nil); err != nil {
		t.Fatalf("RollbackRestoreOwned() error = %v", err)
	}

	if _, err := appliance.RestoreOwned(ctx, appliance.OfflineRestoreOptions{
		DataDir: dataDir, Snapshot: bytes.NewReader(snapshot.Bytes()),
	}); err != nil {
		t.Fatalf("RestoreOwned() error = %v", err)
	}
	if err := appliance.CommitRestoreOwned(dataDir); err != nil {
		t.Fatalf("CommitRestoreOwned() error = %v", err)
	}
	if reopened, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir}); err != nil {
		t.Fatalf("Open() after owned restore error = %v", err)
	} else if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}
