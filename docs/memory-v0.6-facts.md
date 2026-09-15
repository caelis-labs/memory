# v0.6 current facts and trusted evidence

Status: implemented candidate contract, **not GA qualification**. Public wire
owner: `api/memory/facts/v1alpha1` (`memory.facts.v1alpha1`). The existing
`memory.v1alpha1` Remember/Recall contract remains evidence retrieval. In
particular, a superseded Record's original Receipt may still be recalled as
raw evidence; it is not a current user preference. Current personalization
must use `Runtime.Facts().ReadFacts` instead.

## Authority and composition

- `Runtime.Facts()` exposes `ReadFacts`, `FactHistory`, and `Changes`. All require
  a live Recall capability, including exact Space/View, actor, audience and
  LabelSet authorization **before** enumerating candidates.
- `Runtime.Evidence()` is a **trusted host-only** interface. `SubmitEvidence`
  additionally requires Remember authority. Do not project this interface or its
  source/subject/mutation arguments into model tools. Possession of its policy
  setter is owner authority, not read-only worker authority.
- `Runtime.Management()` is the owner governance interface. The host must
  authorize user management interactions before invoking it. It is not a
  per-user bearer transport and must never be given to delegated model work.
- A host selects a stable opaque subject and exact memory partition. A subject
  attributes information **inside** a partition; it never grants access. A
  private/shared combined View can read both authorized Spaces; facts from
  distinct Spaces remain distinct and carry Space IDs. No cross-Space truth
  merge or raw BM25 probability comparison is performed.
- Host source adaptation, user confirmation UI, model/credential/budget choice
  and context assembly stay outside Memory. Memory does not import transcripts,
  reconstruct missing conversations, or modify producer histories.

## Admission and source identity

`SubmitEvidence` atomically admits an immutable Receipt and its provenance,
indexes it, and either enqueues optional Steward work or applies at most eight
explicit structured edits. It never calls a model. Source identity is:

```
(Space, exact LabelSet, producer, event_id, revision, fragment)
```

The source identity is stored as a digest alongside the Receipt ID. Source
subject, role, and optional fact-key hint are trusted host assertions, not text
classification. Same identity + different text/source/assertions is `conflict`;
same identity + identical payload deduplicates even with a different call key.
A suppressed identity returns `accepted=false, rejection_reason=source_suppressed`.
Deduplicated responses also apply Record forgetting barriers: a retained
confirmation source cannot return a forgotten derived fact while cleanup is
pending. Independent unaffected facts from that same source remain available.
An exact producer/subject ingestion deny policy rejects before creating any
Receipt, source row, index entry or Steward job. Responses explain that refusal.
A deny policy affects **future admission**, not existing facts or old Remember.

Roles are `user_quote`, `structured_confirmation`, `observation`, and
`inference`. Explicit host-supplied structured confirmations and verifiable
observations can establish adoptable facts; raw quotes and inferences are
pending. **All model-generated facts are pending regardless of source role or
confidence.** A host must not relabel generated extraction as a structured
confirmation without actual user/host verification. "Choose a vegetarian
restaurant for a friend" is not verification of the user's diet.

Suppression is identity-based, not universal semantic duplicate detection.
Changing an event revision/fragment/producer creates a different source identity;
a producer must retain stable source coordinates and propagate upstream
redactions. Arbitrarily paraphrased text, legacy Remember with a fresh effect
key, external exports and another host's copies are not deduplicated by meaning.

`SubmitEvidenceResponse.Organization` reports `disabled`, `pending`,
`processing`, `applied`, or `failed` based on stored work. `applied` means an
organization effect exists, **not that its facts are confirmed**. Each fact's
`metadata.adoption` is separate. Existing `GetReceiptStatus` remains available
for later processing status. No Steward binding means zero model calls;
structured edits and deterministic reads still work.

## One Record/Revision lifecycle

No separate profile truth store exists. Record heads add subject/key indexes;
immutable Revisions add `fact_json`. Receipts remain the evidence. A textual
event may omit its fact key and stay a standalone event. Unknown legacy subjects,
onsets and adoption remain unknown after [migration](memory-v0.6-migration.md).

| Transition | Meaning | Validation and effect |
| --- | --- | --- |
| establish | initial explicit assertion | no target; same subject/key/conditions cannot silently fork an existing fact |
| change | genuine long-term change | confirmed target + expected revision + explicit valid_from; prior revision ends at that instant for historical adoption |
| exception | finite temporary override | confirmed same-key target + expected revision + valid_from/valid_until; separate linked Record; base resumes after expiry |
| correct | previous interpretation was wrong | exact target revision; prior erroneous revision is marked corrected in historical audit, not portrayed as a former user habit |
| confirm | verification of a pending assertion | exact pending text and target revision; retains original evidence and adds confirmation Receipt |
| deny | rejection, not a preference change | exact target revision; invalidates current head and historical adoption while retaining explicitly marked denial audit |
| forget | remove managed content | owner `DeleteReceipt` over supporting evidence; durable barrier and transitive cleanup, not a model proposal |

