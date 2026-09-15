# Memory v0.6.0 — candidate release notes and checklist

Status: **implementation candidate; not a published/GA release**. The user
authorized a candidate commit, push and PR submission after local acceptance.
Tagging and publication still require separate authorization.
`VERSION=0.6.0` is candidate metadata, not evidence of release.

Base: `51693ff135be8c4149c15117980290aaad6d90da` (v0.5.2), originally clean.
The initial preparation found no remote v0.6.0 tags. This repair did not refresh
remote release state; check it again before an authorized release.
Caelis was initially audited read-only at
`ef6c4697bdf5af18db141b7d4b1eff4f28c3bbc7`. A later read-only status check
found a clean worktree at `f6514aa09cd26e8853e4a6d2420125ac7cb77acd`; the
final read-only check found it clean at
`40a79a68fd85f32dafd82419289f2c8593734a01`. This Memory repair did not modify Caelis; the initial integration audit is not
a requalification of its newer revision.
The reviewed working-tree snapshot and gate evidence are recorded below.
The PR head commit identifies the submitted candidate for remote CI.

## Draft release notes

### Added

- Versioned `memory.facts.v1alpha1`: confirmed versus pending/denied adoption,
  stable subject/key attribution, explicit effective intervals and bounded exact
  conditions on existing Record/Revision state.
- `Runtime.Evidence()` trusted selected-source admission, exact source identity
  deduplication/suppression, producer/subject admission policy and atomic offline
  structured edits. Models cannot self-assert trusted source identity or adoption.
- `Runtime.Facts()` current/historical fact reads, revision audit, controlled
  bilingual aliases, deterministic bounded background, explicit budget trimming,
  time-bound refresh hints and generation/scope-bound change cursors.
- Public embedded management: receipt and Record inspection/tracing, correction,
  deletion, cleanup status, rebuild and Grant revocation without internal imports.
- Durable forgetting barriers and managed cleansing through transitive actual
  Steward read sets, historical revisions, bounded-batch outputs and exception
  links. In-flight work cannot publish after invalidation; Open resumes cleanup.
- Relevant Steward context (subject/key, FTS, recent records), exact persisted
  read-dependency validation and atomic proposals of at most eight operations.
  Generated facts stay pending; structured heads require authorized host edits.
- New `facts-gate`, candidate longitudinal fixtures, structural comparison and
  hot-partition performance harnesses, plus independent Go-module consumer smoke.

### Compatibility and migration

- Remember/Recall remain `memory.v1alpha1` evidence retrieval with read-your-writes.
  A superseded fact's original Receipt can still be an independent evidence hit;
  personalization must explicitly opt into Facts. There is no claim of recalling
  conversations a producer never submitted.
- Schema **1 → 2**, marker `memory-v0.6.0`, is an atomic forward migration from
  the v0.5.0/v0.5.2 floor and final byte-identical prerelease schema. Old receipts,
  idempotency, timestamps, provenance and authorization survive. Legacy facts
  are not promoted to confirmed; unknown onset is not filled from updated_at.
- Old binaries reject schema 2. Do not downgrade in place. Use an independently
  verified pre-upgrade backup and reconcile forget/correction state before
  authorizing a restore generation. See [migration](memory-v0.6-migration.md).
- Embedded Go package only: no sidecar, vector DB, external service, embedding
  download, Corpus/Leaf importer, Bot or Caelis Session format in the protocol.
  New management read types are embedded-only, not new standalone CLI endpoints.

## M01–M07 delivery status

