# Memory v0.6.0 — repaired local candidate evidence

Status: **local implementation candidate; not GA or a published release**.
This validation checkpoint was recorded after the user authorized local fixes
and independent AI-agent trajectory review in place of manual review, before
any commit, push, PR, tag or publication. The subsequent request to submit a PR
authorizes commit/push/PR delivery; tagging and publication remain separate.

## Candidate identity

- Base: `51693ff135be8c4149c15117980290aaad6d90da` (v0.5.2).
- Candidate metadata: `VERSION=0.6.0`; SQLite schema 2, marker `memory-v0.6.0`.
- Repaired source SHA-256:
  **`99037fb24c0f1fc64a5380a91ddcefac6287659cd85066fe9c66dcace0f4e8b1`**.
- The [177-file source manifest](memory-v0.6-candidate-source.json) covers all
  tracked/untracked non-ignored files except `docs/**` and the root `README.md`.
- The [reviewed delivery checkpoint](memory-v0.6-reviewed-delivery-snapshot.json)
  preserves the 233-file snapshot accepted before PR preparation, SHA-256
  `5324731030a5c74deefa3ed568aa1adba0907c0f0bf756d3ad2e4682cb4099b6`.
- The [submission delivery manifest](memory-v0.6-delivery-snapshot.json) includes
  the subsequent documentation-only authorization update, raw reports and review
  artifacts, excluding only itself. The 177-file source manifest is unchanged.

The earlier candidate had source SHA-256
`13e9e08428c10f6e30d258ca9a0e1771a9be34b2eb29306002ebcca9a5f34515`.
Its [source manifest](memory-v0.6-before-review-source.json),
[delivery manifest](memory-v0.6-before-review-delivery-snapshot.json),
[local evidence page](memory-v0.6-before-review-local-candidate.md), and raw
facts/comparison/performance/soak reports are retained with `before-review`
names. They are historical evidence, not results for the repaired source.
The even earlier [internal-gate snapshot](memory-v0.6-internal-gates-source.json)
is also historical.

## Five acceptance-review repairs

| Finding | Repair and regression coverage |
| --- | --- |
| Retried trusted source could return a forgotten derived payload | Check each returned Record against the durable barrier before reading its revision. Test pending cleanup, completed cleanup, reopen and an unaffected fact sharing the retained confirmation source. |
| Query matching could revive a condition-suppressed default | Resolve applicable conditions, exception suppression and equal-specificity ambiguity before query ranking/filtering/budget. Test unrelated default queries and each conflicting value. |
| Repeated exception corrections lost their base relation | Preserve exception identity via the related Record across corrections; enforce finite intervals, non-overlap and legal transitions. Test repeated correction, invalid atomic edits, expiry and denial. |
| Subject without fact key displaced relevant lexical context | Prioritize the exact subject/key only when both exist; use lexical candidates before the subject-only recency fallback. Test old relevant facts among 12 same-subject distractors and the persisted read set. |
| Cleared-revision count included ordinary empty fact JSON | Count only fully cleared revisions covered by a completed deletion barrier. Test normal, pending, completed and recovered states. |

The [original five counterexamples](memory-v0.6-review-reproduction_test.go.txt)
all failed on the previous candidate ([red output](memory-v0.6-review-red.txt))
and now pass unchanged through a temporary Go overlay
([green output](memory-v0.6-review-green.txt)). Extended owning coverage is in
[`facts_regression_test.go`](../../internal/appliance/facts_regression_test.go).
Two independent agents reviewed the repairs and related evaluation assertions;
no additional unresolved code finding was reported within that scope.

Comparing the before-review and repaired source manifests identifies exactly
ten changed paths: five production repair files, the new regression test, the
candidate assertion runner, and the trajectory fixture/manifest/README. API,
SDK, public facade, migration and performance-harness bytes are unchanged from
the original candidate; their full gates were repeated against the repairs.

The final integration-document review also closed two contract wording issues:
owner governance writes versus authorized observation, and fact denial versus
receipt deletion. It clarified Memory cursor scope versus host cache keys,
incremental invalidation, retained audit metadata and receipt/fact correction.
The independent reviewer rechecked these clarifications; this is documentation
alignment, not Caelis product acceptance.

