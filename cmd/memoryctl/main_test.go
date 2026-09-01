package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
