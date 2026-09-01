package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndVerifyDetachedChecksum(t *testing.T) {
	directory := t.TempDir()
	artifact := filepath.Join(directory, "memoryd-windows-amd64.exe")
	checksumPath := artifact + ".sha256"
	if err := os.WriteFile(artifact, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	withArgs(t, []string{"checksum", "-file", artifact, "-output", checksumPath}, func() {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	})
	withArgs(t, []string{"checksum", "-verify", checksumPath}, func() {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	})
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "  memoryd-windows-amd64.exe\n") {
		t.Fatalf("detached checksum = %q", data)
	}
	if err := os.WriteFile(artifact, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	withArgs(t, []string{"checksum", "-verify", checksumPath}, func() {
		if err := run(); err == nil || !strings.Contains(err.Error(), "mismatch") {
			t.Fatalf("verify tampered artifact error = %v", err)
		}
	})
}

func withArgs(t *testing.T, args []string, runFn func()) {
	t.Helper()
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	os.Args = args
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	})
	runFn()
}