## User-authorized independent trajectory review

| Corpus | Agent review | Execution after repair |
| --- | --- | --- |
| Expanded candidates | 208 accept; 26 templates × 8 isolated subjects | 208/208 current-fact checks; 448/448 controlled-alias checks |
| Authored blocking corpus | 14 accepted with documented coverage limits | 14/14 trajectories; strengthened corpus has 96 steps |
| Human review | 0; reviewer identity remains AI | No human-gold or production-quality claim |

The **substitute-review requirement is complete** under the user's explicit
instruction. The agents recorded per-case expectations, decisions, rationale,
source hashes and follow-up adjudications:

- [Candidate review](memory-v0.6-agent-candidate-review.md) and
  [208 detailed decisions](memory-v0.6-agent-candidate-review.json).
- [Blocking review](memory-v0.6-agent-blocking-review.md) and
  [14 detailed decisions](memory-v0.6-agent-blocking-review.json).

The review found two assertion gaps: the runner ignored frozen `expect_current`
labels and did not check pending invisibility/full provenance. Both are closed.
The runner now verifies those labels, no current fact/background before explicit
confirmation, exact subject/Space/Record/revision, and a duplicate-free complete
ReceiptID-to-Source mapping. The t03 denial trajectory also reads inside the
exception's effective interval. The fixture's frozen 0.95/0.90 thresholds and
zero human-review count were preserved.

The 208 expansions are only 26 semantic templates, each in a fresh database.
Controlled vocabulary selected the queries. This does not qualify natural
paraphrase retrieval, production extraction, background usefulness or final
consumer answers. See [method and limits](../memory-v0.6-evaluation.md).

## Environment and local gates

Darwin 25.6.0 arm64; Apple M4, 10 logical CPUs, 24 GiB RAM. Default Go 1.26.8;
the module's Go 1.25.8 floor was tested with CGO disabled. SQLite uses
`modernc.org/sqlite`. These checks require no production model or credentials.

| Command | Post-repair result |
| --- | --- |
| `make check` | PASS: Markdown links, Go formatting, text whitespace, full tests, vet, CLI build and diff check |
| `make race` | PASS: `GOWORK=off go test -race ./...` |
| `make durable` | PASS: uncached process/system tests, 20.575 s |
| `make facts-consumer-gate` | PASS: public-only imports in an independent temporary Go module |
| `make corpus-gate` | PASS: 224 multilingual cases, complete provenance, zero private leaks |
| `make facts-gate FACTS_EVAL_REPORT="$PWD/docs/evidence/memory-v0.6-facts-final.json"` | PASS: 14 blocking, 208 candidate and 448 controlled-alias checks; report checker passed |
| `GOWORK=off CGO_ENABLED=0 GOTOOLCHAIN=go1.25.8 go test ./...` | PASS: pure-Go module-floor full tests |
| `GOWORK=off CGO_ENABLED=0 go test ./...` | PASS: Go 1.26.8 pure-Go full tests |
| Windows amd64 / CGO=0 `go test -c ./appliance` and `go build ./cmd/...`, temporary outputs | PASS: cross-compilation only; no native Windows execution |
| Original five `TestReview` counterexamples, temporary overlay | PASS: 6.544 s; exact original reproductions retained |
| `make m5-benchmark` | PASS: 200 iterations per benchmark, 21.556 s |
| `make ga-soak GA_SOAK_REPORT="$PWD/docs/evidence/memory-v0.6-ga-soak.json"` | PASS: current-source 100k receipt run; details below |

[Recorded commands and verbatim combined output](memory-v0.6-review-gates.json)
retain successful exit status and per-log SHA-256.

Normal full test/race commands used Go's normal cache; changed packages reran.
Durable, facts, corpus, reproductions and performance use uncached execution.
The [post-repair regular-gate checkpoint](memory-v0.6-review-gates-source.json)
was `bbfaeeeafd99d06770c5d05bfc38902c8589bae7cceb2d1074ab61c811baba77`.
The regular gates preceded a final documentation-only change to
`internal/appliance/testdata/facts_eval/README.md`; all compiled code, tests and
JSON fixture inputs are byte-identical to the final source manifest. Soak and
the final performance rerun use that manifest directly. Root documentation
and evidence changes are captured separately by the delivery manifest.

