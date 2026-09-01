package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
)

func TestSecretOutputIsExclusiveAndOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authorization.json")
	file, err := reserveSecretOutput(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSecretJSON(file, map[string]string{"capability": "secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode = %o, want 600", info.Mode().Perm())
	}
	opened, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	var value map[string]string
	if err := json.NewDecoder(opened).Decode(&value); err != nil {
		t.Fatal(err)
	}
	if value["capability"] != "secret" {
		t.Fatalf("secret output = %+v", value)
	}
	if _, err := reserveSecretOutput(path); err == nil {
		t.Fatal("secret output reservation overwrote an existing file")
	}
}

func TestReadIssuerCredentialFormats(t *testing.T) {
	directory := t.TempDir()
	write := func(name string, value any) string {
		t.Helper()
		path := filepath.Join(directory, name)
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	bootstrap := write("bootstrap.json", managementv1alpha1.BootstrapResponse{
		IssuerCredentials: map[string]string{"principal:a": "secret-a"},
	})
	if got, err := readIssuerCredential(bootstrap, "principal:a"); err != nil || got != "secret-a" {
		t.Fatalf("bootstrap credential = %q, %v", got, err)
	}
	if _, err := readIssuerCredential(bootstrap, "principal:b"); err == nil {
		t.Fatal("readIssuerCredential() accepted a missing principal")
	}
	rotated := write("rotated.json", managementv1alpha1.IssuerAuthorization{PrincipalRef: "principal:a", Credential: "secret-b"})
	if got, err := readIssuerCredential(rotated, "principal:a"); err != nil || got != "secret-b" {
		t.Fatalf("rotated credential = %q, %v", got, err)
	}
	raw := filepath.Join(directory, "raw.token")
	if err := os.WriteFile(raw, []byte("secret-c\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readIssuerCredential(raw, "principal:a"); err != nil || got != "secret-c" {
		t.Fatalf("raw credential = %q, %v", got, err)
	}
}
