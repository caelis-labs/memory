package appliance

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func BenchmarkM5Remember(b *testing.B) {
	store, auth := newGoldenStore(b, b.TempDir(), time.Now)
	b.Cleanup(func() { _ = store.Close() })
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for index := range b.N {
		started := time.Now()
		if _, err := store.Remember(b.Context(), auth, v1alpha1.RememberRequest{
			Text:           fmt.Sprintf("M5 durable Remember marker%08d", index),
			IdempotencyKey: fmt.Sprintf("m5-remember-%08d", index),
		}); err != nil {
			b.Fatal(err)
		}
		durations = append(durations, time.Since(started))
	}
	b.StopTimer()
	if p99 := reportPercentiles(b, "remember", durations); p99 > 5*time.Millisecond {
		b.Fatalf("Remember p99 %s exceeds 5ms RC budget", p99)
	}
}

func BenchmarkM5Recall(b *testing.B) {
	store, auth := newGoldenStore(b, b.TempDir(), time.Now)
	b.Cleanup(func() { _ = store.Close() })
	const corpusSize = 200
	for index := range corpusSize {
		if _, err := store.Remember(b.Context(), auth, v1alpha1.RememberRequest{
			Text:           fmt.Sprintf("M5 Recall baseline lookup%03d", index),
			IdempotencyKey: fmt.Sprintf("m5-recall-%03d", index),
		}); err != nil {
			b.Fatal(err)
		}
	}
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for index := range b.N {
		started := time.Now()
		response, err := store.Recall(b.Context(), auth, testRecall(fmt.Sprintf("lookup%03d", index%corpusSize), ""))
		if err != nil || len(response.Fragments) != 1 {
			b.Fatalf("Recall = %+v, %v", response, err)
		}
		durations = append(durations, time.Since(started))
	}
	b.StopTimer()
	if p99 := reportPercentiles(b, "recall", durations); p99 > 5*time.Millisecond {
		b.Fatalf("Recall p99 %s exceeds 5ms RC budget", p99)
	}
}

func BenchmarkM5StartupReadiness(b *testing.B) {
	root := b.TempDir()
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for index := range b.N {
		started := time.Now()
		store, err := Open(b.Context(), Options{DataDir: filepath.Join(root, fmt.Sprintf("startup-%08d", index))})
		if err != nil {
			b.Fatal(err)
		}
		if err := store.Ready(b.Context()); err != nil {
			b.Fatal(err)
		}
		durations = append(durations, time.Since(started))
		if err := store.Close(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if p99 := reportPercentiles(b, "startup", durations); p99 > 150*time.Millisecond {
		b.Fatalf("startup p99 %s exceeds 150ms RC budget", p99)
	}
}

func BenchmarkM5Reindex(b *testing.B) {
	store, auth := newGoldenStore(b, b.TempDir(), time.Now)
	b.Cleanup(func() { _ = store.Close() })
	const entries = 500
	for index := range entries {
		if _, err := store.Remember(b.Context(), auth, v1alpha1.RememberRequest{
			Text: fmt.Sprintf("M5 reindex entry%03d", index), IdempotencyKey: fmt.Sprintf("m5-reindex-%03d", index),
		}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	started := time.Now()
	for range b.N {
		if err := store.RebuildFTS(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(started)
	b.StopTimer()
	throughput := float64(entries*b.N) / elapsed.Seconds()
	b.ReportMetric(throughput, "entries/s")
	if throughput < 50_000 {
		b.Fatalf("reindex throughput %.0f entries/s is below 50000 entries/s RC budget", throughput)
	}
}

func BenchmarkM5Backup(b *testing.B) {
	store, auth := newGoldenStore(b, b.TempDir(), time.Now)
	b.Cleanup(func() { _ = store.Close() })
	for index := range 200 {
		if _, err := store.Remember(b.Context(), auth, v1alpha1.RememberRequest{
			Text: fmt.Sprintf("M5 backup entry%03d", index), IdempotencyKey: fmt.Sprintf("m5-backup-%03d", index),
		}); err != nil {
			b.Fatal(err)
		}
	}
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for range b.N {
		started := time.Now()
		snapshot, err := store.CreateBackupSnapshot(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		durations = append(durations, time.Since(started))
		if err := snapshot.Close(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if p99 := reportPercentiles(b, "backup", durations); p99 > 100*time.Millisecond {
		b.Fatalf("backup p99 %s exceeds 100ms RC budget", p99)
	}
}

func BenchmarkM5Restore(b *testing.B) {
	source, auth := newGoldenStore(b, filepath.Join(b.TempDir(), "source"), time.Now)
	for index := range 200 {
		if _, err := source.Remember(b.Context(), auth, v1alpha1.RememberRequest{
			Text: fmt.Sprintf("M5 restore entry%03d", index), IdempotencyKey: fmt.Sprintf("m5-restore-%03d", index),
		}); err != nil {
			b.Fatal(err)
		}
	}
	credential, err := os.ReadFile(source.ManagementCredentialPath())
	if err != nil {
		b.Fatal(err)
	}
	snapshot, err := source.CreateBackupSnapshot(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	snapshotBytes, err := io.ReadAll(snapshot)
	if err != nil {
		b.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		b.Fatal(err)
	}
	if err := source.Close(); err != nil {
		b.Fatal(err)
	}
	root := b.TempDir()
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for index := range b.N {
		started := time.Now()
		if _, err := Restore(b.Context(), RestoreOptions{
			DataDir:  filepath.Join(root, fmt.Sprintf("restore-%08d", index)),
			Snapshot: bytes.NewReader(snapshotBytes), ManagementCredential: strings.TrimSpace(string(credential)),
		}); err != nil {
			b.Fatal(err)
		}
		durations = append(durations, time.Since(started))
	}
	b.StopTimer()
	if p99 := reportPercentiles(b, "restore", durations); p99 > 250*time.Millisecond {
		b.Fatalf("restore p99 %s exceeds 250ms RC budget", p99)
	}
}

func BenchmarkM5BacklogRecovery(b *testing.B) {
	store, auth := newGoldenStore(b, b.TempDir(), time.Now)
	b.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(b, store, 1)
	for index := range b.N {
		if _, err := store.Remember(b.Context(), auth, v1alpha1.RememberRequest{
			Text: fmt.Sprintf("M5 backlog entry%08d", index), IdempotencyKey: fmt.Sprintf("m5-backlog-%08d", index),
		}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	started := time.Now()
	for range b.N {
		work, found, err := store.ClaimStewardJob(b.Context(), time.Minute)
		if err != nil || !found {
			b.Fatalf("Claim found=%v error=%v", found, err)
		}
		if _, err := store.ApplyStewardProposal(b.Context(), work.Lease, stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore}); err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(started)
	b.StopTimer()
	throughput := float64(b.N) / elapsed.Seconds()
	b.ReportMetric(throughput, "jobs/s")
	if throughput < 500 {
		b.Fatalf("backlog throughput %.0f jobs/s is below 500 jobs/s RC budget", throughput)
	}
}

func reportPercentiles(b *testing.B, prefix string, durations []time.Duration) time.Duration {
	b.Helper()
	if len(durations) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var p99 time.Duration
	for _, percentile := range []int{50, 95, 99} {
		index := (len(sorted)*percentile + 99) / 100
		if index > 0 {
			index--
		}
		value := sorted[index]
		b.ReportMetric(float64(value.Microseconds()), prefix+"_p"+fmt.Sprint(percentile)+"_us")
		if percentile == 99 {
			p99 = value
		}
	}
	return p99
}