The immutable v0.5.2 migration fixture remains SHA-256
`68e02a7031d05e7baa3d5271019911728c42a9ef3fbcba4658bf44500e9f7708`.
Migration tests cover rollback/reopen, evidence preservation, unknown legacy
onset/adoption, unsupported metadata and frozen v0.5.2 guard refusal of schema 2.
That last check executes frozen old guard logic, not an old release binary.
See [migration provenance](../memory-v0.6-migration.md).

## Current structural comparison

The [post-repair raw report](memory-v0.6-comparison-final.json) reruns the exact
v0.5.2 baseline and current tree with the same fixed profile and scripted
proposal bytes (SHA-256
`0190fe4302f47174bb7e27bb9e986a9a2eadbbce2744d9cc6e7f02e48cce3694`):

```sh
GOWORK=off go run ./scripts/facts_compare \
  -baseline-revision 51693ff135be8c4149c15117980290aaad6d90da \
  -unrelated 80 -output docs/evidence/memory-v0.6-comparison-final.json
```

After 80 unrelated Records, the previous Steward context omits the relevant
old coffee Record; current context ranks it first. B0 uses a separate store
with no Steward jobs/Records. Post-Steward Recall is labeled separately. Every
Recall arm is retrieval-only and does not answer or adopt a preference. Trusted
subject/key context is a current-only supplemental arm. This measures structure,
not model answer quality.

The comparison's `current_worktree_sha256` fingerprints Git status/diffs and
untracked path names, not all untracked bytes. Use the content-addressed source
manifest above for candidate identity. The facts harness still marks B0/B1/C2
unrun inside that harness; this separate script supplies structural evidence.

## Current-source durability/load and legacy benchmark

[Raw soak report](memory-v0.6-ga-soak.json): **100 Spaces, 100,000 Receipts,
10,000 Records**; 100,000 status checks before and after restore; zero private
leaks on both sides. Both source and restored state show 10,000 completed jobs,
zero pending jobs, schema 2 and healthy projections. Durations: organization
238,519 ms; baseline Remember 38,667 ms; backup 2,577 ms; rebuild 3,278 ms;
restore plus rebuild 20,498 ms. This uses a deterministic worker, not a model.

M5 measured Remember p95 354 µs, cold Remember p95 483 µs, Recall p95 408 µs,
Open p95 24,347 µs, backup p95 8,250 µs, restore p95 33,914 µs, rebuild
61,449 entries/s and deterministic backlog recovery 1,300 jobs/s. These are
local samples, not new guarantees.

## Current-source performance qualification

The complete repaired 1k/10k/100k confirmation run **passed all 33 frozen checks**, with complete
sample counts and supported FTS/RSS measurements. Soak, full tests and M5 had
finished before this run began. The raw report finished at `2026-09-15T14:12:51Z`.

```sh
FACTS_PERF_SIZES=1000,10000,100000 \
  FACTS_PERF_REPORT="$PWD/docs/evidence/memory-v0.6-perf-final.json" make facts-perf
```

[Raw report](memory-v0.6-perf-final.json), SHA-256 `e18e0610f3d5bb7349b47a6b62a05dd70ea801e9ed352cb87037798c230362bd`;
[measured-value comparison](memory-v0.6-performance-check.json). The
[frozen limits](memory-v0.6-performance-limits.json) remain byte-identical,
SHA-256 `813486e534ca2c94c81dd5356fd0870faccebc09b5684ca9b6fd665327f2338d`.
Their old source metadata belongs to the original freeze; the new comparison
records the repaired source and current raw-report hash. No limit was relaxed.

All latency values are milliseconds unless stated otherwise.

