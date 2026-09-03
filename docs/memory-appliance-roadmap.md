# Memory Go Package Roadmap

Status: authoritative implementation and GA plan.

The primary product is the Go package `github.com/caelis-labs/memory`. Caelis
imports it and runs one Memory runtime as part of the Caelis Host. The package,
not Caelis Control, owns Memory schema, data, authorization, retrieval, and
validation of every derived-memory mutation. Downstream products decide when to
invoke promotion, refinement, or later lifecycle interfaces. Memory never owns
their product scheduler or model provider.

The repository retains `cmd/memoryd`, local transport, `memoryctl`, and packaging
code as a future standalone distribution framework. Building, publishing,
installing, supervising, or version-matching those binaries is not part of the
current Caelis integration or GA critical path.

## Current v0.5 baseline: LabelSet and flat organization

This slice is intentionally limited to the Memory repository.

In scope:

- one canonical exact `LabelSet` bound during Runtime capability issuance;
- automatic LabelSet persistence on Remember and exact LabelSet filtering on
  Recall, receipt status, and consistency-token use;
- LabelSet columns and indexes built directly into one current development
  schema baseline; older unreleased local data is rebuilt, not migrated;
- propagation through durable Steward Jobs, same-LabelSet evidence validation,
  flat Records, immutable Revisions, management inspection, and export;
- conformance proving that two LabelSets in one Space cannot recall, inspect, or
  semantically merge each other's evidence;
- documentation freezing the flat Receipt -> Record -> Revision structure.

Out of scope:

- Caelis workspace-label construction or Runtime integration;
- the public tree protocol, leaf commits, and layered tree construction;
- user-visible label configuration or Agent tool arguments;
- tree traversal, a general relation graph, embedding, or hierarchy-aware Recall;
- another scheduler, automatic organization trigger, or model-provider stack.

The slice is accepted when the default empty LabelSet works without caller
configuration, a non-empty LabelSet survives restart, model-facing inputs expose no
labels, and the full package and race suites pass.

## Product boundary

The irreversible boundary is source-level, not process-level:

```text
Caelis Host
  -> appliance.Open(data directory)
  -> sdk Client(DataPlane)
  -> Host issues a capability with one hidden exact LabelSet
  -> remember(text) / recall(query)
  -> optional Steward Generator callback

Memory package
  -> SQLite schema baseline, receipts, indexes, topology, authorization
  -> static zero-token retrieval
  -> durable Steward jobs and deterministic proposal application

Post-v0.5 tree path
  downstream source-specific producer
    -> memory.tree.v1alpha1 generic leaf revision
    -> deterministic parent topology
    -> memory.tree.worker.v1alpha1 summaries and indexes
    -> bounded memory.tree.v1alpha1 query
```

Caelis may import public `api`, `sdk`, and `appliance` packages. It never imports
`internal/*`, opens `memory.db`, mirrors Memory state, or selects Memory
derived mutation rules. Memory never imports Caelis product types
or owns a model provider, model credential, or provider configuration.

Successful Caelis Host construction means the embedded Memory database is open
at the current schema baseline. There is no independent download, install, probe, handshake,
readiness, degraded-start, or dynamic tool-injection state. A Memory open or
schema initialization error is an ordinary Host startup error. Host shutdown closes Memory
after Runtime work drains.

## Product vision

`v0.5.0` remains the flat durable control: immutable receipts, rebuildable
per-Space lexical indexes, flat semantic Record heads, and exact provenance.
The next product line adds a separate generic layered memory tree without
changing those authorities or pretending that one global corpus is canonical.

The former plan for a global canonical Session-corpus projection, a
time-aware ranking policy, and an automatically injected zero-model task
briefing is retired. None of those mechanisms is a release or future milestone.

### Public leaf model

Memory does not understand Session, JSONL, event paging, checkpoints, source
migrations, or sanitization. A downstream producer owns all source-specific
interpretation and submits an already-admitted leaf through a new public
source-neutral protocol. The first Caelis producer maps one concrete Session
JSONL projection to one leaf, but neither `Session` nor `JSONL` appears in the
Memory protocol.

The first `memory.tree.v1alpha1` leaf revision contains:

```text
TreeID / LeafID / Revision
SourceRef              opaque producer-owned logical source identity
SourceVersion          opaque producer-owned immutable source version
ProjectionRef          opaque producer-owned projection-policy version
Items[]                ordered ItemRef, text, and content digest
ContentDigest          digest of the complete canonical leaf request
Slot                   stable Memory-assigned position in the tree
State                  active, retracted, or conflicted
```

The producer is accountable for projecting, sanitizing, bounding, versioning,
and retracting source content. Memory validates protocol shape, byte budgets,
digests, idempotency, expected head, partition authority, and immutable revision
rules; it never infers what the source fields mean. The same idempotency key and
request digest returns the original revision. Changing an existing key's digest
conflicts. A new accepted revision advances the leaf head and dirties only its
ancestor path.

### Layered parent nodes

Every parent is derived only from an ordered, same-partition child set:

```text
NodeID / Revision
Level
ChildRefs[]           exact child NodeID and Revision pairs
ChildSetDigest        ordered digest of ChildRefs and policy version
SummaryBlocks[]       bounded derived text -> exact child refs
IndexEntries[]        bounded term -> child refs
IndexState            ready or invalidated
SummaryState          pending, ready, degraded, or invalidated
```

The topology builder, not a model, selects children. The first implementation
uses a fixed fan-out of 16 and stable Memory-assigned leaf-slot order. Slots are
never reused or compacted; a retracted leaf retains a content-free tombstone
revision. Parent identity is derived from tree, level, and slot group, so an old
leaf update changes one ancestor path instead of reparenting later leaves. The
builder creates one parent for each complete child group and one frontier group
per level. Parent revisions and edges are immutable; only node heads and the
bounded dirty queue advance. The root is a derived pointer, not another
evidence authority. Owner-authorized content erasure is the sole exception: it
removes affected text/index payloads while retaining content-free revision and
edge tombstones.

Nodes never cross `(SpaceID, LabelSetDigest)`. Child references are checked
before a summary job is created and again before its result is applied. A
parent whose child revision changes is unavailable for retrieval until the new
revision's deterministic index is ready; its summary may still be pending. The
previous revision remains audit evidence but cannot silently represent the new
subtree.

The deterministic parent index is built from the same fixed analyzer as the
leaf index. It maps each retained term to the exact immediate children that
contain it and applies fixed per-child and per-node term limits. It never copies
a child's opaque source metadata into ranking. An accepted summary proposal may
add cited expansion terms, but failure to obtain a summary leaves the
deterministic index usable.

### Public protocols

The tree surface is a system-facing package contract, not another Agent tool:

```text
memory.tree.v1alpha1
  CommitLeaf / RetractLeaf / GetNode / RequestRefinement / Query

memory.tree.worker.v1alpha1
  ClaimBuild / ApplyProposal / FailBuild

memory.tree.management.v1alpha1
  CreateTree / DeleteLeaf / DeleteTree
  InspectTree / RebuildProjection / ExportTree
```

`CommitLeaf` accepts only the generic leaf model above. It contains no path,
Session ID, event sequence, checkpoint, sanitizer rule, provider, model, or
credential field. A producer may encode its own stable identity and version in
opaque references, but Memory does not branch on their contents.

The intended request/response shapes are:

```text
CommitLeafRequest
  tree_ref, leaf_ref?
  expected_revision?
  source_ref, source_version, projection_ref
  items[] { item_ref, text, content_digest }
  idempotency_key
CommitLeafResponse
  leaf_node_ref, revision, content_digest, deduplicated_retry

RetractLeafRequest
  tree_ref, leaf_id, expected_revision, reason_code, idempotency_key
RetractLeafResponse
  leaf_node_ref, revision, deduplicated_retry

RequestRefinementRequest
  tree_ref, node_ref, expected_revision, summary_policy_ref, idempotency_key
RequestRefinementResponse
  build_ref, target_node_ref, deduplicated_retry

BuildWork
  build_ref, operation = summarize | refine
  target_node_ref, base_summary_ref?, level, child_set_digest
  children[] { exact_node_ref, kind, bounded_content, index }
BuildProposal
  summary_blocks[] { text, exact_child_refs[] }
  index_entries[] { term, exact_child_refs[] }

QueryRequest
  tree_ref, query
  max_depth, max_nodes, max_children, max_items, max_bytes
  result_mode = leaf_items | allow_derived_summaries
QueryResponse
  results[] { kind, text, node_ref, leaf_item_refs[], source_refs[] }
  visited_nodes, truncated, degraded
```

