# Memory Appliance Internal Reference Design

Status: non-normative design direction. This document cannot expand the public
contract or block Core Profile delivery.

## Baseline engine

The durable source of truth is an immutable receipt payload. Mutable processing
status is separate:

```text
ReceiptPayload (immutable)
  receipt ID, Space ID, canonical LabelSet, text, normalized source context
  occurred time, received time, idempotency key, request digest, commit sequence

ReceiptProcessing (mutable or append-only events)
  state, attempts, last attempt, terminal error, semantic generation
```

The minimum retrieval projection lexically indexes every receipt for its whole
lifetime. It is partitioned first by authorized Space and filtered by the exact
capability-bound LabelSet in the candidate query. It is disposable and
rebuildable from receipts.

## Flat semantic organization

The first derived structure is intentionally flat:

```text
Record
  current interpreted content, status, Space, and LabelSet
Revision
  immutable version of one Record
Evidence
  same-Space and same-LabelSet links to receipts or authorized human corrections
```

`ADD` promotes receipt evidence into a new Record. `MERGE` and `SUPERSEDE`
refine an existing Record by appending a Revision and moving its active head;
they do not build a tree. The downstream host decides whether and when to invoke
the Worker. Memory owns the fixed proposal vocabulary, validation, persistence,
and exact partition rule. Record taxonomy, parent/child edges, relation graphs,
and depth-based pruning are not frozen by v1alpha1 and cannot enter the default
path without measured benefit over this flat control.

Recall may merge receipt and record candidates, but it deduplicates presentation
without erasing receipt evidence. If the semantic store, model, or jobs are
unavailable, receipt-only Recall remains correct.

The implemented merge enters each semantic FTS table only after its Space is in
the authorized View, then filters the same canonical LabelSet before returning
candidates. It joins active heads to their exact current Revision and
revalidates that every Evidence receipt still exists, is uncorrected, and is in
the same Space and LabelSet. Semantic faults discard only the affected derived
stream and mark the response degraded. Normalized equal text is folded only
across a semantic candidate and overlapping receipt Evidence, or between
semantic candidates; equal independent receipts remain separate. Evidence and
Record references are unioned and sorted for deterministic provenance.

## Planned generic layered tree

The post-v0.5 tree is a separate source-neutral data model. It does not turn the
existing flat semantic Records into tree nodes:

```text
downstream-owned source
  -> downstream-projected public Leaf request
  -> immutable partition-bound Leaf revision
  -> ordered fan-out-16 child groups
  -> parent revisions containing bounded summaries and child indexes
  -> explicit bounded Query to cited leaf Items
```

The producer owns the source format, parsing, admission, sanitization,
checkpointing, migration, versioning, and source deletion. Memory receives only
opaque SourceRef, SourceVersion, and ProjectionRef values plus ordered bounded
Items. It does not define or inspect those opaque values. A Caelis producer may
map one concrete Session JSONL projection to one leaf, but the public protocol
contains no Session or JSONL concept.

`memory.tree.v1alpha1` owns Tree, Leaf, Item, NodeRef, Revision, state, digest,
budget, and provenance types plus `CommitLeaf`, `RetractLeaf`, `GetNode`,
explicit `RequestRefinement`, and, after its evaluation gate, `Query`.
`memory.tree.worker.v1alpha1` owns the provider-neutral `ClaimBuild`,
`ApplyProposal`, and `FailBuild` contract.
Management creation, inspection, rebuild, and export use a distinct
owner-authorized surface. Tree creation fixes one exact Space/LabelSet
partition and topology/index policy. Issued capabilities grant distinct commit,
retract, exact-read, and query operations.

The base tables retain immutable leaf revisions and ordered Items. A canonical
request digest, idempotency key, expected head, and Memory-assigned stable slot
govern each commit. Parent topology is deterministic and partitioned by exact
`(SpaceID, LabelSetDigest)`. An immutable parent revision names an ordered exact
set of child revisions and its digest. Its summary and term-to-child index are
derived data with child provenance, not evidence authority. A summarizer may
propose bounded text and additional terms only after Memory has fixed the child
set; it cannot select parents, edges, roots, authority, or lifecycle.
An explicit refinement job may replace only the bounded summary blocks and
cited expansion terms for the same child-set digest under a new summary-policy
reference. It appends a revision and cannot rewrite topology or deterministic
index entries.

