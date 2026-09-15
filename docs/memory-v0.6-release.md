# Memory v0.6.0 — release notes

## Release scope

This release delivers the independently consumable embedded Go package
`github.com/caelis-labs/memory` at `v0.6.0`, with versioned
`memory.facts.v1alpha1` APIs. The publication record is the
[GitHub Release](https://github.com/caelis-labs/memory/releases/tag/v0.6.0);
release-please publishes the reviewed release PR after protected-PR checks.
See the [release procedure](memory-appliance-release.md#automated-source-releases).

The package release qualifies deterministic admission, fact lifecycle,
authorization, governance, migration, durability and public API consumption.
Production model extraction, arbitrary paraphrase retrieval, final Bot answer
quality and Caelis's new Facts product integration remain **unqualified**.
Those earlier end-to-end GA goals are tracked separately below; publishing this
package does not assert their completion. No standalone binaries are published.

Base: `51693ff135be8c4149c15117980290aaad6d90da` (v0.5.2).
[PR #2](https://github.com/caelis-labs/memory/pull/2) contains the implementation
and review repairs. Earlier candidate results are retained in the
[historical local evidence](evidence/memory-v0.6-local-candidate.md).

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

## External review fixes

The [latest external review](evidence/memory-v0.6-pr2-external-review.txt)
identified three reproducible problems at `c5652ec`:

1. Correcting a change's onset left its predecessor's end at the erroneous date.
   Timeline reads now resolve correction chains before deriving adjacent ends.
   Corrected changes retain explicit onsets and cannot cross the preceding
   interval's start; finite changes cannot make an old preference reappear.
2. Correcting a pending Steward fact could bypass confirmation's same-key guard.
   Every edit producing a confirmed base head now enforces the same uniqueness
   constraint. Equal and different conflicting values both reject atomically.
3. Owner Record listing/tracing held a result set while requesting another pool
   connection. Both operations now use one read-only transaction for the head,
   governance fence, revisions and evidence. Their snapshot is consistent and
   they complete with only one of the production pool's eight connections free.

Six executable regression tests cover these defects, onset corrections in both
directions, repeated corrections, restart, exact boundaries, confirmation
chains, rollback and deterministic pool exhaustion. Independent follow-up review
identified two additional onset cases, now fixed and covered. See the
[release verification record](evidence/memory-v0.6-release-verification.md).

The earlier five acceptance findings and their original red/green evidence
remain in the historical candidate record. Independent agents also adjudicated
208 candidate trajectories and 14 authored blocking trajectories under the
user's explicit substitution authorization. **Human-reviewed count is 0.**
The 208 candidates contain 26 templates over eight subjects; they are not a
production holdout. Their 448 controlled alias checks measure fixed vocabulary.

## Verification and source identity

The repaired implementation checkpoint is frozen by the
[178-file release source manifest](evidence/memory-v0.6-release-source.json):
`a8001fb40d556b9608b1d028ef3bb6a78e9b9c02beb49a5160920b8b168e6463`.
This manifest predates the release-please, CI and security-policy additions;
those changes leave its Go runtime, tests, fixtures and module dependencies
unchanged. The earlier 177-file candidate manifest also remains historical.
Commands, exact input attribution, results and remaining
qualification limits are recorded in the
[release verification record](evidence/memory-v0.6-release-verification.md).

The candidate gate is `make release-candidate`, minimum Go 1.25.8 / CGO=0 full
tests, the fixed 100-Space / 100,000-Receipt / 10,000-Record soak, frozen
1k/10k/100k performance harness and a temporary exact Caelis consumer snapshot.
The user explicitly stopped the final full performance rerun after engineering
feasibility acceptance. It produced no aggregate report; the final source does
not claim a new 33/33 performance pass. Prior-source results remain historical.
Remote `quality` includes native Linux, Darwin arm64 and Windows amd64 embedded
Open on implementation PRs. Release metadata PRs validate version consistency
and retain the tested implementation tree. Merging an approved release PR
triggers publication without repeating the full suite on `main`.

## Separate product qualification

- Production model/profile/output budgets, reviewed natural-language holdout,
  extraction/omission/stale-adoption scoring, arbitrary paraphrase Recall@8,
  background usefulness and final answers have not been measured. The proposed
  95% preference / 90% paraphrase targets are not measured release results.
- Caelis's existing embedded consumer can be tested in an isolated source
  snapshot; its shared checkout and dependency pin are not changed by Memory's
  release. New Facts admission, context/cache invalidation, Session history,
  replay, backup and external-copy reconciliation require the separate
  [Caelis integration work](memory-v0.6-caelis-integration.md).
- Frozen performance limits are local Apple M4 observations for distinct-subject
  reads within one Space/LabelSet. They do not qualify 100k facts for one subject,
  production models or portable latency promises. Earlier recorded overruns
  remain visible; no passing rerun establishes statistical repeatability.

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
