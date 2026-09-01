// ga_soak runs the fixed synthetic GA durability and scale qualification.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type options struct {
	dataDir  string
	output   string
	spaces   int
	receipts int
	records  int
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "ga_soak: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("ga_soak", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts options
	flags.StringVar(&opts.dataDir, "data-dir", "", "optional empty directory in which to retain the source appliance")
	flags.StringVar(&opts.output, "output", "", "optional owner-only aggregate JSON report")
	flags.IntVar(&opts.spaces, "spaces", 100, "number of isolated private Spaces")
	flags.IntVar(&opts.receipts, "receipts", 100_000, "total acknowledged receipts")
	flags.IntVar(&opts.records, "records", 10_000, "semantic Record heads to organize")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if opts.spaces < 1 || opts.spaces > 1_000 || opts.receipts < opts.spaces || opts.records < opts.spaces || opts.records > opts.receipts {
		return fmt.Errorf("require 1..1000 Spaces and at least one receipt and Record per Space")
	}

	root := opts.dataDir
	temporary := false
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "caelis-memory-ga-soak-")
		if err != nil {
			return fmt.Errorf("create soak directory: %w", err)
		}
		temporary = true
	} else {
		entries, err := os.ReadDir(root)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect soak data directory: %w", err)
		}
		if err == nil && len(entries) != 0 {
			return fmt.Errorf("-data-dir must be empty")
		}
	}
	if temporary {
		defer os.RemoveAll(root)
	}

	report, err := executeSoak(ctx, filepath.Join(root, "source"), filepath.Join(root, "restored"), opts)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode soak report: %w", err)
	}
	encoded = append(encoded, '\n')
	if opts.output == "" {
		_, err = stdout.Write(encoded)
		return err
	}
	return writeAggregateReport(opts.output, encoded)
}

func writeAggregateReport(path string, encoded []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".memory-ga-soak-report-*")
	if err != nil {
		return fmt.Errorf("create report temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure report temporary file: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write report: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync report: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish report: %w", err)
	}
	return nil
}
