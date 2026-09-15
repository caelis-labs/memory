# Memory v0.6 Governance: Logical Forgetting Barrier and Managed History Cleansing

Status: normative for the M02 governance surface. It describes the appliance
behavior implemented in `internal/appliance/governance.go`,
`internal/appliance/governance_cleanup.go`, and
`api/memory/management/v1alpha1`.

## Problem

Before v0.6, `DeleteReceipt` removed appliance-owned receipt content, the
receipt projection row, and lexicon evidence, and it invalidated semantic
Records that cited the receipt in their current Revision. Derived payload
survived that path:

* a Record that cited the receipt in an older Revision and was then SUPERSEDEd
  stayed active, so its derived text stayed readable and indexed;
* `semantic_revisions.text` kept a model paraphrase of deleted receipt content;
* a Record whose text was derived while reading another Record was never
  connected to the forgotten receipt at all.

A read of `GetSemanticRecord` after a deletion therefore still returned derived
content attributable to forgotten evidence. v0.6 closes that path.

## Two barriers

Forgetting intent is recorded as a durable barrier row in
`forgetting_barriers(barrier_sequence, space_id, label_set_digest, kind,
receipt_id, status, created_at, cleaned_at)`.

| kind | created by | derived content | history |
| --- | --- | --- | --- |
| `receipt_deleted` | `DeleteReceipt` | cleared (text and fact metadata) | content-free skeleton kept |
| `receipt_corrected` | `CorrectReceipt` | preserved | preserved, use invalidated |

A deletion barrier is the only thing that authorizes content cleansing. A
correction never clears history: it invalidates prior derived use, suppresses
the corrected source, and keeps the wrong-source audit intact.

`barrier_sequence` is the monotonic invalidation version. It is returned by
`DeleteReceipt` as `invalidation_version` and reported by `TraceRecord` and
`CleanupStatus`.

## Two phases

`DeleteReceipt` and `CorrectReceipt` commit in two transactions:

1. **Barrier transaction.** Insert the barrier with `status = 'pending'`,
   suppress the producer source, invalidate every transitively affected Record,
   append the owner change rows, write the tombstone (deletion only), remove the
   receipt payload and its projection and lexicon evidence, and store the
   management effect. The logical forgetting barrier is therefore committed
   before any managed history cleansing, and it becomes visible atomically with
   the receipt removal.
2. **Cleansing transaction.** Clear `semantic_revisions.text`,
   `semantic_revisions.fact_json`, and `semantic_revisions.kind`, clear
   `semantic_records.kind`, `semantic_records.subject`, and
   `semantic_records.fact_key`, remove the projected rows, settle and clean up
   Steward jobs, and mark the barrier `cleaned`.

A crash after phase 1 leaves a `pending` barrier. Derived content is already
denied by the read fences, and `RecoverGovernanceCleanup` finishes the physical
obligation on the next `Open` and after an embedded `CommitRestore`. Recovery is
idempotent.

## Read fences

While a barrier exists, every read of derived state refuses the content:

* `GetSemanticRecord` blanks `Revision.Text`, `Revision.Kind`, and the head
  `Record.Kind`;
* `TraceRecord` returns a content-free Revision skeleton (blank text and kind)
  plus Evidence receipt identities;
* `ListRecords` reports the Record state `forgetting` (barrier committed,
  cleansing pending) or `forgotten` (cleansing complete) with a blank head
  kind;
* the data plane never sees the Record at all: invalidation removes the semantic
  projection row and the Record stops being an active head, and `RebuildFTS`
  only rebuilds `status = 'active'` Records.

`Kind` is cleared because it is free model-supplied text, bounded but not
interpreted, and can therefore carry forgotten content. The head `subject` and
`fact_key` are cleared by managed cleansing and need no read fence: the
management record views never select them, and both the Steward context builder
and the facts reader require `status = 'active'`, which a forgotten Record never
is.

Attribution is preserved on purpose. `semantic_evidence` receipt identities,
Revision identity, kind, operation, job linkage, and timestamps all survive, so
a forgotten Record stays owner-auditable without disclosing derived payload.

## Attribution

The closure is persisted in
`forgetting_barrier_records(barrier_sequence, record_id, space_id)`. It is the
single authority for every derived-state decision:

* the read fences consult it, so a Record reached only through the read set is
  denied just like a Record that cites the receipt directly;
* the governed immutability trigger consults it, so a later cleansing phase can
  never blank a Revision the barrier did not attribute;
* cleanup reads it instead of recomputing attribution, so the cleansing target
  is exactly what the barrier committed.

`semantic_evidence` is never rewritten. No receipt attribution is invented or
removed, and Evidence receipt identities stay immutable.

## Transitive closure

The affected set is computed by `forgettingClosure`:

1. seed with Records whose `semantic_evidence` cites the receipt;
2. add every Job whose own receipt is the forgotten receipt, and every Job whose
   persisted Steward read set (`steward_read_set`) references the forgotten
   receipt or an already attributed Record;
3. add **every** Record produced by those Jobs (`semantic_revisions.job_id`). A
   bounded-batch Job produces several Records, so all of its Revisions are
   walked, not only the first;
4. add every Record that declares a structured fact relation to an attributed
   Record (`semantic_revisions.fact_json` `related_record_id`). That relation is
   a declared derivation edge with no read set, and it is partition-local: only
   Records in the same Space and LabelSet as the forgotten receipt are added;
