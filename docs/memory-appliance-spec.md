# Memory Appliance Specification

Status: normative `memory.v1alpha1` data-plane and
`memory.management.v1alpha1` owner-management contracts.

This document owns the stable boundary between an Agent host such as Caelis and
the independently running Memory Appliance. Internal algorithms and future
storage design are intentionally separated into
[Memory Appliance Internals](memory-appliance-internals.md).

## Purpose

An Agent interacts with memory through only two model-visible operations:

```text
remember(text)
recall(query)
```

The appliance owns what memory means, how it is classified and organized, when
it becomes active or cold, how conflicts are represented, and how retention and
forgetting work. Hosts bind those two operations to an authorized Runtime; they
do not expose identity, Space, View, retrieval strategy, or lifecycle controls
as model arguments.

## Required properties

- A successful Remember is acknowledged only after the owning service has
  durably committed its immutable receipt payload.
- An unknown Remember outcome is retried with the same idempotency key.
- A successful Remember is immediately visible to a following authorized Recall
  without waiting for classification, a model, or a Steward.
- Every accepted receipt remains eligible for baseline lexical retrieval for as
  long as it exists, even if no Steward ever runs.
- Private and shared Spaces are isolated before candidate generation.
- A Recall fragment is evidence, not an instruction or authorization source.
- A host persists the exact model-visible ToolResult and replays it without
  contacting Memory.
- Capability, credential, identity, and audience cannot be selected by model
  arguments or untrusted source metadata.
- Derived memory is versioned, evidence-backed, and cannot widen visibility.

## Non-goals

Memory is not a Session transcript, context-compaction format, message bus, task
queue, workflow state machine, approval ledger, lock, or execution receipt. It
does not scan a host conversation; facts enter only through an explicit
Remember call.

The Core Profile does not include public memory, mixed-audience Sessions,
private-to-shared publication, semantic records, model-assisted Recall,
temperature, bitemporal queries, automatic retention, graph/vector indexes,
remote federation, or a management UI. Those may be added without changing the
two Agent operations or weakening the invariants above.

## Ownership boundary

The host owns:

- stable Runtime actor identity and one Runtime output audience;
- actor-to-Memory binding and acquisition of a temporary capability;
- tool admission, hidden request metadata, and model-visible projection;
- exact ToolCall and ToolResult persistence and replay;
- service discovery, lifecycle, version pinning, and product diagnostics;
- prevention of disclosure after private memory enters model context.

The Memory Appliance owns:

- Realms, cognitive Identities, Spaces, View definitions, grants, and
  capabilities;
- receipt payloads, processing state, consistency cursors, and retrieval;
- classification, semantic organization, models, prompts, lifecycle, retention,
  and forgetting;
- all indexes and projections;
- data-plane and management-plane authorization;
- migrations, backup, restore, inspection, and deletion of appliance data.

A host may depend on a versioned SDK. It must not import Memory storage, index,
domain, or Steward implementation packages and must not maintain a compensating
Memory journal.

## Core Profile

The first end-to-end profile deliberately supports only:

```text
OutputAudience = private | shared
SpaceClass     = private | shared
one Runtime actor
one canonical Session audience
one View definition per Runtime binding snapshot
```

A private Runtime reads its actor's private Space plus shared Spaces and writes
to its private Space. A shared Runtime reads and writes shared Spaces only. The
Core Profile has no `remember_shared` tool. Producing a shared memory from a
private fact requires a separate shared-safe context or a future explicit
declassification flow.

Public Spaces, mixed-audience history, multiple actor bindings in one Runtime,
and autonomous private-to-shared publication are incompatible with this
profile and must fail closed.

## Domain model

| Object | Meaning | Decides authority |
| --- | --- | --- |
| `MemoryRealm` | Administrative root for a user, team, or tenant | Indirectly |
| `MemoryIdentity` | Stable cognitive continuity, normally associated with one Bot | No |
| `MemorySpace` | Storage, access, and retention boundary | Yes |
| `MemoryViewDefinition` | Read Spaces, optional write Space, maximum disclosure, and recall-policy reference | Defines accessible data |
| `MemoryGrant` | Principal, actor, permitted operations and audiences, expiry, and revocation for one View | Delegates use |
| `RuntimeCapability` | Opaque temporary bearer proof derived from a current Grant | Authorizes one call |
| `SourceContext` | Bounded untrusted audit metadata | No |

The three authorization objects have non-overlapping roles:

```text
View       what can be read and where Remember writes
Grant      who may use that View and for which operation/audience
Capability proof that the current caller received that delegation
```

View, Grant, Identity, actor, Session, Workspace, and Space IDs are references,
not credentials.

### Identity continuity

Changing a Session, Workspace, Runtime activation, or model does not change a
MemoryIdentity. Identity replacement is an explicit clone, fork, migrate, or
detach operation. Deleting a host-side Bot revokes its bindings but does not
implicitly delete appliance-owned data.

## Authorization contract

Every data-plane call carries an opaque Runtime capability outside the request
body. Before accessing a receipt or index, the service verifies that the
capability is current and binds:

- service instance or service audience;
- principal and Runtime actor;
- View definition and version;
- requested operation;
- Runtime output audience;
- expiration and non-revoked state.

The Core Profile does not prescribe signed tokens, JWT, key rotation, or a
remote issuance protocol. Its local reference semantics use a random opaque
token and server-side state. A later implementation may use mTLS or signed
tokens without changing Remember or Recall request bodies.

Capability issuance authenticates the delegating principal independently of the
Grant reference. The resulting server-side capability state binds that
principal. Merely knowing a Grant, View, actor, Session, or Space reference can
never redeem a capability.

Expired, revoked, wrong-actor, wrong-operation, wrong-audience, unknown, and
incompatible capabilities fail closed. Capabilities and endpoint credentials
must not enter a Session, model context, ordinary diagnostic log, or API error.

## Data-plane contract

The protocol identifier is `memory.v1alpha1`. Empty Recall is a successful
response containing zero fragments. Service failure is an error and must never
be projected as empty memory.

### SourceContext

```text
SourceContext
  actor_ref?
  session_ref?
  workspace_ref?
  task_ref?
  tool_call_ref?
  source_type?
  extension_labels?
```

All fields are untrusted audit metadata. They cannot choose a Realm, Identity,
Space, View, principal, capability, audience, or policy. In v1alpha1:

- each scalar is at most 256 UTF-8 bytes;
- there are at most 16 extension labels;
- each label key is at most 64 bytes and each value at most 256 bytes;
- the normalized structure is at most 4 KiB;
- unknown or oversized content is rejected rather than silently truncated.

### Remember

```text
RememberRequest
  text                 required, UTF-8, 1..65536 bytes
  source_context       optional
  occurred_at          optional RFC 3339 timestamp
  idempotency_key      required, 1..128 bytes

RememberResponse
  accepted             always true on success
  receipt_id
  consistency_token
  deduplicated_retry
  processing_state
```

The model sees only:

```json
{"accepted":true}
```

`processing_state` describes optional downstream interpretation. It does not
weaken the durability of the accepted receipt.

### Receipt status

```text
GetReceiptStatusRequest
  receipt_id

ReceiptStatus
  receipt_id
  state                 accepted | processing | organized | failed
  accepted_at
  last_attempt_at?
  terminal_error_code?
  semantic_generation?
```

Status lookup is authorized by the same readable Space boundary as Recall.
Mutable processing state is not part of the immutable receipt payload.

### Recall

```text
RecallBudget
  max_fragments         1..64
  max_bytes             16..65536
  deadline_ms           1..30000

RecallRequest
  query                 required, UTF-8, 1..8192 bytes
  source_context        optional
  min_consistency_token optional
  budget                required

RecallFragment
  fragment_id
  text
  evidence_refs[]
  record_refs[]         empty until a semantic extension exists
  space_class           private | shared

RecallResponse
  fragments[]
  consistency_token?
  degraded
  truncated
```

The model sees only the ordered fragment texts:

```json
{"fragments":["first supported fact","second supported fact"]}
```

There is no `snapshot_id` in v1alpha1. Hidden response metadata may include the
endpoint instance, API version, View reference/version, evidence references,
response digest, degradation state, and consistency token.

`max_bytes` covers the complete encoded model-visible Recall result, including
the JSON envelope and escaping, rather than only raw fragment text. The service
truncates extractive text on a UTF-8 boundary; the host SDK performs a final
hard check before exposing the bytes to a model. `deadline_ms` is combined with
any earlier caller deadline and applies through candidate scanning, ranking, and
result encoding.

## Error envelope

Failures use a stable bounded envelope:

```text
ServiceError
  code
  message
  retryable
  request_id
  retry_after_ms?
  details?              typed, bounded, and free of secrets or receipt text
```

Core codes are:

| Code | Meaning |
| --- | --- |
| `invalid_argument` | Request violates a public bound or required field |
| `unauthorized` | Capability is absent, invalid, expired, revoked, or insufficient |
| `incompatible` | API or Core Profile is incompatible |
| `not_found` | An authorized status/management reference does not exist |
| `conflict` | Idempotency key or expected version conflicts |
| `deadline` | Work did not complete before the deadline |
| `unavailable` | The service could not perform the operation |
| `unknown_outcome` | A mutating request may have committed; retry the same effect identity |
| `stale_consistency_token` | The causal cursor is unknown or belongs to another storage generation |
| `internal` | An unexpected failure occurred without exposing sensitive details |

No-result Recall is not an error code.

## Receipt, idempotency, and consistency

The immutable receipt payload contains the receipt ID, target Space, raw text,
normalized SourceContext, occurred time, service received time, idempotency key,
request digest, and commit sequence. Processing state is separate mutable state
or an append-only processing event stream.

Effect identity is:

```text
write_space_id + idempotency_key
```

The request digest covers text, occurred time, normalized SourceContext, and
every other effect-bearing field. Therefore:

```text
same write Space + same key + same digest -> original receipt
same write Space + same key + changed digest -> conflict
same key in a different write Space -> independent effect identity
```

Capability renewal, Grant renewal, or a compatible View version change does not
create another effect within the same write Space. The new capability must still
authorize that Space. Idempotency lookup is Space-partitioned and cannot reveal
whether the same key exists in another Space.

Different idempotency keys preserve distinct receipts even when text matches;
semantic deduplication must never erase independent evidence.

A consistency token is an opaque causal cursor, not authority. Recall accepts
it only if the capability can read the token's Space. A stale storage generation
returns `stale_consistency_token`. The host may persist the token only as
audience-protected, model-hidden ToolResult provider metadata.

## Baseline Recall

The availability path is:

```text
immutable receipts
  -> rebuildable per-Space lexical projection
  -> authorization-compatible candidate generation
  -> deterministic bounded ranking
  -> extractive fragments with receipt evidence
```

Every existing receipt is indexed, not only a recent inbox. Each readable Space
is queried independently and results are merged after per-Space candidate
generation. A shared-only View must not touch a private index. Deleting the
projection and rebuilding it from receipts must preserve Recall behavior within
documented ranking stability.

Semantic records and Steward output are optional enhancement candidates. Recall
must remain useful and read-your-writes must remain correct when those systems
are disabled or unavailable.

## Owner-management plane

The local management protocol is `memory.management.v1alpha1`. Its bearer is
carried outside request bodies and is independent from issuer credentials and
Runtime capabilities. The owner-management client can bootstrap topology,
inspect the appliance, search and trace receipts, correct or delete receipt
content, rebuild projections, revoke Grants, and rotate issuer credentials.
None of these operations is model-accessible.

Inspection returns only bounded topology and operational aggregates: storage
and filesystem byte counts, receipt processing counts and time range,
projection health and drift, capability counts, restore state, and rollback
availability. It contains neither receipt text nor bearer values. A missing or
drifting disposable projection is a diagnosable state and does not erase the
canonical topology needed to repair it.

Management search is lexical and may be restricted to one Space. It returns
receipt text and audit metadata, so its output is sensitive operator data even
though it contains no bearer. Corrected originals are hidden by default but may
be included for an explicit audit search. Trace resolves a Recall evidence
reference to active receipt content, its correction links, or a content-free
deletion tombstone.

Correction never updates an immutable receipt. It appends a replacement receipt
in the same Space, records the relation and audit reason, and excludes the
original from baseline Recall. The replacement has its own receipt ID,
consistency token, immutable payload, and processing state. Rebuilding the FTS
projection retains both receipt payloads while Recall applies the durable
correction relation.

Deletion first records a content-free tombstone containing the receipt and
Space references, original effect identity and digest, deletion time, and audit
reason. It then removes receipt text, processing state, and projection entries
in the same transaction. The retained effect identity makes an old Remember
retry conflict rather than recreate deleted content; remembering the fact again
requires a new host effect identity. Delete and correction requests have their
own idempotency keys and return the original result on an exact retry. A changed
request under the same management key conflicts.

Management authorization is root authority for this local profile. A rich
product Surface must call this plane rather than mirror appliance state or
policy in Caelis Control.

