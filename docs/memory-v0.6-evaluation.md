# Memory v0.6 Facts Evaluation

Status: normative method for the M06 facts/evidence longitudinal evaluation and
performance harness. It describes the checked-in gate in
`internal/appliance/facts_evaluation_test.go` and
`internal/appliance/facts_eval_candidates_test.go`, the fixtures under
`internal/appliance/testdata/facts_eval`, the offline conformance checker in
`scripts/facts_eval`, and the opt-in performance harness in
`internal/appliance/facts_performance_test.go`.

The owning behavior is `internal/appliance/facts_read.go`,
`facts_ingest.go`, `facts_lifecycle.go`, and `facts_changes.go`. This document
owns methodology and evidence classification, not the facts contract itself.
The wire types remain owned by `api/memory/facts/v1alpha1`. Memory stays free of
Caelis product types and this harness calls no model.

## What this evaluation is

The gate runs authored, deterministic, longitudinal trajectories against real
on-disk SQLite through the real `Store` facts, evidence and management APIs,
plus one machine-expanded candidate set. It measures structural facts
semantics: scope and privacy, restart durability, lifecycle transitions,
forgetting and anti-resurrection, source admission, and controlled-alias
retrieval.

It does not measure:

- extraction or interpretation quality;
- arbitrary free-text paraphrase recall (only the frozen controlled alias
  dictionary is exercised);
- model or Steward proposal quality;
- consumer-answer quality.

## Tier nomenclature

Only `measured` arms carry a rate. `unrun` and `blocked` arms are never reported
as results.

| Arm | Meaning | Status |
| --- | --- | --- |
| `C1-authored` | Authored blocking structured-lifecycle trajectories | measured |
| `L0-legacy` | In-tree model-free receipt `Recall` control over the blocking corpus. **Not** exact v0.5.2 | measured (control) |
| `B0-v0.5.2` | Exact v0.5.2 receipt-only Recall with no Steward binding, job or Record | measured structurally by `scripts/facts_compare`; unrun in the facts gate below |
| `B1-steward` | Prior Steward-organized Recall and context selection on the same fixed profile/dataset | measured structurally by `scripts/facts_compare`; model quality unrun |
| `C2-steward` | Relevant Steward context versus `B1-steward` | context selection measured structurally; model/extraction quality unrun |
| `C1-candidate` | 208 machine-expanded structured adoption trajectories | candidate tier only |
| `C3-candidate` | Controlled-alias `Recall@8` over the candidate set | candidate tier only |
| `HUMAN-GOLD` | Human-reviewed quality | blocked |