| Metric | 1,000 Receipts | 10,000 Receipts | 100,000 Receipts | Frozen upper bound |
| --- | --- | --- | --- | --- |
| Seed p50 / p95 / p99 | 0.644 / 1.140 / 2.407 | 1.746 / 3.607 / 7.520 | 12.665 / 27.392 / 32.817 | p95: 5 / 15 / 50 |
| Seed total | 1,047.783 | 19,589.525 | 1,438,742.039 | observational |
| First ReadFacts after reopen | 0.300 | 0.300 | 0.579 | 50 |
| First Changes after reopen | 0.065 | 0.067 | 0.072 | observational |
| Warm ReadFacts p95 | 0.197 | 0.191 | 0.447 | 10 |
| Warm Changes p95 | 0.298 | 0.823 | 0.293 | 10 |
| Contention reader p95 | 0.994 | 0.955 | 0.858 | 20 |
| Contention reader operations / errors | 7,527 / 0 | 19,532 / 0 | 116,686 / 0 | errors: 0 |
| RebuildFTS, one sample | 74.416 | 615.892 | 6,069.356 | 15,000 |
| Managed cleanup p95, eight samples | 4.337 | 34.225 | 348.909 | 500 |
| Cleanup completed / requested | 8 / 8 | 8 / 8 | 8 / 8 | all complete |
| Database bytes after rebuild | 5,971,968 | 45,899,776 | 450,904,064 | 629,145,600 |
| FTS bytes | 720,896 | 5,058,560 | 65,126,400 | 104,857,600 |
| Process peak RSS bytes | 332,906,496 | 359,219,200 | 424,574,976 | 536,870,912 |

### Earlier complete run and fixed diagnostic repeats

The [first full post-repair run](memory-v0.6-perf-review-full-attempt-1.json)
passed [32/33 checks](memory-v0.6-performance-review-full-attempt-1-check.json):
1k seed p95 was **8.270 ms**, above the unchanged **5 ms** bound. All 10k/100k
checks passed. Three sequential 1k diagnostics were declared before execution:
[run 1](memory-v0.6-perf-review-1k-diagnostic-1.json),
[run 2](memory-v0.6-perf-review-1k-diagnostic-2.json), and
[run 3](memory-v0.6-perf-review-1k-diagnostic-3.json) measured seed p95
**1.149 / 1.206 / 1.144 ms**. The cause of the first overrun is not established.

One further full run was then declared and executed with unchanged source,
fixtures and limits; its complete result is the table above. Values were not
assembled across runs, and every failed/diagnostic report remains available.
The latest pass does not qualify repeatability or remove the observed variance.

Measurement limits:

- One hot Space/LabelSet with one fact per receipt and distinct subjects, not
  100k facts for a single subject or a 100k-fact background scan.
- Cold means SQLite reopen, not OS-cache flushing. Two hundred warm queries,
  eight readers and 200 competing writes are exercised at each size.
- Rebuild has one sample and cleanup eight; these are not statistical tail
  confidence estimates. RSS is process peak residency across sequential runs.
- This is a shared desktop observation, not an isolated performance lab or a
  portable SLO. Model calls, tokens, model backlog and answer quality are unmeasured.

## Host boundary and remaining GA work

Caelis was inspected read-only at
`ef6c4697bdf5af18db141b7d4b1eff4f28c3bbc7`. An intermediate read-only status check
found a clean worktree at `f6514aa09cd26e8853e4a6d2420125ac7cb77acd`; the
final read-only check found it clean at
`40a79a68fd85f32dafd82419289f2c8593734a01`. Its HEAD advanced outside this repair. This task did not modify Caelis or requalify
its newer revision. The
[Caelis integration worklist](../memory-v0.6-caelis-integration.md) remains a
separate product task. The public consumer gate validates Memory API usability;
it does not implement Caelis history/replay/cache/context invalidation or backup
reconciliation. No Bot work was done.

Remaining qualification and delivery:

1. Freeze a production-representative model/profile/holdout and evaluate
   extraction, natural paraphrase retrieval, background and final answers.
2. Exact-candidate remote CI, especially native Windows embedded Open. Local
   Darwin execution and Windows cross-compilation do not supply that evidence.
3. Caelis product integration and lifecycle/deletion coordination acceptance.
4. Tagging and publication after qualification and separate authorization.
   Candidate commit/push/PR delivery was authorized after this validation checkpoint.

Managed forgetting invalidates managed state and cleans owned payloads. It
cannot retract already-sent model context, historical Caelis copies, old backups
or exports. Opaque audit/control identifiers and owner reasons remain. See
[release boundaries and checklist](../memory-v0.6-release.md).
