// corpus_eval measures durable multi-round Remember and lexical Recall against
// private local material without emitting source text, queries, or receipts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/caelis-labs/memory/internal/appliance"
)

type options struct {
	sourcePath    string
	sourceKind    string
	outputPath    string
	dataDir       string
	rounds        int
	limit         int
	lexiconPolicy appliance.LexiconPolicy
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "corpus_eval: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("corpus_eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts options
	flags.StringVar(&opts.sourcePath, "source", "", "local Memory Markdown or Codex/Caelis Session JSONL source")
	flags.StringVar(&opts.sourceKind, "format", "auto", "source format: auto, markdown, codex-jsonl, or caelis-jsonl")
	flags.StringVar(&opts.outputPath, "output", "", "optional owner-only aggregate JSON report")
	flags.StringVar(&opts.dataDir, "data-dir", "", "optional retained evaluation data directory")
	flags.IntVar(&opts.rounds, "rounds", 6, "number of write, restart, and Recall rounds")
	flags.IntVar(&opts.limit, "limit", 400, "maximum eligible source chunks")
	flags.IntVar(&opts.lexiconPolicy.MinDocumentFrequency, "lexicon-min-docs", 3, "independent receipts required for static term activation")
	flags.IntVar(&opts.lexiconPolicy.MinBoundaryDiversity, "lexicon-min-boundaries", 2, "distinct left and right boundaries required for activation")
	flags.Float64Var(&opts.lexiconPolicy.MinActivationScore, "lexicon-min-score", 6, "minimum evidence score for activation")
	flags.Float64Var(&opts.lexiconPolicy.MaxAverageOccurrences, "lexicon-max-average-occurrences", 8, "maximum average occurrences per receipt")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if opts.sourcePath == "" {
		return fmt.Errorf("-source is required")
	}
	if opts.rounds < 2 || opts.rounds > 20 {
		return fmt.Errorf("-rounds must be within 2..20")
	}
	if opts.limit < 10 || opts.limit > 10_000 {
		return fmt.Errorf("-limit must be within 10..10000")
	}
	if opts.lexiconPolicy.MinDocumentFrequency < 2 || opts.lexiconPolicy.MinDocumentFrequency > 100_000 ||
		opts.lexiconPolicy.MinBoundaryDiversity < 1 || opts.lexiconPolicy.MinBoundaryDiversity > 8 ||
		opts.lexiconPolicy.MinActivationScore <= 0 || opts.lexiconPolicy.MinActivationScore > 100 ||
		opts.lexiconPolicy.MaxAverageOccurrences < 1 || opts.lexiconPolicy.MaxAverageOccurrences > 100 {
		return fmt.Errorf("lexicon policy flags are outside their supported evaluation range")
	}
	source, err := loadSource(opts.sourcePath, opts.sourceKind)
	if err != nil {
		return err
	}
	report, err := evaluate(ctx, source, opts)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode aggregate report: %w", err)
	}
	encoded = append(encoded, '\n')
	if opts.outputPath == "" {
		_, err = stdout.Write(encoded)
		return err
	}
	return writeReport(opts.outputPath, encoded)
}

func writeReport(path string, encoded []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".memory-corpus-report-*")
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
		return fmt.Errorf("write aggregate report: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync aggregate report: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close aggregate report: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish aggregate report: %w", err)
	}
	return nil
}

func removeTemporaryData(path string) error {
	if path == "" {
		return nil
	}
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove temporary evaluation data: %w", err)
	}
	return nil
}
