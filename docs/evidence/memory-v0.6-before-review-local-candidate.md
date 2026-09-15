> Historical before-review narrative. Its results and review blockers are
> superseded by the [repaired candidate evidence](memory-v0.6-local-candidate.md).
> Narrative/command text is retained; Markdown report links below now point to
> the corresponding archived reports. Original manifests retain their original
> file paths and hashes and do not describe the repaired worktree.

# Memory v0.6.0 — final local candidate evidence

Status: **local implementation candidate, not GA or a published release**.
This evidence covers an uncommitted working tree; it is not an exact-candidate
remote CI result. No commit, push, PR, tag or publication was performed.

## Candidate identity and attribution

- Base: `51693ff135be8c4149c15117980290aaad6d90da` (v0.5.2).
- Candidate metadata: `VERSION=0.6.0`; SQLite schema 2, marker `memory-v0.6.0`.
- Final source snapshot: **`13e9e08428c10f6e30d258ca9a0e1771a9be34b2eb29306002ebcca9a5f34515`**,
  [176-file manifest](memory-v0.6-before-review-source.json).
- Earlier internal-gate snapshot:
  `eefea4171f425e7f85d385f4c343d208632f0c02a2df22e1172d6c50ae9a4e3a`,
  [174-file manifest](memory-v0.6-internal-gates-source.json).
- These source manifests exclude `docs/**` and `README.md`. The
  [delivery snapshot](memory-v0.6-before-review-delivery-snapshot.json) includes source,
  documentation and reports, excluding only itself and Git-ignored files.

The final source delta after the internal gates is exactly seven files:
`appliance/facts.go`, `appliance/runtime.go`, `appliance/planes.go`,
`appliance/planes_public_test.go`, and
`scripts/facts_compare/{main.go,main_test.go,templates.go}`. The facade now
returns narrow forwarding adapters, so a facts/data-plane recipient cannot
recover the owner management interface by type assertion. This is method-set
isolation, not a sandbox for arbitrary Go code. The comparison script now gives
B0 a separate no-Steward store and distinguishes post-Steward Recall from B0.

All internal appliance, API, SDK, performance and soak-harness inputs are
identical between the two manifests. The costly soak, performance and legacy
benchmark runs are therefore attributed to the earlier snapshot, not falsely
presented as rebuilt on the final snapshot. The final facade and script changes
were covered by the final full local gates below.

## Environment

Darwin 25.6.0 arm64, Apple M4, 10 logical CPUs, 24 GiB RAM. Default toolchain:
Go 1.26.8; the module's Go 1.25.8 floor was also tested with CGO disabled.
SQLite uses `modernc.org/sqlite`; no external service, embedding, vector/graph
DB, model download or model invocation was required.

## Local verification

### Final source snapshot

| Command | Result |
| --- | --- |
| `make check` | PASS: Markdown links, Go formatting, text whitespace, `go test ./...`, vet, CLI build and `git diff --check` |
| `make race` | PASS: `GOWORK=off go test -race ./...` |
| `make durable` | PASS: uncached process/system tests, 15.739 s |
| `make facts-consumer-gate` | PASS: public-only imports in a separate temporary Go module, `GOWORK=off`; temporary module removed |
| `GOWORK=off CGO_ENABLED=0 GOTOOLCHAIN=go1.25.8 go test ./...` | PASS: module toolchain floor and pure-Go execution |

Normal Go test caching was enabled for the full test/race commands; changed
packages reran. The durable and explicit evaluation/performance gates use
`-count=1`. Targeted tests include migration rollback/reopen, exact scope and
subject boundaries, source retry/suppression, adoption and historical validity,
exception budget handling, history pagination, concurrent caller-request
immutability, actual Steward read dependencies, batch atomicity, transitive
forgetting beyond 64 hops, cleanup recovery, scheduler fractional timestamps,
and public facade method-set isolation.

### Unchanged internal snapshot

| Command | Result |
| --- | --- |
| `make check`, `make race`, `make durable` | PASS before the final facade/comparison delta; repeated above |
| `make corpus-gate` | PASS: 224 multilingual cases, full provenance, zero private leaks |
| `make facts-gate FACTS_EVAL_REPORT="$PWD/docs/evidence/memory-v0.6-facts-final.json"` | PASS: blocking 14/14, candidates 208/208, controlled aliases 448/448; report checker passed |
| `GOWORK=off CGO_ENABLED=0 go test ./...` | PASS on Go 1.26.8 |
| `make ga-soak GA_SOAK_REPORT="$PWD/docs/evidence/memory-v0.6-ga-soak.json"` | PASS: real SQLite durability/backup/restore and deterministic worker load |
| `make m5-benchmark` | PASS: legacy latency, rebuild and deterministic backlog samples |
| Windows amd64, CGO=0: `go test -c ./appliance` and `go build ./cmd/...`, outputs in system temporary storage | PASS **cross-build only**; artifacts removed, no Windows execution |