| Task | Local implementation | Qualification / remaining work |
| --- | --- | --- |
| M01 | facts protocol, schema migration, fixed v0.5.2 fixture | owning migration/unknown-time tests and external module gate |
| M02 | owner facade, barrier, transitive cleansing, recovery | deterministic stale/in-flight, batch, >64-hop and restart tests; external copies remain host-owned |
| M03 | trusted sources, suppression, future policy, structured edits | source conflicts, retries, wrong subject, read-only and deny-policy tests |
| M04 | relevant context, full read set, atomic batch, pending inference | deterministic structural comparison; real-model extraction quality not qualified |
| M05 | current/history/background/cursors and fixed aliases | lifecycle, isolation, budget, history pagination and restart tests; arbitrary paraphrase quality not qualified |
| M06 | 14 blocking and 208 expanded trajectories independently reviewed under the user's AI substitution authorization; structural comparison and frozen performance limits | human-reviewed count remains 0; production model/consumer answers and natural paraphrase holdout unqualified |
| M07 | public-only consumer harness, local review repairs and release preparation | native Windows/exact-candidate remote CI, Caelis integration and separate release authorization remain |

## Review repairs and substitute adjudication

The local acceptance review found five reproducible defects, now repaired:

1. Trusted-source retry excludes a fact covered by a durable forget barrier,
   including while cleanup is pending and after recovery; unaffected facts from
   the same retained source remain available.
2. Applicable conditions and equal-specificity ambiguity are resolved before
   query matching or budget selection. A query cannot revive a suppressed
   default or choose between conflicting current facts.
3. Repeated exception corrections preserve the related base Record and retain
   finite-interval, non-overlap and allowed-transition constraints.
4. A trusted subject without an optional fact key leaves room for lexical
   context before the subject's recent-record fallback.
5. Cleared-revision diagnostics require a completed deletion barrier and cleared
   payloads; an ordinary unstructured revision's empty fact JSON is insufficient.

The [five original counterexamples](evidence/memory-v0.6-review-reproduction_test.go.txt)
failed before repair and now pass. Owning regressions additionally cover pending
cleanup, reopen, unaffected facts, ambiguity, repeated corrections and denial.
See [local evidence](evidence/memory-v0.6-local-candidate.md).

The user explicitly authorized assistant/independent-agent adjudication in place
of manual review. Two independent agents reviewed
[208 expanded candidates](evidence/memory-v0.6-agent-candidate-review.md) and
[14 authored blocking trajectories](evidence/memory-v0.6-agent-blocking-review.md),
with per-case decisions, rationales, input hashes and follow-up findings. This
substitute-review requirement is complete. **Human-reviewed cases remain 0.**
The 208 candidates are 26 semantic templates across eight subjects; they are
not 208 distinct real-user situations or a production-quality holdout.

Review also strengthened the evaluation runner: it consumes frozen expected
current values, checks absence of adoption before confirmation, checks exact
Record/Revision and complete source attribution, and tests exception denial
inside its effective interval. All 14 blocking, 208 candidate and 448 controlled
alias executions pass after these changes.

## Reproduction and evidence

Local reproduction commands (actual execution status and source attribution
are in the evidence page below):

```sh
make check
make race
make durable
make corpus-gate
make facts-gate
make facts-consumer-gate
GOWORK=off CGO_ENABLED=0 go test ./...
GOWORK=off CGO_ENABLED=0 GOTOOLCHAIN=go1.25.8 go test ./...
FACTS_PERF_SIZES=1000,10000,100000 make facts-perf
make ga-soak GA_SOAK_REPORT=/absolute/path/to/ga-soak.json
make m5-benchmark
```

- [Longitudinal evaluation procedure](memory-v0.6-evaluation.md)
- [Final local candidate evidence](evidence/memory-v0.6-local-candidate.md)
- [Raw candidate evaluation](evidence/memory-v0.6-facts-final.json) and
  [corrected structural comparison](evidence/memory-v0.6-comparison-final.json)
- [Caelis integration tasks and public consumer harness](memory-v0.6-caelis-integration.md)
- [Facts contract](memory-v0.6-facts.md), [governance](memory-v0.6-governance.md),
  [Steward](memory-v0.6-steward.md)