5. repeat until a full pass adds nothing new.

The loop is a finite worklist, not a fixed number of rounds: each pass strictly
adds at least one Record or Job drawn from existing rows, so it terminates on its
own and never returns a partial closure. There is no iteration cap, because a
capped loop would silently leave forgotten derived content readable; a cancelled
context aborts and fails the whole mutation transaction instead.

Because the read set records the exact Record revision and evidence receipt a
Worker was shown, a Record whose text was derived while reading another Record
is included even when its own Evidence never cited the forgotten receipt. The
chain composes: if Job B read Record A and Job C read Record B, forgetting the
receipt behind A clears A, B, and C although only A cites it.

### Legacy data

Revisions that already existed when the governance migration ran have no read
set, so their derivation can never be reconstructed. They are recorded once in
`governance_legacy_revisions`. Once anything at all is attributed, the closure
is conservatively widened to **every** pre-migration Record in the same Space and
LabelSet partition. A pre-migration Worker could have read partition Records
without leaving any reference behind, so no legacy Record can be ruled out; the
whole legacy partition is the honest safe superset. v0.6 Records are still
reached only through the closure above, so a fully attributed Record in the same
partition is never swept in by widening alone.

## Source suppression

`evidence_sources` rows for the receipt are set to `suppressed = 1` on both
deletion and correction, which stops the producer source from ever being
re-ingested or reprocessed into new derived state. The opaque hashed
`source_key` is always retained.

* A deletion also clears `source_json`, because the receipt payload itself is
  removed.
* A correction keeps `source_json` as owner-visible audit and leaves the
  original Receipt immutable evidence.

A suppressed source is never offered to a model: the Steward context builder
skips it, so it cannot reappear as model-visible EvidenceRefs.

## Owner change ledger

`memory_changes` records one receipt-level row plus one row per affected Record:

| kind | meaning |
| --- | --- |
| `receipt_deleted` | the receipt was forgotten |
| `receipt_corrected` | the receipt was shadowed by a correction |
| `record_forgotten` | the Record was invalidated by a deletion barrier |
| `record_invalidated` | the Record was invalidated by a correction barrier |

## Immutability

The v0.5.0 `semantic_revisions_immutable_update` trigger rejected every update.
The governance migration replaces it with a trigger that still rejects every
update except the exact cleansing update authorized by a committed deletion
barrier: only `text`, `fact_json`, and `kind` may become empty, and identity,
Space, operation, job linkage, and timestamps must be unchanged. The DELETE
triggers on `semantic_revisions` and `semantic_evidence` remain unconditional,
because this design never deletes those rows.

## Management surface

`appliance.Management` exposes `SearchReceipts`, `TraceReceipt`,
`CorrectReceipt`, `DeleteReceipt` (the public forget verb), `CleanupStatus`,
`ListRecords`, `TraceRecord`, `RebuildFTS`, and `RevokeGrant`.

`ListRecords`, `TraceRecord`, and `CleanupStatus` are embedded-only in-process
types. They are versioned wire structs usable directly through the embedded
`appliance.Management` facade; no local transport route, SDK client method, or
standalone `memoryctl` subcommand exposes them, and none is implied.

* `DeleteReceiptResponse` adds `invalidation_version` and `cleanup`.
* `CleanupStatus` polls managed cleansing for one receipt, including after an
  unreported deletion outcome.
* `ListRecords` pages Record heads within one exact Space with a stable cursor
  (the last Record identity of the previous page).
* `Inspect` reports `governance.barriers`, `governance.pending_cleanups`,
  `governance.completed_cleanups`, `governance.last_invalidation_version`, and
  `governance.cleared_revisions` without exposing receipt or Record identity.
  Cleared revisions require a completed deletion barrier and blank managed
  payload fields. Empty legacy fact metadata alone is not evidence of cleanup.

## Remaining limitations

* Cleansing clears `text`, `fact_json`, and `kind` on every attributed
  Revision, and `kind`, `subject`, and `fact_key` on the Record head. Derived
  payload that a host may later add in another column is not part of this
  migration and must be cleared by whoever owns that column.
* Job cleanup is bounded by the surviving Revision skeleton: a Job that still
  anchors a Revision must stay for attribution (its `semantic_revisions.job_id`
  foreign key), while a Job with no surviving Revision is removed. Pending and
  leased Jobs that belong to the forgotten receipt or that read an attributed
  Record are settled.
* Widening for legacy data is partition-wide within Space and LabelSet: once
  anything is attributed, every pre-M02 Record in that partition is included.
  It can clear derived Records that provably never read the forgotten receipt,
  because a pre-migration read set does not exist to prove otherwise.
* `semantic_records.invalidated_reason`, Record and Revision identity, Space,
  LabelSet, status, operation, job linkage, timestamps, and Evidence receipt
  identities all survive, so a forgotten Record stays attributable without
  disclosing derived payload. Facts-API lifecycle behavior is owned by the facts
  surface, which reads only `status = 'active'` Records.
* `ListRecords`, `TraceRecord`, and `CleanupStatus` are embedded-only: they are
  reachable in-process through `appliance.Management` and have no local
  transport route, SDK client method, or `memoryctl` subcommand. This milestone
  does not add any of those, and the absence is intentional rather than a
  pending wiring step.
* `CreateBackupSnapshot` copies the physical database. A pending barrier is
  therefore present in a backup image taken between the two phases; the read
  fences and `Open` recovery guarantee it is completed after restore.
