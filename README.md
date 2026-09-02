# Memory

Memory is a Go package that gives Agent hosts a durable memory system with a
deliberately small model-facing surface:

```text
remember(text)
recall(query)
```

The package owns identity continuity, Spaces, Views, durable receipts,
retrieval, classification, consolidation, lifecycle, and forgetting. A host
opens the package, binds those two operations, and may inject a model-backed
Steward callback through its existing provider stack.

Caelis imports Memory and runs it as part of the Caelis Host. There is no
separate Memory download, installation, process, endpoint, readiness state, or
user configuration. A Host that starts successfully has already opened and
migrated its Memory database.

## Packages

- `appliance` exposes the narrow embedded lifecycle and direct data,
  management, capability, and Steward planes;
- `api/memory/*` owns versioned public contracts;
- `sdk/go/memory` binds hidden host context to Remember and Recall;
- `internal/appliance` owns SQLite, migrations, authorization, retrieval,
  governance, and semantic application;
- `conformance` owns reusable semantic and durable behavior suites.

Hosts must not open `memory.db`, import `internal/*`, mirror Memory state, or
let model arguments choose identity, Space, View, audience, retrieval policy,
or lifecycle.

## Current capabilities

- immutable durable receipts with separate processing state;
- Realm, Identity, private/shared Space, View, Grant, issuer, capability, and
  idempotency state in migrated SQLite;
- authorization-first per-Space receipt and semantic FTS;
- immediate read-your-writes and restart durability;
- receipt search, provenance, correction, deletion, backup/restore, and
  diagnostics management foundations;
- durable Steward jobs with typed `ADD`, `MERGE`, `SUPERSEDE`, and `IGNORE`
  proposals;
- evidence and revision validation owned by Memory;
- a provider-neutral Steward `Generator` boundary;
- static receipt/lexical Recall that consumes zero model tokens when no Steward
  model is bound.

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
tool projection, Session ToolResult persistence, and replay. Memory remains
responsible for every memory-domain mutation and authorization decision.

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
