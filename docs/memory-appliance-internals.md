# Memory Appliance Internal Reference Design

Status: non-normative design direction. This document cannot expand the public
contract or block Core Profile delivery.

## Baseline engine

The durable source of truth is an immutable receipt payload. Mutable processing
status is separate:

```text
ReceiptPayload (immutable)
  receipt ID, Space ID, text, normalized source context
  occurred time, received time, idempotency key, request digest, commit sequence

ReceiptProcessing (mutable or append-only events)
  state, attempts, last attempt, terminal error, semantic generation
```

The minimum retrieval projection lexically indexes every receipt for its whole
lifetime. It is partitioned by Space or uses a backend that can prove equivalent
pre-filtering. It is disposable and rebuildable from receipts.

## Optional semantic organization

After the durable receipt path is proven, a semantic extension may add:

```text
Record
  current interpreted content and status
Revision
  immutable version of one Record
Evidence
  same-Space links to receipts or authorized human corrections
```

Record taxonomy is deliberately not frozen by v1alpha1. Claim, Episode, Summary,
and Reference are useful hypotheses, not required database enums. New schemas
must be justified by retrieval or governance evidence and introduced through
migrations.

Recall may merge receipt and record candidates, but it deduplicates presentation
without erasing receipt evidence. If the semantic store, model, or jobs are
unavailable, receipt-only Recall remains correct.

## Steward execution

A logical Steward is a versioned profile plus durable jobs executed by a shared
worker pool. It is not a permanent conversation or one process per identity.

The first proposal vocabulary is intentionally small:

```text
ADD
MERGE
SUPERSEDE
IGNORE
```

The model proposes; deterministic application code validates same-Space
evidence, operation bounds, base revision, size limits, and visibility before
committing. A model cannot edit receipt payloads, change ACL/View/Grant state,
publish private data, tombstone, or hard-delete.

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

## Local durable Core

M1 uses one migrated SQLite database for many Spaces, WAL, `synchronous=FULL`,
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

The first local transport is HTTP over an owner-only Unix Socket. Health is
process liveness; readiness additionally reaches SQLite. This is an M1 local
implementation, not a promise that a future remote backend uses SQLite or the
same physical index layout. Another backend must pass the same semantic and
durable conformance appropriate to its milestone.

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
