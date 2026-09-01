// Package sidecar verifies a packaged memoryd before a host launches it.
package sidecar

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const ManifestFormatVersion = 1

// Manifest binds one native executable to its service and protocol identity.
type Manifest struct {
	FormatVersion  int    `json:"format_version"`
	ServiceVersion string `json:"service_version"`
	BuildRevision  string `json:"build_revision"`
	Protocol       string `json:"protocol"`
	APIVersion     string `json:"api_version"`
	CoreProfile    string `json:"core_profile"`
	GOOS           string `json:"goos"`
	GOARCH         string `json:"goarch"`
	Executable     string `json:"executable"`
	SHA256         string `json:"sha256"`
}

// CreateManifest hashes one regular executable and returns its exact manifest.
func CreateManifest(binaryPath, serviceVersion, buildRevision, goos, goarch string) (Manifest, error) {
	if serviceVersion == "" || buildRevision == "" || goos == "" || goarch == "" {
		return Manifest{}, fmt.Errorf("service version, revision, GOOS, and GOARCH are required")
	}
	info, err := os.Lstat(binaryPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("inspect sidecar executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, fmt.Errorf("sidecar executable must be a regular file")
	}
	digest, err := fileDigest(binaryPath)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion, ServiceVersion: serviceVersion,
		BuildRevision: buildRevision, Protocol: v1alpha1.LocalTransportProtocol,
		APIVersion: v1alpha1.ProtocolVersion, CoreProfile: v1alpha1.CoreProfile,
		GOOS: goos, GOARCH: goarch, Executable: filepath.Base(binaryPath), SHA256: digest,
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Load reads and validates a bounded sidecar manifest.
func Load(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open sidecar manifest: %w", err)
	}
	defer file.Close()
	var manifest Manifest
	decoder := json.NewDecoder(io.LimitReader(file, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode sidecar manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Manifest{}, fmt.Errorf("sidecar manifest contains trailing data")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate rejects unsupported or incomplete manifest identities.
func (m Manifest) Validate() error {
	if m.FormatVersion != ManifestFormatVersion {
		return fmt.Errorf("unsupported sidecar manifest format %d", m.FormatVersion)
	}
	if m.ServiceVersion == "" || m.BuildRevision == "" {
		return fmt.Errorf("sidecar service version and revision are required")
	}
	if !validRevision(m.BuildRevision) {
		return fmt.Errorf("sidecar build revision must be a full hexadecimal Git object ID")
	}
	if m.Protocol != v1alpha1.LocalTransportProtocol || m.APIVersion != v1alpha1.ProtocolVersion || m.CoreProfile != v1alpha1.CoreProfile {
		return fmt.Errorf("sidecar compatibility profile is unsupported")
	}
	if m.GOOS == "" || m.GOARCH == "" {
		return fmt.Errorf("sidecar platform is required")
	}
	if m.Executable == "" || filepath.Base(m.Executable) != m.Executable || m.Executable == "." {
		return fmt.Errorf("sidecar executable must be a base name")
	}
	if len(m.SHA256) != sha256.Size*2 || strings.ToLower(m.SHA256) != m.SHA256 {
		return fmt.Errorf("sidecar SHA-256 is invalid")
	}
	if _, err := hex.DecodeString(m.SHA256); err != nil {
		return fmt.Errorf("sidecar SHA-256 is invalid")
	}
	return nil
}

// VerifyNative verifies the current platform and exact executable bytes.
func (m Manifest) VerifyNative(directory string) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	if m.GOOS != runtime.GOOS || m.GOARCH != runtime.GOARCH {
		return "", fmt.Errorf("sidecar platform %s/%s does not match host %s/%s", m.GOOS, m.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	path := filepath.Join(directory, m.Executable)
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect sidecar executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("sidecar executable must be a regular executable file")
	}
	digest, err := fileDigest(path)
	if err != nil {
		return "", err
	}
	if digest != m.SHA256 {
		return "", fmt.Errorf("sidecar executable digest mismatch")
	}
	return path, nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open sidecar executable: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash sidecar executable: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validRevision(revision string) bool {
	if len(revision) != 40 && len(revision) != 64 {
		return false
	}
	if strings.ToLower(revision) != revision {
		return false
	}
	_, err := hex.DecodeString(revision)
	return err == nil
}
