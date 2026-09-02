# Memory Go Package Roadmap

Status: authoritative implementation and GA plan.

The primary product is the Go package `github.com/caelis-labs/memory`. Caelis
imports it and runs one Memory runtime as part of the Caelis Host. The package,
not Caelis Control, owns Memory schema, data, authorization, retrieval,
classification, Steward work, lifecycle, and forgetting.

The repository retains `cmd/memoryd`, local transport, `memoryctl`, and packaging
code as a future standalone distribution framework. Building, publishing,
installing, supervising, or version-matching those binaries is not part of the
current Caelis integration or GA critical path.

## Product boundary

The irreversible boundary is source-level, not process-level:

```text
Caelis Host
  -> appliance.Open(data directory)
  -> sdk Client(DataPlane)
  -> remember(text) / recall(query)
  -> optional Steward Generator callback

Memory package
  -> SQLite, migrations, receipts, indexes, topology, authorization
  -> static zero-token retrieval
  -> durable Steward jobs and deterministic proposal application
```

Caelis may import public `api`, `sdk`, and `appliance` packages. It never imports
`internal/*`, opens `memory.db`, mirrors Memory state, or selects Memory
classification and lifecycle policy. Memory never imports Caelis product types
or owns a model provider, model credential, or provider configuration.

Successful Caelis Host construction means the embedded Memory database is open
and migrated. There is no independent download, install, probe, handshake,
readiness, degraded-start, or dynamic tool-injection state. A Memory open or
migration error is an ordinary Host startup error. Host shutdown closes Memory
after Runtime work drains.

## Golden Path

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

Future product concepts may select another opaque `BindingRef`. Bot, user,
tenant, workspace, and product identity do not enter the Memory API.

## SDLC and milestone map

```text
P0 Package Boundary
  -> P1 Embedded Caelis Feature
    -> P2 Steward and Realistic Evaluation
      -> P3 GA Candidate and External Review

Standalone Distribution (deferred, independent of P0-P3)
```

| SDLC stage | Milestone | Independently reviewable result |
| --- | --- | --- |
| Architecture | P0 | Public embedded facade over the existing durable authority |
| Construction and integration | P1 | Default Caelis Remember/Recall path works without a sidecar |
| Product verification | P2 | Static and model-backed multi-round behavior is measured |
| Release acceptance | P3 | Caelis release matrix and external review accept the feature |

## Implementation status — 2026-09-02

| Milestone | State | Remaining independent review slice |
| --- | --- | --- |
| P0 | Implemented locally | Complete package gates, publish an exact source revision, then review the public facade and SDK conformance diff |
| P1 | Implemented locally | Complete Caelis aggregate, race, architecture, product-regression, and platform gates against the exact Memory revision |
| P2 | In progress | Chinese lexical baseline, private lexicon growth, and a replicated 64-case low-cost Steward alias/category study are measured; expand the reviewed semantic/contradiction corpus to at least 200 cases |
| P3 | Pending | Produce the exact-revision candidate bundle, complete the release matrix, run Rocky native acceptance, and stop for external review |

The first privacy-preserving multi-round evidence uses both the local Codex
Memory registry and canonical Caelis Session JSONL without retaining source
text. See [Local Memory Registry Corpus Evidence](evidence/memory-registry-corpus-2026-09-02.md).
The first real-provider Steward parameter study uses a frozen non-literal query
fixture and records both accepted and rejected policy variants. See
[Memory Steward Evaluation](evidence/memory-steward-evaluation-2026-09-02.md).

## P0: Package Boundary

### Goal

Make the existing durable engine a first-class embeddable Go package without
exporting storage implementation details.

### Deliverables

- public `appliance.Open(context.Context, Options)`;
- a narrow Runtime lifecycle with `DataPlane`, owner Management, Steward Worker,
  capability issuance, and `Close` boundaries;
- direct SDK binding to `memory.v1alpha1.DataPlane`;
- one provider-neutral Steward Worker interface implemented by both embedded
  and local-transport clients;
- package-level Remember/Recall/restart tests using real SQLite;
- `cmd/memoryd` remains a thin optional composition over the same core.

