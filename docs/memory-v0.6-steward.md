# Memory v0.6 Steward: bounded context, read dependencies, and batch apply

Status: owning design note for the `internal/appliance` Steward surface and the
`api/memory/steward/v1alpha1` wire contract. It records only mechanisms this
milestone ships.

## Scope and authority

The Steward executes one durable organization Job per bound `(Space, LabelSet)`
receipt. Model output remains an untrusted proposal: Memory owns retrieval,
evidence validation, canonical state, and persistence. Nothing here lets a model
select a Space, LabelSet, Job, lease, source attribution, or fact adoption.

## Relevant bounded context (M04)

`ClaimStewardJob` builds the Worker's context in priority order, all restricted
to the Job's exact Space and LabelSet, and bounded by
`ProfileSpec.MaxContextRecords`:

1. **Structured subject/key** — when the host supplies a fact key, active heads
   matching both the trusted Source subject and that key, regardless of age.
2. **Lexical FTS** — active heads ranked by the same `bm25`/lexical analyzer used
   by Recall, using the assigned receipt text as the query. A long-ago head that
   is lexically relevant survives more recent but unrelated heads.
3. **Subject fallback** — when no fact key was supplied, recent heads for the
   trusted subject fill remaining slots only after lexical retrieval. A user's
   unrelated recent facts cannot consume the budget before a relevant old fact.
4. **Recent** — the most recently updated active partition heads fill any
   remaining slots.

The order is deterministic (deduplicated by Record identity) and never queries a
Space the Job does not belong to, so authorization precedes candidate generation.
`WorkRequest.Receipt` additionally carries the host `subject`, `fact_key`, and
faithful `sources` (producer, event_id, revision, fragment, role). These are
attribution copied from `evidence_sources`; a model never authors them.

## Persisted read set and Apply revalidation (M02)

At Claim, Memory persists exactly what the Worker was shown in
`steward_read_set(job_id, attempt, ordinal, record_id, revision, receipt_id)`:
one row per cited evidence receipt per context Record revision.

Before any canonical mutation, `ApplyStewardProposal` revalidates every row of
the claimed attempt:

- each recorded Record still exists, in the same Space and LabelSet, `active`,
  and still at the recorded `current_revision`;
- each recorded evidence receipt still exists at the same scope, is not
  corrected, and is not under a deletion barrier.

Revalidation runs for every proposal, including `IGNORE`. Any drift returns
`ErrStewardConflict`; the Worker's conflict path returns the Job to `pending`, so
the next Claim re-reads fresh context rather than reusing a stale snapshot.

When a persisted read set exists, a proposal may only cite the Job receipt or a
receipt that was actually read. A model-supplied reference that was never shown
is rejected as `ErrStewardProposalInvalid`. Leases created outside Claim (the
pre-M02/test direct path) have no read set and are validated from the Job receipt
alone.

## Bounded multi-op batch (M04)

The original one-op `Proposal` shape is unchanged. A Worker may instead send an
additive, explicitly marked batch:

```json
{"policy":"bounded_batch","ops":[{"operation":"ADD",...},{"operation":"IGNORE"}]}
```

- `Policy` must equal `bounded_batch`, single-op fields must be empty, and
  `1..MaxProposalOps (8)` ops are allowed; each Record may be targeted once.
- Ops are validated and applied in one transaction. If any op is invalid, stale,
  cross-Space, or blocked by the structured-fact guard, the whole batch rolls
  back with no partial application.
- `semantic_revisions.job_id` is no longer `UNIQUE`, so one real Job can anchor
  several Revisions without synthetic jobs. The parent-owned facts migration
  performs that rebuild; this surface only depends on the result.
- `ApplyResult.Ops` reports each applied op; single-op callers keep reading the
  top-level `Operation`/`RecordID`/`Revision` as before.

## Structured fact guard

`semantic_records.subject`/`fact_key` and `semantic_revisions.fact_json` are
host-owned structured state. A model operation whose `MERGE`/`SUPERSEDE` target
head has a nonempty `subject` or `fact_json` (any adoption, including `denied`)
is refused with `ErrStewardConflict` and no head, revision, or projection change.

A model `ADD` derived from a receipt with a trusted host Source that names a
subject is written with pending structured metadata
(`facts.Metadata{subject, key, adoption: pending, transition: establish}`) and
the head carries the host `subject`/`fact_key`. Only the host facts API may later
confirm, change, correct, or deny it. A model `ADD` without host attribution
stays `subject=''`, `fact_json=''` — unknown and unadopted, never a current fact.

## Deletion barrier integration

The governance surface owns `forgetting_barriers` and physical cleansing. This
surface consumes its helpers:

- `ClaimStewardJob` never leases a Job whose receipt has a deletion barrier; such
  rows are settled as `receipt_forgotten`.
- `ApplyStewardProposal` refuses a forgotten Job receipt with
  `ErrStewardLeaseLost`, and revalidation refuses forgotten evidence with
  `ErrStewardConflict`.
- `GetSemanticRecord` blanks derived Revision text while a deletion barrier
  exists, so a crash between barrier commit and physical cleansing cannot
  disclose forgotten content.

Governance's cleanup may delete the Job row once no surviving Revision anchors
it; a subsequent Apply for that lease then reports `ErrStewardLeaseLost`.

## Schema ownership

`factsMigrationSQL` (parent) adds `semantic_records.subject`/`fact_key`,
`evidence_sources`, `memory_changes`, and rebuilds `semantic_revisions`
(dropping `UNIQUE(job_id)`, adding `fact_json`). `governanceMigrationSQL`
(governance) adds `forgetting_barriers` and the content-cleansing trigger. This
surface contributes only `stewardMigrationSQL`, the `steward_read_set` table,
applied inside the same migration transaction.

## Verification

Focused tests cover: deterministic leased work -> forget -> apply with no sleeps;
a long-ago relevant fact surviving noise; bounded-batch atomicity (a stale second
op leaves no partial write); Apply revalidation including `IGNORE` and fresh
context on re-claim; host-sourced `ADD` pending metadata and the structured-head
guard; rejection of unread evidence; and the deterministic zero-model read-set
path. API and SDK tests cover batch shape/schema/parsing.