A producer revision or retraction immediately invalidates the affected ancestor
heads and removes them from retrieval. The dirty path is rebuilt bottom-up.
Parent heads, summaries, indexes, and roots are disposable and reconstructible
from committed leaf and child revisions without reopening an external source.
Accepted summaries may be reused only for the exact child-set and policy digest;
otherwise the parent remains degraded until new Worker output arrives.

Retraction keeps immutable leaf content for owner-authorized audit. Deletion is
a separate management operation: it replaces the leaf with a content-free
tombstone, removes Item text, and purges every summary/index payload with
transitive provenance to that leaf before any tree query resumes. IDs, digests,
and idempotency evidence may remain, but deleted content may not.

Tree retrieval authorizes before reading any tree index and independently
bounds depth, visited nodes, candidate children, returned Items, and bytes. It
is explicit, has no automatic task-briefing side effect, and applies no recency,
decay, active-window, or universal relevance policy. It is a separately
versioned public protocol, never a hidden extension to Remember/Recall or the
semantic Steward contract.

## Steward execution

A logical Steward is a versioned prompt-policy profile plus durable jobs claimed
through the Worker interface. It is not a permanent conversation, one process
per identity, or a model-provider stack inside Memory.

The first proposal vocabulary is intentionally small:

```text
ADD
MERGE
SUPERSEDE
IGNORE
```

The model proposes; deterministic application code validates same-Space and
same-LabelSet evidence, operation bounds, base revision, size limits, and
visibility before committing. LabelSet stays outside the model-facing Work
request and is inherited from the durable Job. A model cannot edit receipt
payloads, change ACL/View/Grant state, select labels, publish private data,
tombstone, or hard-delete.

The current semantic schema stores immutable `(Record, Revision)` rows and
ordered Evidence identifiers separately from mutable Record heads. Evidence
intentionally is not delete-cascaded from receipt storage: an approved receipt
deletion first invalidates every active head that cites it and removes its
semantic FTS entry, while the historical Revision retains only the now-
tombstoned receipt identifier for audit. Per-Space semantic FTS tables follow
the same authorization-before-candidate boundary as receipt FTS.

Job application retains the digest of the opaque lease after completion so an
unknown response outcome can replay only to the same worker authority. The
transaction verifies the proposal digest, same-Space evidence, mandatory job
receipt, active Record head, and optimistic Revision; then it appends Revision
and Evidence, updates the projection, completes the Job, and marks receipt
processing organized together. Changed retries and stale Revisions mutate
nothing.

Profile policy is durable and versioned. Provider transport, model selection,
credentials, billing, and provider-specific limits belong exclusively to the
downstream Worker host. Remember and correction read the current Space binding
and append a deterministic Job in the same transaction as the new receipt. A
later binding change cannot rewrite that Job snapshot. Disabling a Space removes
its binding and moves pending or leased Jobs plus receipt processing to a stable
failed state; existing Records and baseline receipt indexes are untouched.

An authorized Worker client atomically claims the oldest available Job,
changes its receipt status to processing, and receives an opaque lease token
beside a bounded model-facing request. Only a lease digest is durable. An expired
lease returns to pending and any prior Worker loses application authority. The
request is assembled from the receipt and active same-Space heads, then oldest
context heads are removed until the exact JSON fits the profile budget. Job,
Space, lease, access policy, SourceContext, model/provider configuration, and all
bearers never enter that JSON.

The Go Worker SDK accepts a `ModelGenerator` callback supplied by the downstream
host. Memory renders the complete prompt and operation shapes, bounds the input,
and parses one strict proposal; native provider JSON Schema is only an optional
optimization. The SDK contains callback panics and classifies only stable
non-sensitive failure codes before calling claim/apply/fail routes. The Memory
core owns lease expiry, retry ceilings, exponential delay, proposal validation,
and atomic canonical application; it has no outbound provider adapter. Starting
without a Worker leaves accepted receipts on the baseline path.