### Explicit non-goals

- public concrete Store, SQL access, index access, or schema API;
- download, installation, updater, R2/GitHub acquisition, process supervisor,
  compatibility manifest, or platform artifact matrix;
- new cognitive taxonomy, vector/graph search, or model-provider integration.

### Exit criteria

- an external Go consumer imports only public packages and completes durable
  Remember/Recall;
- read-your-writes and restart persistence pass with no transport;
- embedded and local-transport paths share the same semantic conformance suite;
- `go test ./...`, race tests, formatting, and diff checks pass.

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
2. Caelis Host composition and automatic topology.
3. Runtime tools, Session pinning, restart, and replay.
4. Steward static/default behavior and model callback.

Each slice must compile and pass its owning tests independently. Do not combine
future standalone distribution work with these reviews.

### Exit criteria

- the Golden Path passes through step 7;
- a fresh Caelis Store needs no Memory configuration;
- a successful Host always assembles Memory tools for an admitted Runtime;
- restart retains acknowledged facts and causal cursors;
- Session Replay repeats no Memory effect;
- default static mode makes zero Steward model calls;
- removal of sidecar composition leaves no runtime resolver, manifest pin,
  endpoint, downloader, supervisor, readiness diagnostic, or artifact setting.

## P2: Steward and Realistic Evaluation

### Goal

Prove that Memory is useful across realistic multi-round facts before adding
new domain mechanisms.

### Deliverables

- explicit Steward model binding through Caelis' existing provider stack;
- unbound mode deterministically disables later semantic Jobs while retaining
  receipt Recall;
- bounded Memory-owned prompt policy and proposal validation;
- cleaned, privacy-reviewed corpora derived from local Memory Markdown and
  Caelis Session JSONL fixtures;
- fixed `gse` base dictionary plus two- and three-rune Han fallback projection,
  with the same analyzer used for write, Recall, correction, semantic records,
  migration, and rebuild;
- Space-private lexicon evidence, bounded static promotion, retirement, restart,
  and inspection counts; no term text crosses the management boundary;
- a reproducible parameter sweep that compares default, permissive, strict, and
  no-activation controls against the same legacy `unicode61` baseline;
- deterministic Chinese, English, and mixed-language cases covering repeated
  Remember/Recall, contradiction, supersession, unrelated noise, restart,
  private isolation, static fallback, and evidence provenance;
- model-assisted evaluation reports separated from deterministic correctness
  gates.

### Exit criteria

- every accepted receipt remains reachable in static mode;
- malformed, cross-Space, stale-revision, or unsupported proposals mutate
  nothing;
- binding a model affects only later jobs and never changes provider ownership;
- realistic corpus metrics and known limitations are frozen for the candidate;
- dictionary growth is never treated as retrieval-quality evidence; default
  activation remains conservative unless it beats the no-activation control;
- no quality claim depends solely on synthetic marker queries.

## P3: GA Candidate and External Review

### Goal

Accept the embedded feature as part of the Caelis product release.

### Hard gates

- acknowledged receipt loss: zero;
- idempotent retry duplicate effects: zero;
- unauthorized candidate access or private/shared leakage: zero;
- Replay Memory calls: zero;
- Recall fragment provenance coverage: 100%;
- read-your-writes and Host restart failures: zero;
- required Caelis tests, race tests, architecture checks, builds, docs links, and
  release dry-run pass at the exact candidate revision;
- Caelis builds and tests the imported Memory package on its supported Darwin,
  Linux, and Windows AMD64/ARM64 matrix;
- Linux native behavior is verified in the local OrbStack Rocky environment;
- an external reviewer maps every finding to code, test, acceptance ID, or an
  explicitly accepted risk before formal GA.

Memory has no separate runtime artifact gate in this milestone. The exact
Memory source version is compiled into the Caelis binary, so Caelis has one
release matrix, one installation, and one rollback unit.

### Product acceptance

A clean Caelis installation starts offline, requires no Memory download or
configuration, exposes the two tools, passes the Golden Path, and can enable or
disable model-backed Steward behavior solely by changing its system-agent model
binding.

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
