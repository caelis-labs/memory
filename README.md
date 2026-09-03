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

`v0.5.0` is a governed evidence and semantic-memory kernel: immutable receipt
evidence, authorization-first Recall, exact LabelSet partitions, flat semantic
Records, and an optional Steward boundary. It is independently useful, but is
not presented as a complete cognitive-memory system. It does not include a
global Session-corpus importer, time-aware ranking policy, or automatic task
briefing.

Post-GA work separates two new layers. A source-neutral Corpus ledger accepts
immutable Leaf revisions and ordered Items from arbitrary downstream producers.
Memory never reads, parses, sanitizes, checkpoints, or otherwise understands a
producer's source. A concrete Session JSONL may be projected to one Leaf by a
Caelis integration, but that mapping stays entirely outside Memory.

An optional projection substrate builds direct lexical indexes, rollup
hierarchies, summary artifacts, dense indexes, or graphs over the Corpus
ledger. Rollup manifests, summaries, indexes, and active projection snapshots
are distinct objects: none replaces Leaf evidence. Topology and query choices
such as fan-out, stable slots, a single root, or root-first traversal remain
evaluation parameters instead of public compatibility commitments.

Corpus and projection protocols are post-`v0.5.0` milestones. They do not
silently extend `memory.v1alpha1` or `memory.steward.v1alpha1`, and disabling
them cannot affect current `remember(text)` / `recall(query)` behavior. See the
[roadmap](docs/memory-appliance-roadmap.md) for the staged authority model and
acceptance boundary.

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
