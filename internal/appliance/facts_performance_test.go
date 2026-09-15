package appliance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// The M06 facts performance harness is opt-in and reports only measured
// numbers. It seeds one hot Space/LabelSet partition through the real evidence
// admission path, then measures cold and warm point reads, the owner change
// ledger, reader/writer contention, rebuild, managed cleansing, database
// growth and process residency. It never calls a model and never sleeps to
// manufacture a result.
//
//	make facts-perf
//	MEMORY_FACTS_PERF=1 MEMORY_FACTS_PERF_SIZES=1000,10000,100000 \
//	  go test -count=1 -timeout 60m -run '^TestFactsPerformanceHarness$' -v ./internal/appliance
const (
	factsPerfEnv        = "MEMORY_FACTS_PERF"
	factsPerfSizesEnv   = "MEMORY_FACTS_PERF_SIZES"
	factsPerfReportEnv  = "MEMORY_FACTS_PERF_REPORT"
	factsPerfWarmEnv    = "MEMORY_FACTS_PERF_WARM_QUERIES"
	factsPerfContendEnv = "MEMORY_FACTS_PERF_CONTENDERS"
	factsPerfSourceText = "Performance receipt keeps a single hot partition fact."
)

type factsPerfReport struct {
	FormatVersion int            `json:"format_version"`
	Engine        string         `json:"engine"`
	GoVersion     string         `json:"go_version"`
	GOOS          string         `json:"goos"`
	GOARCH        string         `json:"goarch"`
	ModelCalls    int            `json:"model_calls"`
	Notes         []string       `json:"notes"`
	Runs          []factsPerfRun `json:"runs"`
	FinishedAt    string         `json:"finished_at"`
}

type factsPerfRun struct {
	Receipts                  int                 `json:"receipts"`
	HotPartition              string              `json:"hot_partition"`
	Seed                      factsPerfLatency    `json:"seed_per_receipt"`
	SeedTotalMS               float64             `json:"seed_total_ms"`
	ColdReadMS                float64             `json:"cold_first_read_ms"`
	ColdChangesMS             float64             `json:"cold_first_changes_ms"`
	WarmRead                  factsPerfLatency    `json:"warm_read"`
	WarmChanges               factsPerfLatency    `json:"warm_changes"`
	Contention                factsPerfContention `json:"contention"`
	Rebuild                   factsPerfLatency    `json:"rebuild_fts"`
	Cleanup                   factsPerfLatency    `json:"managed_cleanup"`
	CleanupSamples            int                 `json:"cleanup_samples"`
	CleanupCompleted          int                 `json:"cleanup_completed"`
	DatabaseBytes             int64               `json:"database_bytes_before_seed"`
	DatabaseBytesAfterSeed    int64               `json:"database_bytes_after_seed"`
	DatabaseBytesAfterRebuild int64               `json:"database_bytes_after_rebuild"`
	FTSBytes                  int64               `json:"fts_bytes"`
	FTSSupported              bool                `json:"fts_bytes_supported"`
	RSSBytesBefore            int64               `json:"rss_bytes_before"`
	RSSBytesAfter             int64               `json:"rss_bytes_after"`
	HeapAllocBytes            uint64              `json:"heap_alloc_bytes"`
	HeapSysBytes              uint64              `json:"heap_sys_bytes"`
}

type factsPerfContention struct {
	Readers   int              `json:"readers"`
	WriterOps int              `json:"writer_ops"`
	ReaderOps int              `json:"reader_ops"`
	Errors    int              `json:"errors"`
	Latency   factsPerfLatency `json:"reader_latency"`
	WriterMS  float64          `json:"writer_total_ms"`
}

