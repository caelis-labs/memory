# Memory Go Package Roadmap

Status: authoritative implementation and GA plan.

The primary product is the Go package `github.com/caelis-labs/memory`. Caelis
imports it and runs one Memory runtime as part of the Caelis Host. Memory owns
its schema, authorization, durable effects, retrieval, and validation of every
derived-memory mutation. It remains independently runnable and contains no
Caelis product type.

`cmd/memoryd`, local transport, `memoryctl`, and packaging code remain
buildable scaffolding for a possible standalone distribution. They are not in
the current integration or release critical path.

## Product boundary

Memory is a governed evidence and semantic-memory kernel. It supplies public
data structures and protocols for durable facts, corpus items, derived
artifacts, and bounded queries. It does not know how a producer stores or
interprets its source material.

Two independent public models are planned:

```text
Fact Memory ledger (v0.5.0)
  Receipt -> optional semantic Record / Revision -> Remember / Recall

Corpus Memory ledger (post-v0.5.0)
  Corpus -> Leaf -> immutable LeafRevision -> ordered Items -> QueryCorpus

Projection substrate (post-v0.5.0, optional)
  direct Item lexical index
  rollup hierarchy
  summary artifacts
  dense index
  optional graph
```

The two ledgers may share authorization, partition, receipt-like idempotency,
provenance, lifecycle, and migration machinery. They do not reinterpret a
Receipt or semantic Record as a corpus node, and neither depends on the other
for correctness.

The former plan for a global canonical Session-corpus projection, time-aware
ranking, and an automatically injected zero-model task briefing is retired.
Memory does not read Session logs or define Session semantics. A downstream
producer may choose to project one concrete Session JSONL into one Leaf, but
that is only one integration mapping among many; source parsing, admission,
sanitization, checkpointing, and versioning stay outside Memory.

## v0.5.0 GA baseline

`v0.5.0` closes the Fact Memory ledger:

- immutable receipts with durable idempotency and consistency tokens;
- authorization before per-Space candidate generation;
- exact capability-bound LabelSet partitions;
- model-free lexical Remember/Recall;
- flat semantic Records with immutable Revisions and receipt provenance;
- an optional provider-neutral `ModelGenerator` Steward boundary;
- correction, deletion, inspection, export, backup, restore, and diagnostics;
- the first published database compatibility floor, including an exact
  metadata-only upgrade from the final prerelease baseline.

This is a useful standalone product model, not a claim of complete cognitive
memory. Corpus memory and every projection listed below are post-GA additions
and do not block `v0.5.0`.

## Authority and rebuild matrix

| Object | Authority | Mutable role | Rebuild rule |
| --- | --- | --- | --- |
| external producer source | producer-owned | outside Memory | Memory never opens or reconstructs it |
| Receipt | Fact Memory evidence | immutable payload | not rebuilt from a projection |
| semantic Record head | current accepted interpretation | advances to immutable Revisions | rebuilt/validated from accepted revisions and evidence |
| LeafRevision and Item | Corpus Memory evidence accepted from a producer | append-only revisions and lifecycle state | durable base for corpus projections |
| RollupManifest | immutable structural projection input | replaced by another manifest | rebuilt from active LeafRevisions and topology policy |
| SummaryArtifactRevision | derived proposal accepted for one manifest | append-only refinement | discarded or regenerated; never evidence authority |
| IndexArtifactRevision | deterministic or optional derived index | generation changes | discarded and rebuilt from exact referenced inputs |
| ProjectionSnapshot | active publication pointer and coverage watermark | atomically advanced | republished from a complete projection generation |

A query may use derived artifacts to find evidence, but returned facts remain
attributable to exact Receipt or Leaf Item evidence. Disabling or rebuilding a
projection cannot make the flat Remember/Recall path unavailable.

## Corpus Memory public model

The first Corpus protocol is source-neutral and intentionally avoids a
universal `Node` abstraction:

```text
Corpus
  CorpusRef, partition binding, lifecycle state

Leaf
  LeafRef, active revision, lifecycle state

LeafRevision
  LeafRef, Revision, opaque SourceRef / SourceVersion / ProjectionRef
  ordered Items, canonical ContentDigest, CommitSequence

Item
  ItemRef, bounded text or typed content, ContentDigest, ordinal
```

The producer owns source interpretation and sends already-admitted Items.
Memory validates shape, byte budgets, canonical digests, idempotency, expected
head, lifecycle, and partition authority. Replaying the same effect identity
and request digest returns the original result; a changed request under that
identity conflicts. A new accepted revision never rewrites prior evidence.

The working namespace and operation inventory for P3 prototyping is:

```text
memory.corpus.v1alpha1
  CreateCorpus / CommitLeafRevision
  GetLeaf / GetLeafRevision
  RetractLeaf / RedactLeafItems / EraseLeaf
  QueryCorpus / GetCommitStatus / GetErasureStatus

memory.materializer.v1alpha1
  ClaimMaterialization / ApplyDeterministicArtifact
  ApplySummaryProposal / FailMaterialization

memory.corpus.management.v1alpha1
  PutProjectionDefinition / InspectProjection / RebuildProjection
  ActivateProjectionGeneration / DeleteCorpus / ExportCorpus
  PurgeObsoleteGenerations
```

This inventory expresses required capabilities, not a frozen API. P3 narrows
and validates operation granularity, lifecycle vocabulary, consistency, and
status semantics before publishing any package.

Capability operations authorize each effect independently. Space, LabelSet,
actor, audience, View, Grant, lease, provider, model, and credentials remain
outside model-facing payloads. Exact artifact inspection belongs to management
and diagnostics; `GetNode` is not the primary Agent query abstraction.

## Projection substrate

Projection definitions are versioned policy, not evidence. The first required
projection for Corpus Memory is a direct Item lexical index. It supplies a
high-recall control and useful QueryCorpus path before any hierarchy exists.

A rollup hierarchy is one optional projection kind:

```text
ProjectionDefinition
  Kind = rollup_hierarchy
  topology, analyzer, summary, index, and query policy references

RollupManifest
  ManifestRef, Level, exact ordered ChildRefs, ChildSetDigest, TopologyPolicyRef

SummaryArtifactRevision
  ManifestRef, bounded SummaryBlocks, exact SupportRefs
  SummaryPolicyRef, model/prompt/profile provenance, artifact digest

IndexArtifactRevision
  ManifestRef, deterministic postings, optional expansion entries
  AnalyzerRef, IndexPolicyRef, artifact digest

ProjectionSnapshot
  ProjectionRef, Generation, root or forest refs, CoveredCommitSequence
  completeness and degraded state
```

Structural manifests, summaries, and indexes are separate artifacts so that a
model failure cannot prevent deterministic indexing or publish half of a new
generation. A summarizer receives only one fixed manifest and bounded child
representations. Its output is an untrusted proposal: it cannot select child
edges, partition, lifecycle, erasure, or publication.

Fan-out, clustering, balanced versus incremental topology, stable slots,
single-root versus forest shape, and root-first versus collapsed retrieval are
experimental policy choices. They are not frozen into the Corpus wire model or
the post-GA compatibility promise. Protocol, schema, topology, analyzer, index,
model, summary, and query policies version independently.

## Commit and materialization lifecycle

One Leaf commit is accepted in this order:

1. authenticate and authorize the exact partition before reading candidates;
2. validate capability, expected head, idempotency, canonical digest, and
   budgets;
3. append LeafRevision and Items and advance the Leaf head transactionally;
4. update the direct Item lexical index and commit sequence;
5. append an outbox/dirty effect for optional projections;
6. return success only after direct QueryCorpus can observe the commit under
   its consistency token.

The structural materializer consumes committed sequences, creates immutable
RollupManifests and deterministic IndexArtifactRevisions, and publishes only a
coherent ProjectionSnapshot. A summary worker may later attach a
SummaryArtifactRevision for the same manifest. Refinement creates another
summary artifact revision; it does not mutate the manifest.

A changed, retracted, redacted, or erased Leaf invalidates every dependent
artifact before it can be returned. Retraction preserves owner-auditable
history. Redaction and erasure remove authorized content and every transitively
derived payload while retaining only the minimum content-free tombstone and
idempotency evidence required to prevent resurrection.

All projectors are retry-safe. An accepted artifact is keyed by its exact input
digest and policy references. Crashes before snapshot activation leave the
previous complete generation active; crashes afterward replay to the same
effect.

## Query model

QueryCorpus authorizes exact partitions before accessing any index. The first
implementation uses the direct Item index. Hierarchical, dense, or graph
projections are independently feature-gated and may be absent, stale, or
disabled without affecting direct corpus query or Fact Memory Recall.

The hierarchy experiment starts with direct Item high-recall seeds, collapsed
cross-level artifact retrieval, and controlled expansion to Leaf Items. It does
not assume root-first descent. Every returned result revalidates the active
Leaf revision, lifecycle, Space, LabelSet, and evidence provenance before
disclosure. Derived summaries are clearly marked and link to exact support.

A response reports enough state to make projection behavior observable:

```text
projection generation and covered commit sequence
result source = direct | hierarchical | mixed
observed consistency
degraded and truncated flags
bounded provenance to LeafRevision and Item
```

Budgets independently cap candidate artifacts, expansion, returned Items,
bytes, and time. Empty, partial, and rebuilding projections are valid. No
ranking policy based on recency, decay, active windows, or universal importance
is part of the core contract.

## Golden paths