The older `Generator(WorkRequest) -> Proposal` callback remains only as a
pre-GA source-compatibility bridge for the currently published Caelis import.
New consumers use `ModelGenerator(GenerationRequest) -> GenerationResponse`.
After Caelis pins the next Memory prerelease and switches to the new callback,
the direct-proposal bridge is removed before GA.

Memory has no released database compatibility floor yet. A fresh database is
created from one current development baseline; it contains no provider or model
columns because those concepts belong to the downstream callback. A database
created by an older unreleased schema is rejected with an explicit instruction
to remove the development data directory and rebuild it. Forward migrations
begin only after a published release establishes a real compatibility promise.

Later operations such as RELATE, PROMOTE, DEMOTE, ARCHIVE, and model-enhanced
Recall require independent evidence and acceptance cases.

## Orthogonal dimensions

Access class, semantic kind, lifecycle status, retrieval temperature, and
provenance are separate dimensions. Hot, warm, and cold are projections or
policies, not three authoritative databases. Physical cold-tier migration is a
capacity optimization that must preserve the same logical contract.

Temporal interpretation, conflict resolution, shadowing, supersession, vector
search, relation graphs, and automatic retention are future extensions. Private
evidence may influence a private result but cannot mutate or generate shared
canonical state.

The adaptive per-Space lexicon is an internal experiment, not a Core mechanism.
A normal embedded Open supplies no lexicon policy, so schema initialization, Remember,
Recall, correction, deletion, inspection, and Steward work neither learn nor
consume adaptive terms. Focused tests and the offline corpus evaluator may
explicitly enable it to preserve reproducible A/B evidence. Persisted
experimental rows are inert while the experiment is disabled.

## Local durable Core

M1 uses one SQLite database at the current schema baseline for many Spaces, WAL, `synchronous=FULL`,
foreign keys, transactional receipts and idempotency state, and an advisory
single-process owner lock. Receipt payload updates and deletes are rejected by
database triggers. Mutable processing state is stored separately.

Each Space owns an independent FTS5 virtual table. Its table name is derived
from the SHA-256 digest of the Space ID rather than interpolating an external
identifier. Remember writes the receipt and that one Space projection in the
same transaction. Recall authorizes the View first and then enters only each
readable Space's table; shared-only Recall therefore never queries a private
index. All FTS tables are disposable and rebuild solely from receipts.

Runtime capabilities and issuer credentials are random opaque values whose
digests are stored in SQLite. The raw management credential is generated once
in an owner-only local file. Management authorization, issuer authorization,
and Runtime capabilities remain separate.

The primary integration is direct Go interface dispatch through the public
`appliance` facade. The retained standalone adapter uses HTTP over an owner-only
Unix Domain Socket on Darwin/Linux and an owner-restricted named pipe on
Windows. Transport, health, readiness, and process credentials belong only to
that optional adapter; they are not states or dependencies of the embedded
runtime. Both paths must pass the same semantic and durable conformance.

M3 governance keeps corrections and deletion separate from receipt mutation.
A correction adds a normal immutable receipt plus a durable same-Space relation;
candidate lookup filters a corrected original after its Space is authorized.
A deletion transaction writes a content-free tombstone before the guarded
receipt delete trigger permits physical removal. Tombstones retain the original
Space-scoped idempotency identity and request digest so a delayed Runtime retry
cannot resurrect erased content. Management mutation results have a separate
idempotency ledger and never reuse Runtime capabilities.

Online backup uses SQLite `VACUUM INTO` to create a consistent short-lived
owner-only snapshot, verifies it, streams it over the local management Socket,
and removes the plaintext temporary file. `memoryctl` applies a chunked AES-GCM
container so large backups do not require a plaintext durable intermediate.

Offline restore decrypts directly into an owner-only staged database while
holding the appliance owner lock. It verifies and migrates the stage, rotates
generation, creates a consistent rollback snapshot of the current database,
and atomically renames the stage over `memory.db`. A durable
`restore_pending` metadata bit gates readiness and all data-plane operations;
management verification remains available until commit or offline rollback.