type factsPerfLatency struct {
	Samples int     `json:"samples"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
	MinMS   float64 `json:"min_ms"`
	MaxMS   float64 `json:"max_ms"`
}

func TestFactsPerformanceHarness(t *testing.T) {
	if os.Getenv(factsPerfEnv) != "1" {
		t.Skipf("opt-in performance harness; set %s=1 (for example: make facts-perf)", factsPerfEnv)
	}
	sizes := factsPerfParseSizes(t, os.Getenv(factsPerfSizesEnv))
	warm := factsPerfEnvInt(factsPerfWarmEnv, 200)
	contenders := factsPerfEnvInt(factsPerfContendEnv, 8)
	report := factsPerfReport{
		FormatVersion: 1,
		Engine:        "sqlite (modernc.org/sqlite) real files; real Store facts/evidence/management APIs; no random sleeps",
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		ModelCalls:    0,
		Notes: []string{
			"Measured on this machine only; these are not release thresholds and do not claim a hardware-independent result.",
			"No model is invoked, so no model usage or model latency is measured.",
			"Each size uses one fresh on-disk SQLite database in a single Space and LabelSet partition.",
			"The first seeded receipt includes lazy schema/FTS warmup, visible in seed max_ms rather than p50/p95.",
			fmt.Sprintf("warm_queries=%d contenders=%d", warm, contenders),
		},
	}
	for _, size := range sizes {
		t.Logf("facts performance: seeding %d receipts", size)
		report.Runs = append(report.Runs, factsPerfRunSize(t, size, warm, contenders))
	}
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal facts performance report: %v", err)
	}
	t.Logf("facts performance report:\n%s", encoded)
	if path := os.Getenv(factsPerfReportEnv); path != "" {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatalf("create facts performance report directory: %v", err)
			}
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
			t.Fatalf("write facts performance report: %v", err)
		}
	}
}

func factsPerfParseSizes(t *testing.T, value string) []int {
	t.Helper()
	if strings.TrimSpace(value) == "" {
		return []int{1000}
	}
	var sizes []int
	for _, part := range strings.Split(value, ",") {
		size, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || size < 1 {
			t.Fatalf("invalid %s entry %q", factsPerfSizesEnv, part)
		}
		sizes = append(sizes, size)
	}
	return sizes
}

func factsPerfEnvInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func factsPerfRunSize(t *testing.T, receipts, warm, contenders int) factsPerfRun {
	t.Helper()
	dataDir := t.TempDir()
	store, auth := newGoldenStoreWithOptions(t, Options{DataDir: dataDir, Clock: time.Now, BusyTimeoutMS: 5000})
	run := factsPerfRun{Receipts: receipts, HotPartition: "space-bot-a"}
	run.RSSBytesBefore = factsPerfRSSBytes()
	run.DatabaseBytes = factsPerfDatabaseBytes(t, store)
	validFrom := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	subjects := make([]string, receipts)
	seedDurations := make([]time.Duration, 0, receipts)
	seedStarted := time.Now()
	for index := 0; index < receipts; index++ {
		subject := fmt.Sprintf("perf-user-%08d", index)
		subjects[index] = subject
		request := facts.SubmitEvidenceRequest{
			Source: facts.Source{
				Producer: "facts-perf", EventID: fmt.Sprintf("facts-perf-%08d", index),
				Revision: "r1", Fragment: "f1", Subject: subject, FactKey: "perf.hot", Role: facts.RoleConfirmation,
			},
			Text:           factsPerfSourceText,
			IdempotencyKey: fmt.Sprintf("facts-perf-%08d", index),
			Mutations: []facts.Mutation{{
				Transition: facts.TransitionEstablish, Subject: subject, Key: "perf.hot",
				Text: factsPerfSourceText, ValidFrom: &validFrom,
			}},
		}
		started := time.Now()
		if _, err := store.SubmitEvidence(t.Context(), auth, request); err != nil {
			t.Fatalf("seed receipt %d: %v", index, err)
		}
		seedDurations = append(seedDurations, time.Since(started))
	}
	run.SeedTotalMS = float64(time.Since(seedStarted).Microseconds()) / 1000
	run.Seed = factsPerfLatencyOf(seedDurations)
	if err := store.Close(); err != nil {
		t.Fatalf("close facts performance store before cold read: %v", err)
	}
	store, err := Open(t.Context(), Options{DataDir: dataDir, Clock: time.Now, BusyTimeoutMS: 5000})
	if err != nil {
		t.Fatalf("reopen facts performance store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	target := subjects[len(subjects)/2]
	coldStarted := time.Now()
	if _, err := store.ReadFacts(t.Context(), auth, facts.ReadRequest{Subject: target, Key: "perf.hot", Budget: facts.Budget{MaxFacts: 8, MaxBytes: 8192}}); err != nil {
		t.Fatalf("cold facts read: %v", err)
	}
	run.ColdReadMS = float64(time.Since(coldStarted).Microseconds()) / 1000

	coldCursor := time.Now()
	first, err := store.Changes(t.Context(), auth, facts.ChangesRequest{Limit: 256})
	if err != nil {
		t.Fatalf("cold changes: %v", err)
	}
	run.ColdChangesMS = float64(time.Since(coldCursor).Microseconds()) / 1000

	readDurations := make([]time.Duration, 0, warm)
	for index := 0; index < warm; index++ {
		subject := subjects[(index*7919)%len(subjects)]
		started := time.Now()
		if _, err := store.ReadFacts(t.Context(), auth, facts.ReadRequest{Subject: subject, Key: "perf.hot", Budget: facts.Budget{MaxFacts: 8, MaxBytes: 8192}}); err != nil {
			t.Fatalf("warm facts read: %v", err)
		}
		readDurations = append(readDurations, time.Since(started))
	}
	run.WarmRead = factsPerfLatencyOf(readDurations)

	changeDurations := make([]time.Duration, 0, warm)
	for index := 0; index < warm; index++ {
		after := first.Cursor
		after.Sequence = 0
		started := time.Now()
		if _, err := store.Changes(t.Context(), auth, facts.ChangesRequest{After: after, Limit: 256}); err != nil {
			t.Fatalf("warm changes: %v", err)
		}
		changeDurations = append(changeDurations, time.Since(started))
	}
	run.WarmChanges = factsPerfLatencyOf(changeDurations)

	run.Contention = runFactsPerfContention(t, store, auth, subjects, contenders, validFrom)
	run.DatabaseBytesAfterSeed = factsPerfDatabaseBytes(t, store)

	rebuildStarted := time.Now()
	if err := store.RebuildFTS(t.Context()); err != nil {
		t.Fatalf("rebuild FTS: %v", err)
	}
	run.Rebuild = factsPerfLatencyOf([]time.Duration{time.Since(rebuildStarted)})
	run.DatabaseBytesAfterRebuild = factsPerfDatabaseBytes(t, store)
	run.FTSBytes, run.FTSSupported = factsPerfFTSBytes(t, store)

	samples := receipts
	if samples > 8 {
		samples = 8
	}
	cleanupDurations := make([]time.Duration, 0, samples)
	for index := 0; index < samples; index++ {
		started := time.Now()
		var receipt v1alpha1.ReceiptID
		if err := store.db.QueryRowContext(t.Context(), `SELECT receipt_id FROM receipts WHERE space_id='space-bot-a' ORDER BY received_at, receipt_id LIMIT 1 OFFSET ?`, index).Scan(&receipt); err != nil {
			t.Fatalf("resolve cleanup receipt %d: %v", index, err)
		}
		response, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
			ReceiptID: receipt, Reason: "performance harness cleanup", IdempotencyKey: fmt.Sprintf("facts-perf-delete-%08d", index),
		})
		if err != nil {
			t.Fatalf("managed cleanup %d: %v", index, err)
		}
		if response.Cleanup == managementv1alpha1.CleanupStateCompleted {
			run.CleanupCompleted++
		}
		cleanupDurations = append(cleanupDurations, time.Since(started))
	}
	run.CleanupSamples = samples
	run.Cleanup = factsPerfLatencyOf(cleanupDurations)

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	run.RSSBytesAfter = factsPerfRSSBytes()
	run.HeapAllocBytes = stats.HeapAlloc
	run.HeapSysBytes = stats.Sys
	return run
}

func runFactsPerfContention(t *testing.T, store *Store, auth v1alpha1.CallAuthorization, subjects []string, contenders int, validFrom time.Time) factsPerfContention {
	t.Helper()
	const writerOps = 200
	out := factsPerfContention{Readers: contenders, WriterOps: writerOps}
	var mu sync.Mutex
	var readDurations []time.Duration
	var errors int
	var writerDuration time.Duration
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for reader := 0; reader < contenders; reader++ {
		wg.Add(1)
		go func(reader int) {
			defer wg.Done()
			index := reader
			for {
				select {
				case <-stop:
					return
				default:
				}
				subject := subjects[(index*104729)%len(subjects)]
				started := time.Now()
				_, err := store.ReadFacts(t.Context(), auth, facts.ReadRequest{Subject: subject, Key: "perf.hot", Budget: facts.Budget{MaxFacts: 8, MaxBytes: 8192}})
				if err != nil {
					mu.Lock()
					errors++
					mu.Unlock()
				}
				mu.Lock()
				readDurations = append(readDurations, time.Since(started))
				out.ReaderOps++
				mu.Unlock()
				index++
			}
		}(reader)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		started := time.Now()
		for index := 0; index < writerOps; index++ {
			subject := fmt.Sprintf("perf-contended-%08d", index)
			request := facts.SubmitEvidenceRequest{
				Source: facts.Source{
					Producer: "facts-perf-contended", EventID: fmt.Sprintf("facts-perf-contended-%08d", index),
					Revision: "r1", Fragment: "f1", Subject: subject, FactKey: "perf.hot", Role: facts.RoleConfirmation,
				},
				Text:           factsPerfSourceText,
				IdempotencyKey: fmt.Sprintf("facts-perf-contended-%08d", index),
				Mutations: []facts.Mutation{{
					Transition: facts.TransitionEstablish, Subject: subject, Key: "perf.hot",
					Text: factsPerfSourceText, ValidFrom: &validFrom,
				}},
			}
			if _, err := store.SubmitEvidence(t.Context(), auth, request); err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
			}
		}
		writerDuration = time.Since(started)
		close(stop)
	}()
	wg.Wait()
	out.Errors = errors
	out.WriterMS = float64(writerDuration.Microseconds()) / 1000
	out.Latency = factsPerfLatencyOf(readDurations)
	return out
}

func factsPerfLatencyOf(durations []time.Duration) factsPerfLatency {
	out := factsPerfLatency{Samples: len(durations)}
	if len(durations) == 0 {
		return out
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	ms := func(value time.Duration) float64 { return float64(value.Microseconds()) / 1000 }
	out.MinMS = ms(sorted[0])
	out.MaxMS = ms(sorted[len(sorted)-1])
	out.P50MS = ms(sorted[len(sorted)*50/100])
	out.P95MS = ms(sorted[len(sorted)*95/100])
	out.P99MS = ms(sorted[len(sorted)*99/100])
	return out
}

func factsPerfDatabaseBytes(t *testing.T, store *Store) int64 {
	t.Helper()
	var pageCount, pageSize int64
	if err := store.db.QueryRowContext(t.Context(), `PRAGMA page_count`).Scan(&pageCount); err != nil {
		t.Fatalf("read page count: %v", err)
	}
	if err := store.db.QueryRowContext(t.Context(), `PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatalf("read page size: %v", err)
	}
	return pageCount * pageSize
}

func factsPerfFTSBytes(t *testing.T, store *Store) (int64, bool) {
	t.Helper()
	var bytes int64
	// dbstat is optional SQLite functionality; report unsupported instead of
	// inventing a projection size.
	if err := store.db.QueryRowContext(t.Context(), `SELECT COALESCE(SUM(pgsize),0) FROM dbstat WHERE name LIKE '%fts%'`).Scan(&bytes); err != nil {
		return 0, false
	}
	return bytes, true
}
