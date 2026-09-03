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

`v0.5.0` closes the current embedded package: immutable receipt evidence,
authorization-first Recall, exact LabelSet partitions, flat semantic Records,
and the optional Steward boundary. It does not include a global Session-corpus
importer, time-aware ranking policy, or automatic task briefing.

The next product line is a generic layered memory tree with its own versioned
public data model and protocol. A downstream producer commits an immutable leaf
revision containing ordered projected items, opaque source/version references,
and a content digest. Memory does not read, parse, sanitize, page, checkpoint,
or otherwise understand the producer's source. In the first Caelis integration,
one producer-defined leaf corresponds to the projection of one concrete Session
JSONL; that mapping remains entirely outside Memory.

Each parent contains an ordered set of child revision references plus a bounded
summary and index derived only from those children. Parent topology is
deterministic and partition-local. A summarizer may propose text and index terms
through a bounded public Worker protocol. Explicit refinement may append a new
summary revision for the same child set, but neither operation can choose
children, move a node across Space or LabelSet boundaries, or authorize
persistence. Memory
validates and versions every accepted projection. Replacing or retracting a
leaf dirties exactly one ancestor chain, which can be rebuilt from committed
leaf and child revisions.

Tree construction and traversal are post-`v0.5.0` milestones. They use a new
source-neutral protocol namespace rather than silently extending
`memory.v1alpha1` or `memory.steward.v1alpha1`. The current `remember(text)` and
`recall(query)` contracts remain the stable baseline. See the
[roadmap](docs/memory-appliance-roadmap.md) for the staged data model,
algorithms, and acceptance boundary.

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