`B0-v0.5.2` and `B1-steward` are **unrun inside `make facts-gate`** because
that gate does not archive a baseline: v0.5.2 has no facts/evidence API. They are
measured structurally by `scripts/facts_compare`, which archives the exact
baseline revision with `git archive` and runs the same fixture, profile and
scripted deterministic proposal bytes on both sides. See
[Structural baseline comparison](#structural-baseline-comparison). The
`L0-legacy` control isolates the legacy receipt retrieval path in the current
tree; it is never a v0.5.2 result.

`C2-steward` model/extraction quality remains unrun and GA-blocked. The
structural comparison measures **context selection only**: whether the relevant
older Record is offered to the worker. A deterministic scripted generator is
acceptable and needs no model tokens, but it cannot show that a model uses the
context well, and no extraction or paraphrase quality is claimed.

## Measured method

Each blocking trajectory gets its own temporary SQLite database and a fixed
clock (`2026-09-01T00:00:00Z`). Trajectories drive only public or owning APIs:

- `SetIngestionPolicy` and `SubmitEvidence` with explicit mutations;
- `ReadFacts`, `FactHistory`, and `Changes`;
- `DeleteReceipt`, `ListRecords`, `TraceRecord` from the management plane;
- `RebuildFTS`;
- `Close` + `Open` for restart.

The candidate set expands each of 26 templates over 8 subjects to 208
trajectories, each in its own temporary database. Every candidate performs
establish (user quote) → confirm (structured confirmation) → optional change,
then checks the C1 structured-adoption arm (one confirmed current fact whose text
equals the frozen, independently reviewed `expect_current` label) and the C3 arm (the current fact appears
within the first 8 facts for each controlled alias query). The runner also rejects
any current background before confirmation and checks exact admitted subject,
Space, Record/revision and Receipt-to-Source attribution after confirmation/change.

Execution reports contain only aggregate results, fixture digests and qualification
limits. The separate agent-review artifacts retain the synthetic per-case inputs,
expected results and adjudication rationale so the review can be inspected.

## Frozen thresholds

`explicit_preference_min = 0.95` and `recall_at8_alias_min = 0.90` are recorded
in `manifest.json` before measurement. Both the Go gate and `scripts/facts_eval`
hold an independent floor, so lowering the manifest alone cannot produce a
passing result.

## Candidate circularity and authorized independent review

The candidate set is derived from the implementation's own controlled
vocabulary. A high candidate-tier rate is therefore circular: it confirms that
the fixture, the controlled alias dictionary, and the implementation agree, and
nothing more. The candidate arms are labeled `CANDIDATE TIER ONLY` in the
report, and `scripts/facts_eval` fails a report that presents them as
human-reviewed gold.

On 2026-09-15 the user explicitly authorized independent AI-agent review in
place of the originally requested manual trajectory review. Two separate agents
reviewed the 208 expanded candidates and 14 authored trajectories, preserving
per-case rationale, input hashes, findings and follow-up adjudication:

- [208 candidate decisions](evidence/memory-v0.6-agent-candidate-review.json)
  and [review summary](evidence/memory-v0.6-agent-candidate-review.md);
- [14 blocking decisions](evidence/memory-v0.6-agent-blocking-review.json)
  and [review summary](evidence/memory-v0.6-agent-blocking-review.md).

The **user-authorized substitute review is complete**. `human_reviewed_cases`
remains 0: the reviewer identity is accurately recorded as an independent AI
subagent, not a human annotator. The 208 expansions contain only 26 semantic
templates, with isolated stores; this is not 208 independent situations or a
held-out production sample. The review closed two harness gaps (frozen labels
and pending/provenance assertions) and strengthened t03 with a denied-exception
read inside its former valid interval. It did not change the frozen 0.95/0.90
thresholds.

**Remaining quality gate:** production-representative extraction, natural
paraphrase, background and final answers on a separately qualified holdout.
Neither controlled candidate scores nor the substitute review establishes those
quality targets. `HUMAN-GOLD` remains explicitly unqualified in raw reports;
it is not a claim that the authorized substitute-review task is unfinished.

## Structural baseline comparison

`scripts/facts_compare` answers the `B0`/`B1`/`C2` structural question without a
model. It archives the exact baseline revision with `git archive` into the
system temporary directory, copies the current working tree, injects one fixture
and one generated test into both, and runs `go test` in each tree:

```sh
go run ./scripts/facts_compare \
  -baseline-revision 51693ff135be8c4149c15117980290aaad6d90da \
  -unrelated 80 -output docs/evidence/memory-v0.6-comparison-final.json
```

Experiment: one old coffee Record, then 80 newer unrelated Records, then a
related tea/coffee probe. Both sides use the legacy `Remember` path (the
baseline has no facts/evidence API), the same profile, and byte-identical
scripted deterministic proposals, so this is not an ingestion-advantage test.

Measured structural result (corrected nomenclature; final report in
[`docs/evidence/memory-v0.6-comparison-final.json`](evidence/memory-v0.6-comparison-final.json)):

- `B0-exact-v0.5.2-no-steward-recall` runs the raw receipt fixture on a separate
  fresh Store with no Steward profile binding, asserts zero Steward jobs and zero
  Records, and returns receipt-only fragments. `coffee` returns `I prefer
  coffee.` first and `tea coffee` returns the newer update first with the old
  receipt second.
- `B1-exact-v0.5.2-steward-recall` is the Steward-organized Recall on the same
  side; its fragments include the derived `Prefers coffee.` Record, which is why
  it must not be labeled `B0`. `B1-prior-steward-context` is the baseline
  recency-only `Claim` context: the eight most recently updated heads, so the
  relevant older Record is not offered.
- `C2-relevant-steward-context` offers the relevant older Record at rank 1, and
  `C2-current-steward-recall` returns the same fragments as `B1`.
  `C2-trusted-source-supplemental` offers the subject/fact-key matched Record at
  rank 1 and is `not_applicable` for the baseline.
- `L0-current-no-steward-recall` is the in-tree receipt-only control; it is not a
  baseline result.

Every Recall arm records `retrieval_only = true` and
`answers_preference = false`: retrieval is not adoption, and the false-adoption
risk (older preference fragments still ranking behind a newer update) is
reported, never resolved as an answer.

The comparison harness records the scheduler encoding per side: the exact
v0.5.2 baseline compares `time.RFC3339Nano` schedule text, which is not
fixed-width, while the final v0.6 candidate stores fixed-width UTC nanoseconds.
That is measured, not assumed: the report carries
`rfc3339nano_lexicographic` for the baseline and `fixed_width_utc_nanoseconds`
for the current tree. The harness still uses a deterministic whole-second
monotonic clock so its own comparison does not depend on either encoding.

A pre-existing v0.5.2 Steward job-scheduling defect surfaced while building this
comparison: `available_at`/`lease_expires_at` are compared as
`time.RFC3339Nano` text, which is not fixed-width. The comparison harness isolates
that baseline defect with a deterministic whole-second monotonic clock. The
final v0.6 candidate fixes the scheduler with exact fixed-width nanosecond UTC
values and explicit migration of mutable scheduler fields only; fractional
onset/expiry regressions cover `.120` → `.123` and exact lease expiry. Details of
the discovery and minimal baseline reproduction are in
[`docs/evidence/memory-v0.6-facts-structural-comparison-2026-09-15.md`](evidence/memory-v0.6-facts-structural-comparison-2026-09-15.md).

Real extraction, model and consumer-answer quality comparisons remain
GA-blocked.

## Performance harness

`internal/appliance/facts_performance_test.go` is opt-in and reports only
measured numbers:

```sh
FACTS_PERF_SIZES=1000 make facts-perf
FACTS_PERF_SIZES=1000,10000,100000 make facts-perf
```

It seeds one hot Space/LabelSet partition through the real evidence admission
path (default `space-bot-a`, one fact per receipt with distinct subjects) and
measures per-receipt seed
latency p50/p95/p99, cold first read after reopen, warm point reads, the owner
change ledger, reader/writer contention (errors counted, no sleep-based
synchronization), `RebuildFTS`, managed cleansing via `DeleteReceipt`, database
page size, FTS page bytes when `dbstat` is available, and process peak RSS plus
Go heap size. The report states `model_calls = 0` because no model is invoked.

The harness itself does not enforce a release threshold. Numbers are
machine-specific and the 100,000-receipt size is expensive; it is not run
automatically. The candidate has separately
[frozen local hardware limits](evidence/memory-v0.6-performance-limits.json)
after its baseline and before any optimization, and records the final rerun
and comparison in the [local evidence](evidence/memory-v0.6-local-candidate.md).
This is not 100k facts for one subject: reads are point queries across distinct
subjects. Cold means SQLite reopen, not cache flushing; model calls are zero.

## Reproduction

```sh
make facts-gate

FACTS_PERF_SIZES=1000 make facts-perf

go run ./scripts/facts_eval -fixtures internal/appliance/testdata/facts_eval \
  -report dist/facts-eval-report.json

go run ./scripts/facts_compare -unrelated 80 -output dist/facts-compare-report.json
```

The fixture identities, provenance, and re-hash procedure are documented in
[`internal/appliance/testdata/facts_eval/README.md`](../internal/appliance/testdata/facts_eval/README.md).
The final evidence index is
[`docs/evidence/memory-v0.6-local-candidate.md`](evidence/memory-v0.6-local-candidate.md),
with actual source-snapshot attribution, the raw facts report, corrected
structural comparison, performance and soak results. The preliminary
[facts evaluation](evidence/memory-v0.6-facts-evaluation-2026-09-15.md) and
[structural comparison note](evidence/memory-v0.6-facts-structural-comparison-2026-09-15.md)
are retained as historical evidence, not the latest candidate result.
Governance forget semantics are owned by
[`docs/memory-v0.6-governance.md`](memory-v0.6-governance.md); the pre-v0.6
corpus evaluation remains
[`docs/memory-appliance-evaluation.md`](memory-appliance-evaluation.md).
