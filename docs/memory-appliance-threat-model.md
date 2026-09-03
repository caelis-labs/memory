# Memory Package Threat Model

Status: source-linked Core Profile security baseline. Standalone transport is a
retained but deferred adapter and is not a Caelis runtime dependency.

## Protected assets

- private receipt text and source context;
- shared receipt integrity;
- Realm, Identity, Space, View, Grant, and LabelSet capability configuration;
- issuer credentials and Runtime capabilities;
- consistency and idempotency semantics;
- Recall provenance and separately persisted Caelis Session copies;
- Steward Jobs, evidence, proposals, and semantic Records;
- backup, migration, deletion, and storage-generation state.

Post-v0.5 Corpus work additionally protects admitted LeafRevisions and Items,
direct indexes, manifests, summary/index artifacts, projection generations,
and exact support provenance. These assets are not part of the v0.5 GA surface.

## Current trust boundaries

```text
model output and Agent arguments       untrusted
Caelis Host composition                selects actor, audience, View, and Grant
Runtime capability                     temporary bearer authority
Memory public API                      versioned semantic boundary
Memory authorization layer            access enforcement before candidates
Memory SQLite store                    canonical Memory authority
Steward ModelGenerator output          untrusted provider text
Caelis Session store                   separate replay authority
```

Caelis imports public `appliance`, `api`, and `sdk` packages. It does not open
Memory tables or import `internal/*`. Direct Go calls remove process discovery,
download, manifest, endpoint, and sidecar-impersonation threats from the Caelis
path; they do not weaken Memory's logical authorization boundary.

Successful embedded `appliance.Open` means the database is at the current baseline, queryable,
and not awaiting restore commit. Failure is an ordinary Host construction
error. There is no partially ready embedded Memory state.

## Primary threats and controls

| Threat | Required control |
| --- | --- |
| Agent chooses another identity, Space, or LabelSet | Agent schemas omit authority fields; the Host-selected capability fixes one View and one canonical LabelSet |
| Shared query leaks private candidates | Authorize and select per-Space indexes before candidate generation |
| Private context flows to shared output | Caelis binds one immutable audience and rejects incompatible sinks |
| Stolen or stale capability is reused | Short lifetime, stored digest, explicit revoke, and fail-closed validation |
| Grant reference is treated as a credential | Authenticate the issuer principal separately before capability issuance |
| Capability or issuer credential enters history | Keep bearer bytes outside request bodies, ToolResults, AppConfig, and logs |
| SourceContext forges authority | Treat it as bounded audit metadata only |
| Retry duplicates a receipt | Bind a stable Space/idempotency key to one request digest, including the selected LabelSet |
| Consistency token grants access | Reauthorize the token's Space and exact LabelSet on every Recall |
| Steward invents or widens facts | Require same-Space and same-LabelSet evidence, closed operations, byte bounds, and deterministic proposal validation |
| ModelGenerator receives Memory authority | Pass only bounded model-facing work; keep lease, Space, View, Grant, actor, audience, and bearers outside the callback |
| Provider egress exposes private receipts | Provider selection and egress belong to an explicitly bound downstream Host model; zero-token static mode is the default |
| Worker outage blocks Memory | Durable receipt and lexical Recall remain independent from Steward Jobs |
| Error or metric exposes content | Emit typed codes, counts, sizes, and opaque identifiers only |
| Memory delete is mistaken for global erasure | Disclose that canonical Session copies require their own deletion workflow |
| Restore reuses an invalid causal cursor | Rotate storage generation and return a stale-token error |
| Corrupt backup replaces live data | Authenticate, integrity-check, migrate, and stage fully before atomic replacement |
| Rollback loses acknowledged writes | Keep a restored generation unavailable to embedded Open until explicit commit |

## Post-v0.5 Corpus and projection boundary