Space, LabelSet, actor, audience, View, Grant, lease, provider, model, and
credentials stay outside model-facing payloads. The authenticated binding
selects the writable/readable partition; a `tree_ref` is only an object
reference within that authority. `reason_code` is a bounded lifecycle category,
not arbitrary source content.

Management creates a Tree under one exact Space and LabelSet and fixes its
topology/index policy version. Issued capabilities grant distinct tree commit,
retract, read, refine, or query operations; ordinary Remember/Recall authority
does not implicitly grant them. This makes the protocol safe for at-least-once
producers without requiring Memory to understand their checkpoint or
transaction model.
`RetractLeaf` is reversible lifecycle state and retains historical content for
owner audit. Owner-authorized `DeleteLeaf` is erasure: it keeps only a
content-free tombstone and removes leaf text plus every derived summary/index
payload whose provenance reaches that leaf before retrieval resumes.

The current `memory.v1alpha1` Remember/Recall and
`memory.steward.v1alpha1` Record proposal protocols remain unchanged. Tree work
must not reinterpret a Receipt or semantic Record as a tree node.

### Summary and index execution

Leaf indexing and parent topology are deterministic. A downstream summarizer
may propose a parent summary and additional index terms through the public
Worker protocol. The proposal receives only the fixed child revisions selected
by Memory and returns bounded text plus exact child citations. It cannot choose
topology, Space, LabelSet, visibility, lifecycle, deletion, or publication.

Memory owns the prompt profile, input/output budgets, parser, child-set digest,
lease, retry ceiling, and atomic application. The Host owns provider, model,
credential, billing, and scheduling. One node revision has at most one accepted
summary effect. Summarizer failure leaves the node pending or degraded and never
blocks current receipt Recall or Host startup. There is no zero-model promise
for tree summarization: cost is measured per materialized node revision and is
incurred only by producer-triggered dirty work or an explicit Host action,
never by wall clock time alone.

`RequestRefinement` may create a new summary revision for the same exact child
set under a new summary-policy reference. The Worker receives the current
summary as an optional base and may improve only summary blocks and cited
expansion terms. It cannot change child edges, deterministic index entries, or
the node's partition. A stale target or unchanged request is rejected or
deduplicated.

### Tree retrieval and authority

Tree retrieval is explicit; it does not inject an automatic task briefing. The
query path authorizes the exact Space and LabelSet before reading any tree
index. It starts from the current partition root, scores only exact immediate
children named by matching parent index entries, and descends best-first until
the budget or leaf result limit is reached. Every hop revalidates the active
node revision and partition before returning leaf items or clearly marked
derived summaries with complete node and opaque source provenance. Initial
traversal is lexical with stable node-reference tie-breaks; there is no recency,
decay, active-window, or universal relevance score.

Budgets independently bound depth, visited nodes, candidate children, returned
items, and encoded bytes. Empty and partially materialized trees are valid: the
caller can continue using the current flat Recall path. Query does not become a
default product path until it demonstrates material benefit over searching all
authorized leaves.

### Rebuild and lifecycle

Accepted leaf revisions are the tree's durable base layer; parent summary,
index, edge-head, and root tables are disposable projections. Memory can rebuild
all upper levels from leaf revisions without rereading an external source or
calling a summarizer for deterministic indexes. Restoring summary text may
reuse an accepted proposal for the same child-set/policy digest or leave the
parent degraded until a Worker supplies one.

The producer remains authoritative for source correction, replacement,
redaction, and deletion and expresses each through `CommitLeaf` or
`RetractLeaf`; erasure additionally invokes owner-authorized `DeleteLeaf`.
Memory never mutates or reopens the external source. Skills,
procedures, credentials, files, and Session logs retain their existing owners.
Embeddings and general relation graphs remain separate experiments rather than
hidden dependencies of the tree.

## Golden Paths

### Durable tool path

1. Start Caelis with an empty Store.
2. Host synchronously opens Memory and provisions one private Identity, Space,
   View, Grant, issuer, and opaque default binding.
