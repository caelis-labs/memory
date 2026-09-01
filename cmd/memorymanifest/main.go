package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caelis-labs/memory/sdk/go/memory/sidecar"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "memorymanifest: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var binaryPath, outputPath, serviceVersion, revision, goos, goarch string
	flag.StringVar(&binaryPath, "binary", "", "memoryd executable path")
	flag.StringVar(&outputPath, "output", "", "manifest output path")
	flag.StringVar(&serviceVersion, "service-version", "", "memoryd service version")
	flag.StringVar(&revision, "revision", "", "exact source revision")
	flag.StringVar(&goos, "goos", "", "artifact GOOS")
	flag.StringVar(&goarch, "goarch", "", "artifact GOARCH")
	flag.Parse()
	if binaryPath == "" || outputPath == "" {
		return fmt.Errorf("-binary and -output are required")
	}
	manifest, err := sidecar.CreateManifest(binaryPath, serviceVersion, revision, goos, goarch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".memoryd-manifest-*")
	if err != nil {
		return fmt.Errorf("create manifest temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set manifest permissions: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish manifest: %w", err)
	}
	return nil
}