Final source snapshot (177 files, excluding docs and root README):
`99037fb24c0f1fc64a5380a91ddcefac6287659cd85066fe9c66dcace0f4e8b1`.
See the [source manifest](evidence/memory-v0.6-candidate-source.json) and
[whole delivery snapshot](evidence/memory-v0.6-delivery-snapshot.json).

Post-repair `make check`, race, durable, corpus, facts, independent-module
consumer, Go 1.25.8 and Go 1.26.8 / CGO=0 full tests, M5 benchmarks and
100k-receipt soak passed. Windows amd64 / CGO=0 cross-compilation passed;
native Windows execution remains unrun. The repaired 1k/10k/100k performance
confirmation run passed all **33/33 unchanged limits**. The earlier complete
run had a 1k seed-latency overrun (32/33); it and all fixed diagnostic reruns
remain recorded, and repeatability is unqualified. See the
[local evidence](evidence/memory-v0.6-local-candidate.md). Earlier reports and
source/delivery manifests are retained separately with `before-review` names.
Local execution is not exact-candidate remote CI or publication evidence.

## GA blockers and authorization checklist

- [x] Complete the local review, repair its five findings, and retain original
  failing reproductions and passing regressions.
- [x] Complete ≥200 trajectory adjudications under the user's explicit
  independent-AI substitution authorization: 208 expanded plus 14 authored
  cases. Preserve reviewer identity and keep the human-reviewed count at zero.
- [x] Obtain authorization for a candidate commit, push and PR submission.
  Use the PR head commit as the exact candidate SHA for remote checks. Local
  validation was recorded before Git delivery; its source manifest is unchanged.
- [ ] Freeze and run production-representative model/profile/output budgets on
  the reviewed holdout. Separately score extraction, retrieval, background and
  consumer answers; report errors, stale adoption, omissions and abstention.
  Candidate-tier structured adoption/alias counts do not establish ≥95%/≥90%
  real-user quality targets.
- [x] Freeze [hardware-bound performance limits](evidence/memory-v0.6-performance-limits.json)
  after the measured baseline and before optimization/tuning; preserve their
  original bytes and provenance. This is local hardware qualification.
- [x] Verify the repaired 1k/10k/100k candidate against all 33 unchanged limits.
  See the [current comparison](evidence/memory-v0.6-performance-check.json).
- [ ] Wait for exact-candidate native Linux/Darwin and **native Windows embedded
  Open** CI. Cross-compilation and Darwin success are not Windows evidence.
- [ ] Integrate the released API into Caelis through separate scoped tasks and
  qualify its history/replay/cache/context invalidation and backup reconciliation.
  Passing Memory deletion is not passing Caelis product deletion.
- [ ] Obtain separate authority to tag and publish after candidate qualification.
  Only then follow the owning [release procedure](memory-appliance-release.md).

## Material boundaries

1. Managed forgetting fences new adoption at barrier commit, then cleanses owned
   payloads and supported projections. An already-running snapshot read and a
   context already sent to a model cannot be retracted.
2. Opaque IDs, source/effect digests, effect keys, owner audit reasons, immutable
   reference skeletons and control metadata remain. Hosts must not put sensitive
   payloads into identity/audit fields. This is not forensic media erasure.
3. Pre-forget backups/exports/provider copies and Caelis histories are separate
   copies. Restoring old data requires explicit deletion reconciliation and a
   new generation; a barrier in a newer backup cannot sanitize an older image.
4. v0.5 derived context was not logged. Forgetting conservatively widens to
   legacy derived Records in the same exact partition, potentially removing
   more interpreted state than the selected Receipt alone.
5. Steward unavailable/disabled means no model-based extraction. Explicit
   confirmations and structural/lexical reads still work. Generated output is
   never automatic confirmation, deletion authority or authorization expansion.
6. Source suppression matches stable producer/event/revision/fragment identities,
   not arbitrary paraphrases, new producer coordinates, or untrusted legacy
   Remember calls with fresh effect keys. Only explicitly admitted evidence is
   covered; complete conversation recall is not promised.