3. The model sees exactly `remember(text)` and `recall(query)`.
4. Remember `commit does not authorize push` and immediately Recall it.
5. Restart Caelis and Recall the same fact from a new Session.
6. Replay the original Session byte-for-byte without repeating a Memory call.
7. With no Steward model binding, static receipt/lexical behavior consumes zero
   model tokens.
8. Bind the system-managed Memory Steward, Remember a new fact, and prove the
   downstream callback produces an appliance-validated semantic Record.
9. Remove the binding and prove later receipts remain available through the
   static path without model calls.

### Public layered-tree path (post-v0.5.0)

1. A producer projects any owned source into bounded ordered leaf items and
   commits one revision through `memory.tree.v1alpha1`.
2. Memory validates capability, exact Space/LabelSet partition, request digest,
   idempotency, expected head, and byte budgets without interpreting the source.
3. Replaying the same request is idempotent; a new source version advances the
   leaf, while key reuse with another digest conflicts.
4. Memory deterministically groups 16 ordered same-partition child revisions,
   builds their bounded index, and optionally accepts one cited summary
   proposal for that exact child-set digest.
5. An explicit authorized query traverses bounded parent indexes and returns
   leaf items or marked summaries with complete node and opaque source
   provenance.
6. A producer revision or retraction invalidates only the affected ancestor
   path, which is rebuilt bottom-up from committed child revisions.

Future product concepts may select another opaque `BindingRef`. Bot, user,
tenant, workspace, and product identity do not enter the Memory API.

## SDLC and milestone map

```text
P0 Package Boundary
  -> P0.1 LabelSet and Flat Record Baseline
    -> P1 Embedded Caelis Feature
      -> P2 v0.5.0 GA Closure
        -> P3 Public Tree Model and Leaf Protocol
          -> P4 Layered Rollup Tree
            -> P5 Tree Retrieval and Identity Views

Optional Steward Quality (cross-cutting, non-blocking for the static path)

Standalone Distribution (deferred, independent of P0-P5)
```

| SDLC stage | Milestone | Independently reviewable result |
| --- | --- | --- |
| Architecture | P0 | Public embedded facade over the durable authority |
| Context partition | P0.1 | Capability-bound LabelSet and flat derived-memory structure |
| Construction and integration | P1 | Default Caelis Remember/Recall works without a sidecar |
| Release closure | P2 | The current flat embedded package reaches v0.5.0 GA without adding a new product mechanism |
| Public tree foundation | P3 | A source-neutral protocol commits immutable, partition-bound leaf revisions |
| Hierarchical organization | P4 | Same-partition child revisions roll up into deterministic summary and index nodes |
| Retrieval and product views | P5 | Explicit bounded tree traversal proves value before public Query or an identity view ships |

## Implementation status — 2026-09-03

| Milestone | State | Remaining independent review slice |
| --- | --- | --- |
| P0 | Complete at package scope | Public facade, SDK conformance, default-path lexicon retirement, close concurrency, and CI package checks are implemented; external review remains part of P2 |
| P0.1 | Complete in Memory | LabelSet baseline, exact data-plane and Steward partitioning, flat-structure contract, and the 224-case multilingual package gate are complete; Caelis workspace injection is the next slice |
| P1 | Technical integration complete | Default embedded tools, persistence, Replay safety, and system-managed Steward binding exist |
| P2 | Release closure, not yet accepted | Freeze the v0.5 schema and API, remove the deprecated pre-GA Generator surface, pass the exact downstream/platform matrix, and complete external review and formal release acceptance |
| P3 | Planned after v0.5.0 | Freeze the source-neutral public tree data model, leaf protocol, and conformance suite; a Caelis Session projector is a downstream integration |
| P4 | Planned after P3 | Deliver deterministic fan-out, immutable parent revisions, cited summaries/indexes, invalidation, and bottom-up rebuild |
| P5 | Deferred until P4 evidence | Prove bounded tree traversal beats authorized leaf scanning before shipping public Query or building identity views |

Optional Steward quality remains in progress and non-blocking for static
operation. Existing prompt/parser work and corpus reports may inform tree
summary evaluation, but they are not evidence that the tree has been built or
accepted.

