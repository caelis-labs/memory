# Memory

Memory is a Go package that gives Agent hosts a durable memory system with a
deliberately small model-facing surface:

```text
remember(text)
recall(query)
```

The package owns identity continuity, Spaces, Views, durable receipts,
authorization, retrieval, and every derived-memory mutation. The current
package baseline is a durable model-free lexical path plus an optional
provider-neutral Steward path. Classification, consolidation, lifecycle, and
forgetting are product directions, not claims that the first release already
implements them.

Caelis imports Memory and runs it as part of the Caelis Host. There is no
separate Memory download, installation, process, endpoint, readiness state, or
user configuration. A Host that starts successfully has already opened and
initialized its Memory database.

## Packages

- `appliance` exposes the narrow embedded lifecycle and direct data,
  management, capability, and Steward planes;
- `api/memory/*` owns versioned public contracts;
- `sdk/go/memory` binds hidden host context to Remember and Recall;
- `internal/appliance` owns the SQLite schema baseline, authorization, retrieval,
  governance, and semantic application;
- `conformance` owns reusable semantic and durable behavior suites.

Hosts must not open `memory.db`, import `internal/*`, mirror Memory state, or
let model arguments choose identity, Space, View, audience, retrieval policy,
or lifecycle.

## Current capabilities

- immutable durable receipts with separate processing state;
- Realm, Identity, private/shared Space, View, Grant, issuer, capability, and
  idempotency state in SQLite;
- side-effect-free validation of one exact issuer, Grant, View, actor, audience,
  and operation delegation before a Host reports ready;
- exact capability-bound LabelSet partitions for downstream workspaces or
  future identities, without exposing labels to model tools;
- authorization-first per-Space receipt and semantic FTS;
- immediate read-your-writes and restart durability;
- receipt search, provenance, correction, deletion, backup/restore, and
  diagnostics management foundations;
- durable Steward jobs with typed `ADD`, `MERGE`, `SUPERSEDE`, and `IGNORE`
  proposals;
- evidence and revision validation owned by Memory;
- a provider-neutral Steward `ModelGenerator` boundary plus Memory-owned prompt
  rendering and strict proposal parsing;
- static receipt/lexical Recall that consumes zero model tokens when no Steward
  model is bound.

Adaptive local lexicon learning is retained only as an internal experiment.
The public embedded runtime does not enable it, learn terms, consult learned
terms during Recall, expose its candidates to Steward, or rebuild indexes for
it. It may re-enter the product path only after a frozen-corpus A/B result shows
a material quality gain over the fixed analyzer.

## Product direction

The product target is a private persistent corpus, not merely two keyword
tools. Caelis will project sanitized canonical Session content into Memory,
Memory will rank evidence with explicit time authority, and an ordinary
stateless Session will receive a short task-relevant briefing assembled without
model calls. That briefing is evidence for context only: it is not an
instruction, authorization, or durable identity.

One immutable evidence authority serves three deliberately different
projections: explicit Recall, the stricter stateless briefing, and a future
bounded identity capsule. Session observations do not silently become explicit
user preferences, and old historical evidence is not presented as current just
because a keyword matches. The current derived-memory baseline is deliberately
flat: a Record has one active head, immutable numbered Revisions, and receipt
Evidence. `ADD` promotes evidence into a Record; `MERGE` and `SUPERSEDE` refine
it. Downstream products decide when to request that work. Embeddings, trees,
graphs, and hierarchies remain evaluation-gated experiments rather than public
API concepts.

A later stateful identity profile may maintain personality, relationships, and
working style through a bounded capsule assembled from the same labeled flat
records. Background ingestion, indexing, bounded time ranking, and deterministic
organization are algorithm-first; no idle model billing is acceptable. See the
[roadmap](docs/memory-appliance-roadmap.md) for the staged product and acceptance
boundary.

## Embedded use

```go
runtime, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
if err != nil {
    return err
}
defer runtime.Close()

client := memory.NewClient(runtime.DataPlane(), capabilities, source, budget)
```

The embedding remains responsible for product configuration, model selection,
tool projection, Session ToolResult persistence, replay, and selecting an opaque
LabelSet when it issues a Runtime capability. The model-facing Remember and
Recall requests do not contain labels. Memory remains responsible for LabelSet
validation, exact partition enforcement, every memory-domain mutation, and
authorization decisions.

## Standalone framework

`cmd/memoryd`, `cmd/memoryctl`, local transport, and packaging code are retained
as a future standalone-distribution framework for non-Go hosts and ecosystem
Plugins. They are buildable but are not the current product, are not published
for Caelis, and are not part of the current GA critical path.

Read the [specification](docs/memory-appliance-spec.md),
[roadmap](docs/memory-appliance-roadmap.md),
[acceptance plan](docs/memory-appliance-acceptance.md), and
[evaluation procedure](docs/memory-appliance-evaluation.md) before extending
the public API.

Run the package gates with:

```sh
make check
make race
make durable
```

The repository gates set `GOWORK=off` so the module remains independently
buildable even when cloned beside a parent development workspace.
