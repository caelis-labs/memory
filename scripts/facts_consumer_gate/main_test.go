package main

import (
	"strings"
	"testing"
)

func TestPublishedModuleHasNoLocalReplacement(t *testing.T) {
	mod, err := temporaryGoMod("/nonexistent/local/checkout", "v0.6.1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mod), "replace") || !strings.Contains(string(mod), "require "+memoryModule+" v0.6.1\n") {
		t.Fatalf("published consumer did not pin the release: %s", mod)
	}
	if _, err := temporaryGoMod("", "v0.6.1\nreplace example.com/other => /tmp/other"); err == nil {
		t.Fatal("invalid version accepted")
	}
}
