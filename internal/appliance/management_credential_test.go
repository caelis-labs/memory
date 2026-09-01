package appliance

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestManagementCredentialRotationRevokesOldBearerAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	store, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	oldCredential := strings.TrimSpace(string(oldBytes))
	if err := store.RotateManagementCredential(t.Context()); err != nil {
		t.Fatal(err)
	}
	newBytes, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	newCredential := strings.TrimSpace(string(newBytes))
	if newCredential == oldCredential || newCredential == "" {
		t.Fatal("management credential rotation did not replace the bearer")
	}
	if store.AuthenticateManagement(oldCredential) || !store.AuthenticateManagement(newCredential) {
		t.Fatal("management credential rotation did not revoke old authority")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.AuthenticateManagement(oldCredential) || !restarted.AuthenticateManagement(newCredential) {
		t.Fatal("rotated management authority did not survive restart")
	}
}

func TestPendingManagementCredentialRotationRecoversEitherRenameOutcome(t *testing.T) {
	for _, renamed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before rename", true: "after rename"}[renamed], func(t *testing.T) {
			dataDir := t.TempDir()
			store, err := Open(t.Context(), Options{DataDir: dataDir})
			if err != nil {
				t.Fatal(err)
			}
			oldBytes, err := os.ReadFile(store.ManagementCredentialPath())
			if err != nil {
				t.Fatal(err)
			}
			oldCredential := strings.TrimSpace(string(oldBytes))
			newCredential := "pending-management-credential"
			if _, err := store.db.ExecContext(t.Context(),
				`INSERT INTO metadata(key, value) VALUES ('management_credential_digest_pending', ?)`,
				digestString(newCredential)); err != nil {
				t.Fatal(err)
			}
			if renamed {
				if err := os.WriteFile(store.ManagementCredentialPath(), []byte(newCredential+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := Open(t.Context(), Options{DataDir: dataDir})
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			wantCredential := oldCredential
			if renamed {
				wantCredential = newCredential
			}
			if !restarted.AuthenticateManagement(wantCredential) {
				t.Fatal("restart did not recover the file-selected management credential")
			}
			var pending int
			if err := restarted.db.QueryRowContext(t.Context(),
				`SELECT COUNT(*) FROM metadata WHERE key = 'management_credential_digest_pending'`).Scan(&pending); err != nil {
				t.Fatal(err)
			}
			if pending != 0 {
				t.Fatal("restart left pending management credential state")
			}
		})
	}
}

func TestConcurrentManagementAuthenticationAndRotation(t *testing.T) {
	store, err := Open(t.Context(), Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credentialBytes, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	credential := strings.TrimSpace(string(credentialBytes))
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				_ = store.AuthenticateManagement(credential)
			}
		}()
	}
	if err := store.RotateManagementCredential(t.Context()); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
}
