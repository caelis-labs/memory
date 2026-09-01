package appliance

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestBackupRestoreRotatesGenerationAndRollbackPreservesAcknowledgedState(t *testing.T) {
	dataDir := t.TempDir()
	store, auth := newGoldenStore(t, dataDir, time.Now)
	before, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "fact present in backup", IdempotencyKey: "before-backup",
	})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.CreateBackupSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	backup, err := io.ReadAll(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "acknowledged after backup", IdempotencyKey: "after-backup",
	})
	if err != nil {
		t.Fatal(err)
	}
	credentialBytes, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	credential := string(trimCredential(credentialBytes))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restored, err := Restore(t.Context(), RestoreOptions{
		DataDir: dataDir, Snapshot: bytes.NewReader(backup), ManagementCredential: credential,
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored.SourceGeneration != inspection.Generation || restored.StorageGeneration == inspection.Generation || !restored.RollbackAvailable {
		t.Fatalf("restore result = %+v, source generation = %s", restored, inspection.Generation)
	}
	if _, err := os.Stat(filepath.Join(dataDir, RollbackDatabaseFilename)); err != nil {
		t.Fatal(err)
	}
	restoredStore, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := restoredStore.Ready(t.Context()); err == nil {
		t.Fatal("restored generation became ready before operator commit")
	}
	search, err := restoredStore.SearchReceipts(t.Context(), managementv1alpha1.SearchReceiptsRequest{Query: "backup", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Receipts) != 1 || search.Receipts[0].Text != "fact present in backup" {
		t.Fatalf("restored management search = %+v", search.Receipts)
	}
	search, err = restoredStore.SearchReceipts(t.Context(), managementv1alpha1.SearchReceiptsRequest{Query: "acknowledged", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Receipts) != 0 {
		t.Fatalf("post-backup receipt appeared in restored snapshot: %+v", search.Receipts)
	}
	if _, err := restoredStore.Recall(t.Context(), auth, testRecall("backup", before.ConsistencyToken)); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnavailable) {
		t.Fatalf("pending restored Recall error = %v, want unavailable", err)
	}
	if err := restoredStore.Close(); err != nil {
		t.Fatal(err)
	}

	rolledBack, err := RollbackRestore(t.Context(), dataDir, credential, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.StorageGeneration == restored.StorageGeneration || rolledBack.StorageGeneration == inspection.Generation {
		t.Fatalf("rollback did not rotate generation: restore=%+v rollback=%+v", restored, rolledBack)
	}
	rollbackStore, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollbackStore.Close() })
	response, err := rollbackStore.Recall(t.Context(), auth, testRecall("acknowledged", ""))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, response, "acknowledged after backup")
	if _, err := rollbackStore.Recall(t.Context(), auth, testRecall("acknowledged", after.ConsistencyToken)); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeStaleConsistencyToken) {
		t.Fatalf("pre-rollback cursor error = %v, want stale_consistency_token", err)
	}
}

func TestRestoreFailureLeavesCurrentDatabaseUsable(t *testing.T) {
	dataDir := t.TempDir()
	store, auth := newGoldenStore(t, dataDir, time.Now)
	remembered, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "current database survives failed restore", IdempotencyKey: "restore-failure-current",
	})
	if err != nil {
		t.Fatal(err)
	}
	credentialBytes, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	credential := string(trimCredential(credentialBytes))
	if _, err := Restore(t.Context(), RestoreOptions{
		DataDir: dataDir, Snapshot: bytes.NewReader([]byte("not a SQLite backup")), ManagementCredential: credential,
	}); !errors.Is(err, ErrOwnerLocked) {
		t.Fatalf("online Restore error = %v, want ErrOwnerLocked", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), RestoreOptions{
		DataDir: dataDir, Snapshot: bytes.NewReader([]byte("not a SQLite backup")), ManagementCredential: credential,
	}); err == nil {
		t.Fatal("corrupt restore snapshot succeeded")
	}
	if _, err := os.Stat(filepath.Join(dataDir, RollbackDatabaseFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed restore created rollback state: %v", err)
	}
	reopened, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	response, err := reopened.Recall(t.Context(), auth, testRecall("survives", remembered.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, response, "current database survives failed restore")
}

func TestCommitRestoreRemovesRollbackOnlyWhileOffline(t *testing.T) {
	dataDir := t.TempDir()
	store, _ := newGoldenStore(t, dataDir, time.Now)
	snapshot, err := store.CreateBackupSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	backup, err := io.ReadAll(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	_ = snapshot.Close()
	credentialBytes, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	credential := string(trimCredential(credentialBytes))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), RestoreOptions{DataDir: dataDir, Snapshot: bytes.NewReader(backup), ManagementCredential: credential}); err != nil {
		t.Fatal(err)
	}
	running, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitRestore(dataDir, credential); !errors.Is(err, ErrOwnerLocked) {
		t.Fatalf("online CommitRestore error = %v, want ErrOwnerLocked", err)
	}
	if err := running.Ready(t.Context()); err == nil {
		t.Fatal("restored generation was ready before commit")
	}
	if err := running.CommitRestore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := running.Ready(t.Context()); err != nil {
		t.Fatalf("committed restored generation is not ready: %v", err)
	}
	if err := running.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CommitRestore(dataDir, credential); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, RollbackDatabaseFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback database still exists: %v", err)
	}
}