The retained evidence reports remain bounded experiments, not GA product
claims. See [Local Memory Registry Corpus Evidence](evidence/memory-registry-corpus-2026-09-02.md),
[Memory Steward Evaluation](evidence/memory-steward-evaluation-2026-09-02.md),
and [Real Corpus and Local Gemma Steward Evidence](evidence/memory-real-corpus-gemma4-2026-09-02.md).

## P0: Package Boundary

### Goal

Make the durable engine a first-class embeddable Go package without exporting
storage implementation details.

### Deliverables

- public `appliance.Open(context.Context, Options)`;
- a narrow Runtime lifecycle with `DataPlane`, owner Management, Steward Worker,
  capability issuance, and `Close` boundaries;
- direct SDK binding to `memory.v1alpha1.DataPlane`;
- one provider-neutral Steward Worker interface implemented by embedded and
  retained local-transport clients;
- package-level Remember/Recall/restart and concurrent-close tests using real
  SQLite;
- `cmd/memoryd` remains a thin optional composition over the same core;
- adaptive lexicon code is internal, explicitly experimental, and absent from
  public Open, default schema initialization, and production data paths.

### Explicit non-goals

- public concrete Store, SQL access, index access, schema API, or tuning flags;
- download, installation, updater, R2/GitHub acquisition, process supervisor,
  compatibility manifest, or platform artifact matrix;
- new cognitive taxonomy, vector/graph search, or model-provider integration.

### Exit criteria

- an external Go consumer imports only public packages and completes durable
  Remember/Recall;
- read-your-writes and restart persistence pass with no transport;
- embedded and retained local-transport paths share the semantic conformance
  suite;
- default paths produce no adaptive terms or related Steward inputs;
- `go test ./...`, race tests, formatting, and diff checks pass.
- the digest-frozen multilingual corpus gates durable retrieval, provenance,
  user-perceived latency, Space isolation, and exact LabelSet isolation.

## P1: Embedded Caelis Feature

### Goal

Ship Memory as an ordinary default Caelis Host capability.

### Deliverables

- Caelis Host synchronously opens Memory under its Store;
- first startup automatically provisions the private default topology;
- direct in-process DataPlane, Management, capability, and Steward calls;
- unconditional Runtime projection of exactly `remember` and `recall` after
  successful Host construction;
- logical binding snapshots retain actor, principal, View, Grant, audience, and
  version, but no endpoint, artifact, build revision, or digest;
- canonical ToolResults and hidden consistency tokens remain replay-safe;
- `/subagent` exposes the system-managed Memory Steward model binding;
- no endpoint, binding, data path, install, runtime location, or Memory binary
  setting is exposed to users.

### Explicit non-goals

- asynchronous Memory startup, partial Host readiness, retry/backoff manager,
  runtime availability state, hot replacement, or process crash isolation;
- CLI/MCP Memory adapters or a public integration Plugin;
- rich management UI or product-level Bot model.

### Review slices

1. Memory public facade and direct SDK conformance.
2. Caelis Host composition, automatic topology, and close lifecycle.
3. Runtime tools, Session pinning, restart, and replay.
4. Steward static/default behavior and model callback.

Each slice compiles and passes its owning tests independently. Do not combine
future standalone distribution work with these reviews.

### Exit criteria

- the durable Golden Path passes through step 7;
- a fresh Caelis Store needs no Memory configuration;
- a successful Host always assembles Memory tools for an admitted Runtime;
- restart retains acknowledged facts and causal cursors;
- Session Replay repeats no Memory effect;
- default static mode makes zero Steward model calls;
- removal of sidecar composition leaves no runtime resolver, manifest pin,
  endpoint, downloader, supervisor, readiness diagnostic, or artifact setting.

## P2: v0.5.0 GA Closure

### Goal

Publish the current flat embedded package as a precise, supportable `v0.5.0`
baseline. This milestone adds no Session import, ranking policy, briefing, tree,
or other new product mechanism.

### Independently reviewable slices

1. **Compatibility floor.** Freeze the current schema and public API, document
   upgrade from the final release candidate, and reject unsupported older
   pre-release data explicitly.
2. **Pre-GA cleanup.** Remove the deprecated Generator surface and any stale
   product claim or integration seam that would become permanent at GA.