The fixed owner-only management token is rotatable without returning its raw
value over the API: the manager writes a replacement at the same protected
path, revokes the old digest, and returns only a success receipt. Durable
pending-digest recovery makes interruption before or after the atomic file
rename deterministic on restart. Rotation remains available while a restored
generation is pending so an owner can recover authority; topology, issuer,
Grant, correction, deletion, and Runtime capability mutations remain disabled
until that generation is committed.

### Export, backup, restore, and generation

`memory.management.v1alpha1` exports evidence as `memory.export.v1` NDJSON. The
first record binds the format, storage generation, and creation time; following
records contain management-visible receipts or content-free tombstones. Export
is plaintext and therefore always written by `memoryctl` to a new owner-only
file. Space filtering, corrected-original inclusion, and tombstone inclusion
are explicit request choices.

Backup is a consistent SQLite snapshot streamed only over the owner-only local
Socket. `memoryctl` encrypts the stream before durable output using a random
256-bit key and independently authenticated chunks. The key is written to a
distinct new owner-only file and is never stored in the appliance database or
backup container. A backup without its key is unrecoverable; co-locating the
two files defeats the confidentiality boundary.

Restore is offline and must acquire the same owner lock as `memoryd`. It fully
authenticates the encrypted stream, verifies SQLite integrity and foreign keys,
applies supported forward migrations, binds the operator-provided management
credential, and rotates `storage_generation` before atomically replacing the
database. Any failure before replacement leaves the current database usable.
An existing database is first copied to a consistent owner-only rollback image.

A restored generation starts in `restore_pending`: health and management
inspection/search are available, but readiness, Remember, Recall, and receipt
status fail as unavailable. The operator either stops the process and restores
the rollback image, or calls the authenticated online `restore-commit`
operation. Commit removes the rollback image before enabling readiness and the
data plane. Consequently no receipt can be acknowledged in a generation that
is still eligible for rollback. Restore and rollback each rotate generation;
all prior consistency tokens fail with `stale_consistency_token` rather than
being interpreted against different state.

An upgrade uses the same pending-generation barrier. With the service stopped,
the old `memoryctl prepare-upgrade` authenticates the owner, snapshots the exact
stopped database into the rollback image, and marks the live database pending
before a new binary may migrate it. The new binary exposes health and
authenticated verification operations but cannot acknowledge a receipt or
other governed mutation. Acceptance commits through `restore-commit`; failure
is rolled back with the old `memoryctl`, preserving every effect acknowledged
before shutdown.

## Replay and deletion boundary

The host persists the exact model-visible Memory ToolResult plus bounded hidden
provider metadata. Replay uses that value byte-for-byte and makes zero Memory
API calls. A new Session may observe evolved memory; old Session history retains
what the model saw at the time.

Appliance deletion removes only data owned by the appliance. Recall text already
materialized into a host's canonical Session belongs to that Session history.
Complete erasure therefore requires a separate host-side Session deletion or
redaction workflow; Memory hard delete must never claim to erase those copies.

## Evolution

The M2 local host profile performs an exact compatibility handshake before a
Runtime receives Memory tools. The request names `memory.local.v1alpha1`,
`memory.v1alpha1`, and `memory.core.v1alpha1`; the service rejects any mismatch
with `incompatible` rather than negotiating a weaker profile. A successful
response reports the packaged service version, exact source revision, and
diagnostic storage schema version. The host separately verifies the native
binary SHA-256 from a pinned sidecar manifest before launch and compares the
handshake build identity with that manifest after readiness.

Buildability is not a support claim. A release line separately publishes its
native support matrix; the M3 Alpha matrix contains only `darwin/arm64`.
Supported packaging rejects an artifact outside that matrix even when Go can
cross-compile it.

Runtime capability issuance is a separate issuer-plane operation. Its request
contains principal, Grant, actor, audience, operation, and TTL references while
the principal credential remains an out-of-band bearer. Management and Runtime
bearers cannot act as issuer credentials. Renewal issues a fresh temporary
capability for the same immutable binding; it never changes Remember effect
identity.

Additive v1alpha1 fields must be optional and ignored safely by compatible
readers. Effect-bearing changes require a new request digest rule and protocol
version. Public memory, mixed audiences, new Space classes, semantic records,
and publication require explicit profile negotiation; they cannot silently
appear in the Core Profile.

The API may become stable `v1` only after a durable standalone service and the
Caelis Golden Path pass end to end. M0 semantic compatibility is demonstrated
by the shared semantic suite; durable compatibility additionally requires the
M1 crash/restart harness. Neither is inferred from matching type names.
