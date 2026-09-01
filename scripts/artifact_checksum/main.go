package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "artifact_checksum: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var filePath, outputPath, verifyPath string
	flag.StringVar(&filePath, "file", "", "artifact to checksum")
	flag.StringVar(&outputPath, "output", "", "detached checksum output")
	flag.StringVar(&verifyPath, "verify", "", "detached checksum to verify")
	flag.Parse()
	if verifyPath != "" {
		if filePath != "" || outputPath != "" {
			return fmt.Errorf("-verify is mutually exclusive with -file and -output")
		}
		return verify(verifyPath)
	}
	if filePath == "" || outputPath == "" {
		return fmt.Errorf("-file and -output are required")
	}
	digest, err := checksum(filePath)
	if err != nil {
		return err
	}
	line := digest + "  " + filepath.Base(filePath) + "\n"
	if err := os.WriteFile(outputPath, []byte(line), 0o644); err != nil {
		return fmt.Errorf("write detached checksum: %w", err)
	}
	return nil
}

func verify(checksumPath string) error {
	file, err := os.Open(checksumPath)
	if err != nil {
		return fmt.Errorf("open detached checksum: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, 8<<10))
	if !scanner.Scan() {
		return fmt.Errorf("detached checksum is empty")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) != 2 || len(fields[0]) != sha256.Size*2 || filepath.Base(fields[1]) != fields[1] {
		return fmt.Errorf("detached checksum format is invalid")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil || strings.ToLower(fields[0]) != fields[0] {
		return fmt.Errorf("detached checksum digest is invalid")
	}
	if scanner.Scan() || scanner.Err() != nil {
		return fmt.Errorf("detached checksum contains trailing data")
	}
	artifactPath := filepath.Join(filepath.Dir(checksumPath), fields[1])
	digest, err := checksum(artifactPath)
	if err != nil {
		return err
	}
	if digest != fields[0] {
		return fmt.Errorf("artifact checksum mismatch")
	}
	return nil
}

func checksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