### Fact Memory (v0.5.0)

1. Open Memory in an empty Host store and issue a hidden exact capability.
2. Expose only `remember(text)` and `recall(query)` to the model.
3. Remember a fact, immediately Recall it, restart, and Recall it again.
4. Replay the original product Session without repeating a Memory effect.
5. With no Steward binding, keep the static path useful with zero model calls.
6. With a binding, accept only a validated semantic proposal with exact
   receipt provenance; remove the binding without affecting static Recall.

### Corpus Memory (post-v0.5.0)

1. An arbitrary producer commits an already-projected LeafRevision.
2. Memory validates authority and durable effect identity without interpreting
   the source references.
3. Direct QueryCorpus observes the committed Items under a consistency token.
4. Optional materializers build a new coherent projection generation from
   exact Leaf revisions.
5. Query may use that generation to find evidence and reports its coverage and
   provenance.
6. Retraction, redaction, erasure, crash, and full projection loss preserve the
   ledger contract and never leak stale derived content.

## SDLC and milestone map

```text
P0 Package Boundary
  -> P0.1 LabelSet and Flat Record Baseline
    -> P1 Embedded Caelis Feature
      -> P2 v0.5.0 GA Closure

P3 Corpus Ledger and Direct Query
  -> P4 Projection Substrate and Rollup Experiment
    -> P5 Comparative Retrieval and Optional Product Adoption

Optional Steward Quality (cross-cutting)
Standalone Distribution (deferred and independent)
```

| Milestone | Independently reviewable result |
| --- | --- |
| P0 | Public embedded facade over durable Memory authority |
| P0.1 | Exact LabelSet partitions and flat semantic Records |
| P1 | Caelis Remember/Recall works without a sidecar |
| P2 | `v0.5.0` publishes the Fact Memory ledger and schema floor |
| P3 | Source-neutral Leaf ledger, direct Item index, and QueryCorpus |
| P4 | Multi-projection substrate plus evaluation-gated rollup hierarchy |
| P5 | A measured query strategy and optional downstream adoption |

## P2: v0.5.0 GA closure

Exit criteria:

- remove every deprecated pre-GA `Generator` callback;
- freeze the public API and `memory-v0.5.0` schema baseline;
- preserve final prerelease data through the documented metadata promotion;
- pass package, race, durable, frozen corpus, and GA soak gates;
- validate the exact Caelis consumer against `ModelGenerator`;
- resolve or explicitly accept the external architecture review;
- push the exact candidate, wait for its remote quality workflow, then create
  the annotated `v0.5.0` tag and non-prerelease source release.

Corpus or projection implementation is not a P2 exit criterion.

## P3: Corpus ledger and direct query

Deliver `memory.corpus.v1alpha1`, additive migration from the v0.5 schema,
immutable Leaf revisions and ordered Items, lifecycle/status APIs, direct
per-partition Item lexical indexing, and bounded QueryCorpus. Prove two
unrelated producers through conformance. One example may map one concrete
Session JSONL projection to one Leaf, while the protocol remains free of
Session, JSONL, path, event, checkpoint, sanitizer, provider, and model fields.

Exit requires authority-before-candidate, exact idempotency/conflict behavior,
restart and rebuild safety, full Leaf/Item provenance, effective redaction and
erasure, and useful direct retrieval with every optional projection disabled.

## P4: projection substrate and rollup experiment

Deliver independently versioned ProjectionDefinition, RollupManifest,
SummaryArtifactRevision, IndexArtifactRevision, ProjectionSnapshot, durable
materialization work, invalidation, rebuild, and generation activation.
Evaluate at least one rollup topology without promoting its fan-out, grouping,
root shape, or traversal policy into the public Corpus contract.

Exit requires same-partition exact input references, reproducible deterministic
artifacts, atomic generation publication, bounded summary proposals, complete
support provenance, retry safety, transitive lifecycle invalidation, and clean
feature-flag rollback to P3 direct query.

## P5: comparative retrieval and optional adoption

Compare direct-only, collapsed cross-level, controlled-expansion, and any
summary-assisted variants on a frozen longitudinal corpus. Record recall,
abstention, harmful context, latency, storage, rebuild time, model cost,
staleness, and privacy failures. Graph or dense projections enter the same
comparison only when they have a concrete hypothesis.

Freeze a public query strategy or downstream default only when it materially
beats direct Item retrieval without unacceptable regression. A downstream
product may compose bounded identity views over exact authorized results;
Memory does not acquire Bot, user, workspace, relationship, or task-briefing
types.

## Deferred standalone distribution

Standalone publication requires a concrete external consumer and a separate
release decision. It must define supported platforms, artifact sources,
endpoint security, installation ownership, update/rollback, compatibility, and
public acceptance. It must remain optional for embedded consumers.