3. **Exact-revision validation.** Pass package conformance, full race tests,
   fixed corpus, GA soak, documentation links, diff checks, and downstream
   Caelis tests against the exact candidate commit.
4. **Platform and product acceptance.** Complete the supported native matrix,
   Rocky Linux offline path, clean-install Golden Path, and external review.
5. **Formal release.** Advance `VERSION`, verify remote quality for the exact
   commit, create an annotated `v0.5.0` tag, and publish the source release.

### Exit criteria

- acknowledged receipt loss, duplicate retry effects, Replay Memory calls, and
  unauthorized candidate access are all zero;
- Recall provenance coverage is 100%, and read-your-writes and restart tests
  pass;
- the schema/API compatibility floor and RC-to-GA upgrade are frozen;
- no deprecated pre-GA Generator contract remains public;
- Memory and the exact Caelis consumer revision pass their required
  architecture, test, race, build, documentation, offline, and platform gates;
- the fixed 100-Space, 100,000-receipt, 10,000-Record soak report is retained;
- an external reviewer maps every finding to code, test, acceptance ID, or an
  explicitly accepted risk;
- remote quality succeeds for the exact release commit before the annotated
  tag and public release are created.

The generic layered tree begins after this milestone and never retroactively
blocks `v0.5.0`.

## Cross-cutting: Optional Steward Quality

The existing provider-neutral Steward remains an optional enhancement to the
flat baseline and a possible execution mechanism for future tree summaries.
Memory continues to own bounded prompts, parsing, validation, leases, retries,
and atomic proposal application; the Host continues to own provider, model,
credentials, billing, and scheduling.

Every accepted receipt remains reachable without a model. Malformed,
cross-Space, cross-LabelSet, stale-revision, or unsupported proposals mutate
nothing. The existing 64-case study and any later 200-case reviewed study are
useful evidence for prompt quality, but neither they nor dictionary growth
constitute tree acceptance. A model-backed quality claim must retain its own
frozen corpus, cost, latency, abstention, and regression evidence.

## P3: Public Tree Model and Leaf Protocol

### Goal

Publish a source-neutral tree data model that can durably accept immutable leaf
revisions without understanding the producer's storage or domain.

### Independently reviewable slices

1. **Types.** Add `api/memory/tree/v1alpha1` types for Tree, Leaf, Item, NodeRef,
   immutable Revision, opaque SourceRef/SourceVersion/ProjectionRef, state,
   content digest, and budgets.
2. **Data plane.** Add `CommitLeaf`, `RetractLeaf`, and `GetNode` behind existing
   capability authority. The request selects no Space, LabelSet, slot, provider,
   or model; those remain Host-bound or Memory-assigned.
3. **Revision semantics.** Freeze canonical request hashing, idempotency,
   expected-head conflict, stable Memory-assigned slot, immutable history, and
   retraction behavior.
4. **Storage and migration.** Add an additive migration from the v0.5 schema
   floor and persist leaf heads, immutable revisions, ordered items, and the
   rebuildable deterministic leaf index in separate tree tables. With no Tree,
   the current Remember/Recall storage and behavior remain byte-for-byte
   unaffected.
5. **Conformance and producer guide.** Test at least two unrelated synthetic
   producers and document the Caelis mapping as an example only: one concrete
   Session JSONL projection becomes one leaf before crossing the Memory API.
6. **Owner lifecycle.** Add tree creation and leaf/tree deletion to the
   management protocol. Retraction preserves immutable history; deletion keeps
   only content-free audit and idempotency evidence and purges transitive
   derived content.

### Exit criteria

- the protocol contains no Session, JSONL, event, path, checkpoint, sanitizer,
  provider, or model semantics;
- identical requests return one leaf revision, while changed digests under the
  same idempotency key or stale expected heads fail closed;
- every leaf revision retains exact ordered item and opaque source provenance;
- authorization occurs before node reads or writes, and no node crosses an
  exact `(SpaceID, LabelSetDigest)` partition;
- restart, backup, restore, retraction, index loss, and rebuild preserve leaf
  history and active-head semantics;
- owner deletion makes leaf text and every transitively derived summary/index
  payload unrecoverable through Memory while retaining content-free tombstones;