The immutable v0.5.2 migration fixture has SHA-256
`68e02a7031d05e7baa3d5271019911728c42a9ef3fbcba4658bf44500e9f7708`.
Migration tests cover evidence preservation, unknown legacy onset/adoption,
transactional rollback and restart, unsupported metadata, and the frozen
v0.5.2 schema guard's refusal of schema 2. The refusal evidence is a test of
frozen old guard logic, **not an executed old release binary**. See
[migration provenance](../memory-v0.6-migration.md).

## Evaluation: conformance is not human quality

[Raw facts report](memory-v0.6-facts-before-review.json): 14 authored blocking
trajectories, 208 machine-expanded candidates and 448 controlled-alias checks
all pass. **Human-reviewed cases: 0; required for GA: at least 200.** Candidate
fixtures are derived from the controlled vocabulary and are circular evidence;
they do not establish natural paraphrase, extraction or consumer-answer quality.
The frozen 0.95 preference and 0.90 alias Recall@8 targets have not been qualified
on a human-reviewed holdout.

[Corrected structural comparison](memory-v0.6-comparison-before-review.json) was run
with:

```sh
go run ./scripts/facts_compare \
  -baseline-revision 51693ff135be8c4149c15117980290aaad6d90da \
  -unrelated 80 -output docs/evidence/memory-v0.6-comparison-final.json
```

Both sides use the same fixture, profile and scripted proposal bytes (SHA-256
`0190fe4302f47174bb7e27bb9e986a9a2eadbbce2744d9cc6e7f02e48cce3694`).
B0 separately asserts no Steward jobs or Records and measures receipt-only
Recall. B1's prior recency-only context omits the old relevant coffee Record
after 80 unrelated Records; C2 offers it at rank 1. Post-Steward Recall is
separately named. Every Recall arm is retrieval-only and does **not** answer or
adopt a preference. Trusted-source subject/key selection at rank 1 is a
current-only supplemental arm, not a baseline capability.

The comparison report's `current_worktree_sha256` fingerprints Git status,
tracked diffs and untracked path names, not untracked file contents. It is not
a content-addressed source manifest; use the complete candidate and internal
manifests above for file-content identity.

The raw facts report intentionally retains B0/B1/C2 as unrun **inside that
harness**; the separate comparison supplies structural evidence only. The
[earlier structural note](memory-v0.6-facts-structural-comparison-2026-09-15.md)
retains its supersession notice and must not be used for its former B0 label.

## Performance qualification

The final 1k/10k/100k rerun **passed** (test duration 1,411.93 s; finished
`2026-09-15T12:10:27Z`):

```sh
FACTS_PERF_SIZES=1000,10000,100000 \
  FACTS_PERF_REPORT="$PWD/docs/evidence/memory-v0.6-perf-final.json" make facts-perf
```

[Raw report](memory-v0.6-perf-before-review.json), SHA-256
`fb503290104ec3eb7432712b416b1689069de49a19b7129c982812abaaebb4c4`.
Direct `measured <= upper_bound` comparison against every
[frozen hardware-bound limit](memory-v0.6-performance-limits.json) passed
**33/33 checks**, with complete sample counts, cleanup completion and supported
FTS/RSS measurements validated. The
[comparison report](memory-v0.6-performance-before-review-check.json) records each value,
unchanged limit and the hashes of both input files. Limits were not relaxed.

All latency values below are milliseconds unless otherwise stated:

| Metric | 1,000 Receipts | 10,000 Receipts | 100,000 Receipts | Frozen upper bound |
| --- | --- | --- | --- | --- |
| Seed p50 / p95 / p99 | 0.621 / 1.092 / 2.457 | 1.700 / 2.906 / 7.090 | 12.462 / 26.198 / 29.736 | p95: 5 / 15 / 50 by size |
| Seed total | 1,002.490 | 18,382.942 | 1,370,839.758 | observational |
| First ReadFacts after reopen | 0.298 | 0.315 | 0.375 | 50 |
| First Changes after reopen | 0.065 | 0.071 | 0.069 | observational |
| Warm ReadFacts p95 | 0.156 | 0.166 | 0.419 | 10 |
| Warm Changes p95 | 0.271 | 0.265 | 0.291 | 10 |
| Contention reader p95 | 1.013 | 1.017 | 0.871 | 20 |
| Contention reader operations / errors | 7,483 / 0 | 19,903 / 0 | 118,437 / 0 | errors: 0 |
| RebuildFTS, one sample | 67.531 | 562.870 | 6,016.266 | 15,000 |
| Managed cleanup p95, eight samples | 4.273 | 33.591 | 342.044 | 500 |
| Cleanup completed / requested | 8 / 8 | 8 / 8 | 8 / 8 | all complete |
| Database bytes after rebuild | 5,988,352 | 45,862,912 | 450,813,952 | 629,145,600 |
| FTS bytes | 720,896 | 5,058,560 | 65,126,400 | 104,857,600 |
| Process peak RSS bytes | 341,196,800 | 369,803,264 | 433,176,576 | 536,870,912 |

The [initial 100k baseline](memory-v0.6-perf-100k.json) is retained separately:
it began before final internal governance hardening and is not the final run.
No performance optimization was introduced between the baseline and rerun.
Final 100k cleanup p95 is 342.044 ms versus the earlier 186.071 ms; the stronger
managed closure is not presented as a speedup, and the final value remains
below the independently frozen 500 ms bound.

Measurement boundaries:

- One hot Space/LabelSet, one explicit fact per receipt, **distinct subjects**.
  This is not 100k facts for a single subject or a 100k-fact background scan.
- Cold means first read after SQLite reopen, not an OS-cache or dictionary flush.
- 200 warm queries, eight competing readers and 200 writes; rebuild is one sample
  and cleanup eight samples, not statistically qualified tail latency.
- Some local gates ran concurrently with seeding. This is an observational local
  hardware sample, not an isolated performance lab or a hardware-independent SLO.
- Model calls are zero. Production model latency/tokens, model-driven backlog,
  invalid organization rate, extraction and answer quality remain unmeasured.

### Durability/load and legacy benchmark

[GA-soak report](memory-v0.6-ga-soak-before-review.json): **100 Spaces, 100,000 Receipts,
10,000 active Records**, 100,000 receipt-status checks both before and after
restore, and zero private leaks on both sides. All 10,000 jobs completed;
pending jobs were zero and both projections were healthy at schema 2.
Recorded durations: organization 222,494 ms; baseline Remember 39,567 ms;
backup 2,031 ms; rebuild 2,665 ms; restore plus rebuild 15,601 ms. This validates
deterministic worker durability, not production model behavior.

`make m5-benchmark` (200 iterations) measured legacy Remember p95 311 µs,
cold Remember p95 381 µs, Recall p95 336 µs, Open p95 25,333 µs, backup p95
7,696 µs, restore p95 33,099 µs, rebuild 64,458 entries/s and deterministic
backlog recovery 1,408 jobs/s. These are measured samples, not new guarantees.

## Host boundary and remaining GA gates

Caelis remained clean and unchanged at
`ef6c4697bdf5af18db141b7d4b1eff4f28c3bbc7`. The
[read-only audit and integration worklist](../memory-v0.6-caelis-integration.md)
identify concrete Memory host, tool, replay/history, cache and backup changes.
The external consumer harness demonstrates Memory's public API use; it does
not implement or qualify the Caelis product integration. No Bot work was done.

Still required:

1. At least 200 genuinely human-reviewed trajectories and a frozen,
   production-representative model/profile/holdout answer evaluation.
2. Exact-candidate remote CI, including native Windows embedded Open. Darwin
   tests and Windows cross-compilation are not native Windows evidence.
3. Separate Caelis integration and product-level lifecycle, context invalidation,
   replay/history and backup/deletion reconciliation acceptance.
4. Human review, an authorized candidate commit and separate authority for
   push/PR/tag/publication. There is no final release commit or artifact here.

Managed forgetting is logical invalidation and owned-payload cleansing, not
forensic erasure. Existing snapshots, model contexts, old backups, exports and
Caelis histories cannot be retroactively retracted. Audit/control identifiers
and owner reasons remain and must be opaque/content-free. Legacy attribution
may conservatively cleanse additional derived state within the same exact
partition. See the [release boundaries and checklist](../memory-v0.6-release.md).
