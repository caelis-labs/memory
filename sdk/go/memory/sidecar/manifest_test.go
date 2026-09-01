package sidecar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManifestVerifiesExactNativeExecutable(t *testing.T) {
	directory := t.TempDir()
	binaryPath := filepath.Join(directory, "memoryd-test")
	if err := os.WriteFile(binaryPath, []byte("first executable bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := CreateManifest(binaryPath, "0.2.0-alpha.1", strings.Repeat("a", 40), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := manifest.VerifyNative(directory)
	if err != nil {
		t.Fatal(err)
	}
	if verified != binaryPath {
		t.Fatalf("VerifyNative() = %q, want %q", verified, binaryPath)
	}
	if err := os.WriteFile(binaryPath, []byte("tampered executable bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.VerifyNative(directory); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("VerifyNative(tampered) error = %v", err)
	}
}

func TestSupportedPlatformRequiresNativeReleaseEvidence(t *testing.T) {
	platforms := SupportedPlatforms()
	if len(platforms) != 1 || platforms[0] != (Platform{GOOS: "darwin", GOARCH: "arm64"}) {
		t.Fatalf("SupportedPlatforms() = %+v", platforms)
	}
	platforms[0] = Platform{GOOS: "changed", GOARCH: "changed"}
	if !IsSupportedPlatform("darwin", "arm64") {
		t.Fatal("caller mutated the supported platform authority")
	}
	for _, platform := range []Platform{
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
	} {
		if IsSupportedPlatform(platform.GOOS, platform.GOARCH) {
			t.Fatalf("unsupported platform accepted: %+v", platform)
		}
	}
}

func TestVerifySupportedNativeRejectsBuildablePreview(t *testing.T) {
	directory := t.TempDir()
	binaryPath := filepath.Join(directory, "memoryd-test")
	if err := os.WriteFile(binaryPath, []byte("executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := CreateManifest(binaryPath, "0.3.0-alpha.1", strings.Repeat("a", 40), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manifest.VerifySupportedNative(directory)
	if IsSupportedPlatform(runtime.GOOS, runtime.GOARCH) {
		if err != nil {
			t.Fatalf("VerifySupportedNative() = %v", err)
		}
	} else if err == nil || !strings.Contains(err.Error(), "not a supported release platform") {
		t.Fatalf("VerifySupportedNative(preview) error = %v", err)
	}
}

func TestManifestRejectsPlatformTraversalAndSymlink(t *testing.T) {
	directory := t.TempDir()
	binaryPath := filepath.Join(directory, "memoryd-test")
	if err := os.WriteFile(binaryPath, []byte("executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := CreateManifest(binaryPath, "0.2.0-alpha.1", strings.Repeat("a", 40), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	wrongPlatform := manifest
	wrongPlatform.GOOS = "unsupported-os"
	if _, err := wrongPlatform.VerifyNative(directory); err == nil || !strings.Contains(err.Error(), "does not match host") {
		t.Fatalf("VerifyNative(wrong platform) error = %v", err)
	}
	traversal := manifest
	traversal.Executable = "../memoryd"
	if err := traversal.Validate(); err == nil {
		t.Fatal("Validate() accepted executable traversal")
	}
	symlinkName := "memoryd-link"
	if err := os.Symlink(binaryPath, filepath.Join(directory, symlinkName)); err != nil {
		t.Fatal(err)
	}
	symlinkManifest := manifest
	symlinkManifest.Executable = symlinkName
	if _, err := symlinkManifest.VerifyNative(directory); err == nil || !strings.Contains(err.Error(), "regular executable") {
		t.Fatalf("VerifyNative(symlink) error = %v", err)
	}
}

func TestLoadRejectsUnknownAndTrailingManifestData(t *testing.T) {
	directory := t.TempDir()
	binaryPath := filepath.Join(directory, "memoryd-test")
	if err := os.WriteFile(binaryPath, []byte("executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := CreateManifest(binaryPath, "0.2.0-alpha.1", strings.Repeat("a", 40), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	validPath := filepath.Join(directory, "valid.json")
	if err := os.WriteFile(validPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(validPath)
	if err != nil || loaded != manifest {
		t.Fatalf("Load(valid) = %+v, %v", loaded, err)
	}
	unknownPath := filepath.Join(directory, "unknown.json")
	unknown := append(encoded[:len(encoded)-1], []byte(`,"unknown":true}`)...)
	if err := os.WriteFile(unknownPath, unknown, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unknownPath); err == nil {
		t.Fatal("Load() accepted unknown manifest field")
	}
	trailingPath := filepath.Join(directory, "trailing.json")
	if err := os.WriteFile(trailingPath, append(encoded, []byte(`{}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(trailingPath); err == nil {
		t.Fatal("Load() accepted trailing manifest data")
	}
}