- the Caelis example proves a downstream projector can represent one concrete
  Session JSONL as one leaf without Memory importing or interpreting it.

## P4: Layered Rollup Tree

### Goal

Roll immutable leaf revisions into a bounded hierarchy whose parents summarize
and index only their exact child revisions.

### Independently reviewable slices

1. **Rollup schema.** Add separate parent revision, child edge, build job, dirty
   queue, and partition root tables. Do not reinterpret current semantic
   Records as tree nodes or change their public wire semantics.
2. **Deterministic topology.** Use fan-out 16, stable Memory-assigned slot order,
   non-reused tombstone slots, complete child groups, and one frontier group per
   level. Models never choose topology.
3. **Deterministic index.** Build bounded term-to-child references without a
   model and make every entry attributable to its child revision.
4. **Public summary/refinement protocol.** Add `RequestRefinement` plus
   `api/memory/tree/worker/v1alpha1` claim, apply, and fail operations. An
   optional summarizer receives only a frozen child set and optional base
   summary, then returns bounded summary blocks, index terms, and citations.
   Memory verifies the child digest and applies at most one effect for a build
   input.
5. **Invalidation and rebuild.** Child changes remove stale ancestors from
   retrieval immediately, enqueue only the affected path, and reconstruct the
   hierarchy bottom-up after restart or full index loss.

### Exit criteria

- identical authorized leaves and policy produce identical topology, child-set
  digests, indexes, and roots across rebuilds;
- every parent edge is immutable, same-partition, and names an exact child
  revision;
- every summary assertion and index entry retains bounded child provenance;
- stale or degraded parents are excluded from retrieval until their exact
  replacement revision is ready;
- crash and retry at every build boundary have at most one accepted effect;
- no model binding is required for leaf commit, topology, indexing,
  invalidation, or rebuild; summary cost is reported per materialized revision.

## P5: Tree Retrieval and Identity Views

### Goal

Complete the public tree protocol with an explicit bounded query path only
after it demonstrates material benefit over scanning all authorized leaves.
Do not inject an automatic task briefing.

### Entry criteria

- P3 and P4 correctness, isolation, rebuild, and cost gates are accepted;
- a frozen longitudinal corpus represents real multi-Session tasks;
- a concrete downstream consumer defines its query and privacy needs.

### Independently reviewable slices

1. **Traversal engine.** Authorize the exact partition first, then bound
   depth, visited nodes, candidate children, returned leaf Items, and bytes.
2. **Comparative evaluation.** Compare deterministic tree traversal and the
   optional summary-assisted variant against an authorized all-leaf scan on
   recall, abstention, harmful context, latency, storage, rebuild, and cost.
3. **Public query contract.** Add `Query` to `memory.tree.v1alpha1` only after
   the traversal gate passes. Freeze result kinds, provenance, budgets,
   degraded state, pagination, compatibility, and empty-result semantics
   without extending `memory.v1alpha1` implicitly.
4. **Identity views.** A downstream product may map its identity and work
   context to opaque LabelSets and bounded views. Memory does not acquire Bot,
   workspace, relationship, or social product types.
5. **Lifecycle audit.** Verify producer revision/retraction, owner deletion,
   export, and reauthorization across leaf Items, parent summaries, and roots.

### Exit criteria

- tree traversal materially beats the bounded all-leaf control without privacy,
  abstention, harmful-context, latency, storage, or rebuild regression;
- every returned leaf Item or derived summary carries complete source and
  node provenance;
- empty, partial, degraded, and rebuilding trees have explicit behavior and do
  not disrupt current flat Recall;
- identity views remain downstream composition over exact LabelSets rather
  than a second Memory authority.

## Deferred standalone distribution

The retained `memoryd`, `memoryctl`, local transport, manifests, and packaging
tests preserve a future path for non-Go hosts, other Agents, and a publishable
Plugin. Work may resume only after a concrete external consumer or an observed
need for process isolation justifies it.

That future milestone must independently define supported platforms, artifact
sources, installation ownership, update and rollback, compatibility policy,
native endpoint security, and public acceptance. It must not re-enter the
Caelis embedded critical path or make Caelis depend on a downloaded Memory
binary.
