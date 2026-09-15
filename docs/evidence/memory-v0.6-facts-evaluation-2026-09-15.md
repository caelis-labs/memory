# Memory v0.6 Facts Evaluation Evidence — 2026-09-15

Status: **preliminary recorded run**, retained as historical evidence of the
M06 facts/evidence gate and a single-machine 1k/10k performance sample. See the
[final local candidate evidence](memory-v0.6-local-candidate.md) for source
attribution, later 100k measurements and the corrected B0/B1/C2 structural
comparison. The unrun arms below are unrun **inside this facts harness**, not a
claim that the separate comparison was never executed. This report contains no
source text, query text, receipt identity or data-directory path.

Method and ownership: [`docs/memory-v0.6-evaluation.md`](../memory-v0.6-evaluation.md).
Repo revision at measurement: `51693ff` (`v0.5.2` base plus uncommitted M06
work).

## Environment

- host: darwin/arm64, single machine;
- Go toolchain: `go version` from the workspace at measurement time;
- engine: `modernc.org/sqlite`, real on-disk files under `t.TempDir()`;
- model calls: 0. No model, Steward proposal, or provider is involved.

## Fixture identities

| Fixture | SHA-256 |
| --- | --- |
| `trajectories.json` | `17d938e1eab190d9a7e15ee89ccb13616f40d807784ffca00ad26a32415d8b93` |
| `candidates.json` | `c6c7649fefdca6bc917d5528ed7a28624413fb8360e72a649f7564ee08959715` |

Frozen thresholds: `explicit_preference_min = 0.95`, `recall_at8_alias_min =
0.90`.

## Command

```sh
make facts-gate
```

This runs `TestFactsLongitudinalEvaluationGate` with
`MEMORY_FACTS_EVAL_CANDIDATES=1`, writes an owner-only report to
`dist/facts-eval-report.json`, then runs `go run ./scripts/facts_eval` to
independently re-verify the fixtures, digests, labels, thresholds, and report.

## Measured arms

| Arm | Sample | Passed | Rate | Classification |
| --- | --- | --- | --- | --- |
| `C1-authored` | 14 trajectories | 14 | 1.00 | measured, structural conformance |
| `L0-legacy` | 2 baseline Recall probes | 2 | 1.00 | measured, control only |
| `C1-candidate` | 208 candidates | 208 | 1.00 | candidate tier only |
| `C3-candidate` | 448 alias queries | 448 | 1.00 | candidate tier only |

`C1-authored` covers scope and exact-LabelSet isolation, restart, source
idempotency and conflict, temporary exception with `as_of`, permanent change,
correction, denial, forget (cleansing, source suppression, no resurrection),
wrong subject, missing/stale target, untrusted role, ingestion policy, batch
atomicity, mixed-language explicit preference, read budgets, and recall-only
write rejection.

`L0-legacy` is the current in-tree model-free receipt `Recall` over the blocking
corpus. It is **not** exact v0.5.2 and is not a v0.5.2 result.

`C1-candidate` and `C3-candidate` are candidate-tier circular evidence. The
fixtures are expanded from the implementation's own controlled vocabulary, so a
1.00 rate confirms fixture/vocabulary/implementation agreement and nothing
about extraction, arbitrary paraphrase, model or consumer-answer quality.

## Unrun and blocked arms

| Arm | Status | Reason |
| --- | --- | --- |
| `B0-v0.5.2` | unrun | requires an exact v0.5.2 checkout; v0.5.2 has no facts/evidence API |
| `B1-steward` | unrun | requires the prior Steward revision with a fixed generator/profile/dataset binding |
| `C2-steward` | unrun | requires a scripted deterministic generator and the `B1-steward` comparison; structural selection only |
| `HUMAN-GOLD` | blocked | fewer than 200 human-reviewed fact trajectories exist |

**Release blocker (unchanged):** no facts/evidence quality claim is releasable
until at least 200 trajectories are human-reviewed. The candidate arms above do
not satisfy it.

## Performance sample (1000 and 10,000 receipts)

Command (defaults: 200 warm queries, 8 contenders):

```sh
GOWORK=off MEMORY_FACTS_PERF=1 MEMORY_FACTS_PERF_SIZES=1000,10000 \
  go test -count=1 -timeout 30m -run '^TestFactsPerformanceHarness$' ./internal/appliance
```

Single hot partition (`space-bot-a`), one confirmed fact per receipt.

| Metric | 1,000 receipts | 10,000 receipts |
| --- | --- | --- |
| Seed per receipt p50 / p95 / p99 | 0.62 / 1.04 / 2.31 ms | 1.75 / 6.54 / 7.91 ms |
| Seed total | 972 ms | 21,493 ms |
| Cold first `ReadFacts` after reopen | 0.29 ms | 0.27 ms |
| Cold first `Changes` after reopen | 0.06 ms | 0.07 ms |
| Warm `ReadFacts` p50 / p95 / p99 | 0.153 / 0.166 / 0.175 ms | 0.166 / 0.183 / 0.233 ms |
| Warm `Changes` p50 / p95 / p99 | 0.267 / 0.284 / 0.330 ms | 0.267 / 0.278 / 0.295 ms |
| Contention reads (8 readers, 200 writer ops) | 7,417 ops, 0 errors | 20,209 ops, 0 errors |
| Contention reader p50 / p95 / p99 | 0.591 / 0.921 / 1.178 ms | 0.564 / 0.873 / 1.107 ms |
| Contention writer total | 574 ms | 1,492 ms |
| `RebuildFTS` | 73.7 ms | 604.8 ms |
| Managed cleanup via `DeleteReceipt` (8 samples) | p50 2.32 ms / p95 2.60 ms; 8/8 completed | p50 15.58 ms / p95 16.13 ms; 8/8 completed |
| Database bytes after seed | 5,808,128 | 44,548,096 |
| Database bytes after rebuild | 5,988,352 | 45,895,680 |
| FTS page bytes (`dbstat`) | 720,896 | 5,058,560 |
| Process peak RSS before / after | 17,317,888 / 340,000,768 | 341,409,792 / 367,329,280 |
| Go heap alloc / sys after run | 156,853,288 / 332,888,408 | 163,268,208 / 347,044,184 |

The first seeded receipt includes lazy schema/FTS warmup and appears in
`seed max_ms` (294.6 ms at 1,000) rather than p50/p95. The 100,000-receipt size
was **not** run here and is not claimed; it remains available through
`FACTS_PERF_SIZES=1000,10000,100000 make facts-perf`. These numbers are one
machine, one run, and are not release thresholds.