Batches validate and apply in one SQLite write transaction. Stale, cross-Space,
cross-LabelSet or wrong-subject targets abort **all** receipts and edits. Lifecycle
updates require structured confirmation. Model proposals cannot overwrite
structured heads. [Steward](memory-v0.6-steward.md) owns bounded relevant context,
parsing/policy and actual-read dependency validation.

Time intervals are `[valid_from, valid_until)`. Neither `updated_at` nor receipt
arrival is automatically used as fact onset. Nil onset means a current assertion
with unknown historical onset: usable now, not proof at an arbitrary past time.
`change` requires explicit onset; `exception` requires both endpoints. Conditions
are at most eight exact `{key,value}` host-context equalities. Missing context
never matches. No natural-language condition or arbitrary expression is
interpreted. More-specific matching conditions refine unconditional facts;
equally-specific ambiguous same-partition/key matches abstain.
Condition precedence and ambiguity are resolved before query matching and
budget trimming. A query cannot reinstate an overridden default or pick one
side of an unresolved conflict. Exception links survive repeated corrections;
every correction retains a finite, non-overlapping interval, and an exception
cannot be converted to a permanent change by first correcting it.

## Read purposes, context and invalidation

- `ReadFacts` with no `AsOf` selects confirmed, applicable current facts only.
  It never mixes raw Receipt hits into personalization.
- `ReadFacts.AsOf` selects historical adoption using explicit valid intervals.
  Incorrect/denied/forgotten assertions are not adopted for the past.
- `FactHistory` gives bounded revision audit, including explicit `changed`,
  `corrected`, and `denied` labels. Continue with `AfterRevision=NextRevision`
  when truncated; if nothing fits, raise the byte budget rather than advancing.
  It never returns a personalization background.
- `DataPlane.Recall` remains legacy lexical evidence/semantic retrieval, including
  read-your-writes. Use it when raw submitted evidence is the intended purpose.

Structured key selection precedes the controlled alias/lexical path. The fixed
alias keys are `preference.drink`, `diet`, `language`, and `timezone`. There is no
query rewrite model, embedding model, graph store or external index. These are
bounded vocabulary matches, not a promise of arbitrary natural paraphrase recall.

Background is a deterministic formatting of the returned facts, not another
editable profile. Each fact includes Record ID/revision, metadata, Space and
Receipt/source references. Fact text is JSON escaped in background lines; it
remains **data, never instruction authority**. Budget limits are 1–64 facts and
512–1,048,576 bytes. `BytesUsed` charges the facts JSON array plus background UTF-8
bytes (not the response envelope/cursor); `Truncated` marks omitted facts. The
host must additionally enforce its complete model-context/token budget.

Responses carry a generation/scope-bound cursor and the next effective time
boundary (`RefreshAt`). Poll `Changes` under the same capability/scope to learn
receipt/fact/governance invalidations. The mutation transaction commit is the
linearization point: subsequent reads may not adopt invalidated data. A read
already in progress can have an earlier snapshot; a context already sent to a
model cannot be recalled. Hosts must invalidate cached/background/session copies
before the next model call and refresh on `RefreshAt` even if no write occurred.

Change events are retained in this version. Paginate until `HasMore=false` and
advance only to the returned cursor. `ResetRequired` means generation/scope
changed, cursor is ahead, or initial synchronization is needed: discard cached
facts and read a fresh snapshot. Cursors are not authentication tokens. The host
must not persist context indefinitely without polling or expiry checks.

## Forgetting and restore boundaries

See [governance](memory-v0.6-governance.md) for barrier/cleanup completion and
restart recovery. To forget a fact, inspect its Receipt evidence through owner
management and delete the selected evidence. Receipt-level deletion can
conservatively remove multiple dependent facts; the host should explain that
scope before accepting a destructive request.

Cleanup is logical deletion of managed payloads and supported projections, not
forensic disk erasure. Source suppression digests, content-free revision and
receipt IDs, governance/lease audit metadata remain. A backup/export made before
forgetting still contains old data. Do not restore it and claim forgetting held:
restore is a separate owner-authorized generation transition, fenced pending
cross-component verification. Reconcile the host's deletion ledger/upstream
sources before `CommitRestore`, or discard that backup. Opaque IDs, effect keys and
owner-supplied audit reasons are retained; hosts must not copy sensitive fact
text into those metadata fields. Memory's barrier does
not clean Caelis replay/context copies or external model-provider retention.
