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

## Local deployment direction

M1 is expected to use one SQLite database for many Spaces, WAL where supported,
transactional receipts and idempotency state, an FTS projection, migrations,
and an owner lock. This is a reference direction, not an API promise. A future
remote implementation may use different storage while passing the same
semantic and durable conformance suites appropriate to its milestone.