The Corpus ledger accepts already-admitted LeafRevision requests from a trusted downstream
producer through a public source-neutral protocol. The producer owns source
format, parsing, admission, sanitization, checkpointing, migration, versioning,
and source deletion. Memory neither knows nor verifies those domain semantics.
It validates capability, exact partition, structure, sizes, digests,
idempotency, and immutable revision rules.

The external source remains producer-owned authority. An accepted LeafRevision
is Memory's durable Corpus evidence. RollupManifests,
SummaryArtifactRevisions, IndexArtifactRevisions, and ProjectionSnapshots are
derived projections. A Caelis producer may map one Session JSONL projection to
one Leaf, but that mapping adds no Session boundary to Memory.

| Threat | Required control |
| --- | --- |
| A producer admits secret or instruction-bearing content | Keep source admission and sanitization in the producer, bind the request to a preselected exact partition, and never treat stored text as authority or instructions |
| Malicious Leaf content poisons a summary artifact | Bound every Worker input and proposal, require exact support citations, and treat summarizer output as untrusted |
| A manifest references another Space or LabelSet | Authorize before building or querying, require exact partition equality on every reference, and verify it again at artifact apply |
| A changed or deleted Leaf leaves a stale artifact searchable | Invalidate every dependent artifact before advancing its visible generation; atomically publish only coherent ProjectionSnapshots |
| Leaf erasure leaves source text in historical artifacts or a retry resurrects it | Purge Item text and every transitively derived payload before retrieval resumes; retain only content-free tombstones, digests, and idempotency evidence that reject delayed replay |
| Producer retry or source-reference reuse silently changes a leaf | Bind canonical request digest, opaque source/version/projection references, prior head, and idempotency key; fail closed when the same key names another digest |
| A caller uses opaque references to smuggle authority | Never branch on SourceRef, SourceVersion, ProjectionRef, or ItemRef contents; authority comes only from the authenticated capability |
| Projection or query work exhausts Host resources | Bound materialization work and independently cap candidates, expansion, returned Items, bytes, summarizer calls, time, and retries; keep topology parameters versioned |
| A derived summary is mistaken for source evidence | Mark summaries as derived and retain exact manifest, support, LeafRevision, Item, and opaque source provenance in every result |

These controls gate P3-P5. Corpus and projection work uses separately versioned
public contracts. Optional projections may be disabled independently; current
flat Recall and the future direct Corpus query remain separate availability
paths.

## Steward boundary

The Memory package owns prompt-policy profiles, input/output budgets, durable
Jobs, leases, attempt ceilings, evidence validation, and atomic application.
The downstream Host owns provider choice, credentials, billing, retention, and
provider-specific limits. The injected ModelGenerator has no tools, receives the
Memory-rendered request, and returns only untrusted provider text. Memory owns
proposal extraction and parsing. With no explicit model binding, no provider
call occurs and all accepted receipts remain retrievable through the static
path.

## Replay and deletion boundary

The Host persists the exact model-visible Memory ToolResult. Replay reads those
bytes and performs no Memory call. Deleting Memory evidence cannot erase text
already copied into canonical Session history; complete erasure requires the
separate Session deletion or redaction workflow.

## Deferred standalone risks

The retained `memoryd`, local transport, process credentials, manifests, and
packaging introduce additional executable-integrity, endpoint-ownership,
attach, restart, update, and rollback risks. They require a separate threat
model and native acceptance matrix before independent publication. Cross-build
success or historical preview evidence does not close those risks and cannot
become a prerequisite for embedded Caelis.

## Candidate residual risks

- model-backed semantic quality still requires a frozen reviewed corpus and an
  external evaluation; deterministic validation proves safety, not judgment;
- resource exhaustion is governed through ordinary Caelis Host profiling and
  limits rather than a first-version process-isolation subsystem;
- future multi-product binding selection must preserve the current opaque
  `BindingRef` authority boundary and must not make product identity a Memory
  domain concept;
- native Darwin, Linux, and Windows behavior is accepted through the Caelis
  release matrix, with local Linux execution additionally verified on Rocky in
  OrbStack before external review.
