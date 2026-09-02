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

## Current delivery slice: LabelSet and flat organization foundation

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
- Session corpus import, automatic briefing, retention, decay, and forgetting;
- user-visible label configuration or Agent tool arguments;
- a memory tree, general relation graph, embedding, or hierarchical traversal;
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
  -> time-aware ranking and bounded task briefing (planned foundation)
  -> durable Steward jobs and deterministic proposal application
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

The product is a private persistent corpus, not merely two keyword tools. It
has two consumption profiles that share evidence, authorization, storage, and
retrieval without becoming two Memory systems.

### Stateless Session profile

An ordinary Session has no durable personality or autonomous identity. Before
work begins, the Host may supply bounded task text and receive a short
task-relevant briefing containing such evidence as similar prior tasks, stable
user preferences, and previously accepted technical decisions. The briefing:

- is length-bounded and evidence-backed;
- is advisory context, never an instruction, authority, permission, or
  decision;
- does not give the Session a persistent identity;
- is assembled by deterministic retrieval and ranking in the default path;
- is an assembly-time context input, not a third model-visible Memory tool.

The public briefing API is deliberately not frozen until corpus projection,
ranking inputs, and response budgets have executable acceptance tests.

### Stateful identity profile

A future Bot-like product may map its opaque identity and workspace concepts to
one or more exact LabelSets while retaining a stable `MemoryIdentity` for hard
continuity. Memory does not gain a Bot type. The canonical derived structure
remains flat Records with immutable Revisions and evidence links; any personality,
relationship, or work-style hierarchy is a downstream projection until measured
evidence justifies promoting another structure.

### Corpus ingestion

All eligible local Caelis Session content is a product corpus source, but the
Memory database is not the canonical Session log. Production ingestion reads a
checkpointed canonical Session projection through the Session Service API and
submits stable, idempotent Memory inputs. It never scans physical `*.jsonl`
files or depends on their layout.

The projection retains durable user and assistant facts with source provenance.
It excludes system and developer prompts, hidden reasoning, credentials,
approval payloads, transient progress, and raw tool input/output unless a
separately reviewed sanitizer converts them into safe durable facts. Import is
resumable, bounded, and model-free. A local JSONL reader may remain an offline
evaluation utility but is not a production ingestion path.

### Time and authority

Receipt occurrence, receipt arrival, later reinforcement, correction, and
supersession remain separate signals. No year-scale decay curve is a foundation
requirement. A later forgetting milestone starts with immediately testable,
bounded active windows per LabelSet; overflow may become dormant without
deleting evidence. Explicit Remember and evidence-backed promoted Records do not
expire merely because wall-clock time passed.

### Model-cost contract

Receipt ingestion, sanitization, indexing, time decay, candidate retrieval,
deduplication, and default briefing generation consume zero model tokens.
Starting or idling Caelis with no Memory work consumes zero model tokens. A
model-backed Steward runs only after an explicit model binding and only for new
eligible evidence or an explicit bounded organization action analogous to
`/dream`; it never wakes solely because wall-clock time passed.

Automatic model work has a frozen per-receipt call budget. A network or model
failure cannot create an unattended retry loop: any retry that spends another
model call waits for a later active task or an explicit organization action.

Memory owns the prompt, input budget, proposal parser, evidence checks, and
apply policy. The downstream Host supplies only its existing provider/model
callback and accounting. Algorithmic organization remains authoritative;
model output is optional untrusted advice.

### Product convergence rules

Memory keeps one evidence authority and derives bounded products from it. It
does not create separate authoritative stores for episodic, semantic, hot,
warm, or cold memory. The planned products are:

- explicit Recall, which searches evidence on demand and may answer an
  explicitly historical query;
- a stateless task briefing, which admits only current, sufficiently supported
  evidence and may be empty;
- a future identity capsule, which projects a small stable root for one
  stateful identity before any hierarchical expansion is justified.

Those products may use different ranking, temporal, and abstention policies.
There is no universal `relevance * recency * confidence` score: a recent
incidental mention must not outrank an explicit durable decision merely because
it is newer, while an old unsupported preference must not enter an automatic
briefing merely because one keyword matches. Correction and supersession are
state transitions, not negative score weights.

Explicit Remember and sanitized Session observation are also different forms
of evidence. A Session projection records what occurred; it does not silently
upgrade every conversation sentence into an accepted preference or decision.
Because `SourceContext` is untrusted audit metadata, any source class that
affects ranking or admission must come from a host-authenticated, model-hidden
ingestion boundary defined and tested in P2.

Skills, executable procedures, routines, credentials, files, and Session logs
retain their existing product owners. Memory may recall evidence about them but
does not become their registry, scheduler, or canonical store.

### Retrieval evolution ladder

Retrieval evolves behind the same narrow Remember/Recall surface:

1. the current fixed multilingual lexical analyzer is the durable control;
2. P2 may add bounded model-free phrase, association, temporal, contradiction,
   and abstention projections when each beats that control on a blind holdout;
3. an explicitly bound Steward may add evidence-backed semantic Records, but
   static retrieval and briefing remain complete without it;
4. embeddings, relation graphs, or hierarchical indexes remain experiments
   until they produce material end-to-end benefit over the best zero-token
   baseline without unacceptable privacy, latency, storage, rebuild, or
   dependency cost.

Every derived index is disposable and rebuildable from authorized evidence.
An experiment cannot add a user-facing model/provider setting, change the
Remember/Recall arguments, or become required for startup. Promotion requires
the same frozen corpus, a held-out set, parameter-trajectory evidence, and a
documented rollback to the previous accepted retriever.

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

### Product-foundation path

1. Import eligible canonical history from multiple local Sessions through one
   resumable cursor.
2. Start an unrelated new Session with a natural-language task, without asking
   the model to guess Recall keywords.
3. Assemble one short authorized briefing that cites similar tasks, relevant
   preferences, and current decisions.
4. Prefer recent supported evidence over stale keyword-only evidence while
   retaining explicit historical lookup.
5. Repeat the path offline with no Steward binding and observe zero model calls.

Future product concepts may select another opaque `BindingRef`. Bot, user,
tenant, workspace, and product identity do not enter the Memory API.

## SDLC and milestone map

```text
P0 Package Boundary
  -> P1 Embedded Caelis Feature
    -> P2 Product Foundation
      -> P3 Optional Steward Quality
        -> P4 GA Candidate and External Review
          -> P5 Stateful Identity Hierarchy

Standalone Distribution (deferred, independent of P0-P5)
```

| SDLC stage | Milestone | Independently reviewable result |
| --- | --- | --- |
| Architecture | P0 | Public embedded facade over the durable authority |
| Context partition | P0.1 | Capability-bound LabelSet and flat derived-memory structure |
| Construction and integration | P1 | Default Caelis Remember/Recall works without a sidecar |
| Product foundation | P2 | Sanitized Session corpus, time-aware ranking, and bounded task briefing make Memory useful without exact tool keywords |
| Optional enhancement | P3 | Memory-owned Steward policy adds measured value without becoming a cost or availability dependency |
| Release acceptance | P4 | Caelis release matrix and external review accept the feature |
| Future identity | P5 | Downstream identity composition uses LabelSets and measured projections without polluting the stateless path |

## Implementation status — 2026-09-02

| Milestone | State | Remaining independent review slice |
| --- | --- | --- |
| P0 | Complete at package scope | Public facade, SDK conformance, default-path lexicon retirement, close concurrency, and CI package checks are implemented; external review remains part of P4 |
| P0.1 | Complete in Memory | LabelSet baseline, exact data-plane and Steward partitioning, flat-structure contract, and the 224-case multilingual package gate are complete; Caelis workspace injection is the next slice |
| P1 | Technical integration complete | Default embedded tools, persistence, replay, and system-managed Steward binding exist; product usefulness is not yet accepted |
| P2 | Planned, now GA-critical | Implement canonical Session corpus projection, deterministic time-aware ranking, and a bounded model-free task briefing |
| P3 | In progress, non-blocking for static operation | Memory-owned prompt/parser and an initial 64-case low-cost study exist; Caelis adapter cutover and at least 200 reviewed cases remain |
| P4 | Not accepted | Product-foundation gates, native Windows acceptance, exact-revision cross-platform gates, and external review remain |
| P5 | Vision only | Define downstream identity composition only after P2/P3 establish evidence, pruning, and cost behavior |

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

## P2: Product Foundation

### Goal

Make Memory useful before an Agent guesses an exact Recall keyword, without
introducing a background token bill.

### Independently reviewable slices

1. **Canonical Session corpus projection.** Caelis reads checkpointed canonical
   Session events through the Session Service, applies a reviewed sanitizer,
   and submits stable idempotent inputs. Restart resumes from a durable cursor;
   no production code reads physical Session JSONL.
2. **Bounded active-memory admission.** Memory combines the fixed analyzer with
   deterministic correction, supersession, reinforcement, and per-LabelSet
   active-window rules. Evaluation varies small count-based bounds on real
   Session corpora; no multi-year curve blocks the first useful slice.
3. **Task briefing.** A package-owned bounded request/response produces a short
   advisory context for a new stateless Session from task text and authorized
   evidence. It has provenance and a byte budget, carries no authority, and
   makes no model call.
4. **Corpus evaluation.** Frozen Chinese, English, and mixed-language tuning
   and blind holdout cases cover similar historical tasks, durable preferences,
   changed decisions, older facts, contradictory evidence, sparse/non-literal
   task wording, correct abstention, and harmful irrelevant context. Every
   report compares the empty-context, exact-lexical, best model-free, and any
   model-assisted variants and preserves the parameter trajectory rather than
   only the winning run.

Each slice is independently committable and reviewable. Do not freeze the
briefing public API before slices 1 and 2 establish the real inputs and ranking
contract.

### Exit criteria

- every eligible local Session is projected or has a recorded, non-sensitive
  exclusion reason;
- explicit Remember and observational Session evidence remain distinguishable
  through a trusted, model-hidden source boundary; `SourceContext` labels do
  not establish authority;
- re-import and crash recovery create no duplicate facts;
- excluded content and one identity's private evidence never appear in another
  authorized result;
- bounded active-window ranking meets its frozen corpus thresholds and reports
  all tested count parameters;
- the frozen non-literal set improves over exact lexical retrieval without an
  embedding model or unacceptable false-positive growth;
- a historical Recall result is visibly qualified, while an unqualified stale
  or unresolved candidate is omitted from an automatic briefing;
- the briefing fits its byte budget, cites its evidence, contains no authority
  or imperative policy, and improves a frozen task-context benchmark over an
  empty-context control without increasing harmful-context or false-memory
  errors;
- all four slices consume zero model tokens.

## P3: Optional Steward Quality

### Goal

Prove that an explicitly bound low-cost model materially improves organization
or non-literal retrieval without weakening the static path.

### Deliverables

- explicit Steward model binding through Caelis' existing provider stack;
- unbound mode deterministically disables later semantic Jobs while retaining
  receipt Recall;
- bounded Memory-owned prompt rendering, input construction, proposal parsing,
  and proposal validation; native JSON Schema may optimize a provider but is
  never required for correctness;
- cleaned, privacy-reviewed evaluation corpora derived from local Memory
  Markdown and canonical Session projections;
- fixed `gse` base dictionary plus two- and three-rune Han fallback projection,
  with the same analyzer used for write, Recall, correction, semantic records,
  schema initialization, and rebuild;
- adaptive lexicon learning retained only behind an internal evaluation option;
  default Open, schema initialization, Remember, Recall, correction, deletion, and Steward
  paths neither learn nor consume local terms;
- deterministic Chinese, English, and mixed-language cases covering repeated
  Remember/Recall, contradiction, supersession, unrelated noise, restart,
  private isolation, static fallback, and evidence provenance;
- model-assisted reports separated from deterministic correctness gates;
- a frozen automatic per-receipt model-call budget and demand-gated retry
  policy; polling an empty queue or waiting for time to pass never spends a
  token.

### Exit criteria

- every accepted receipt remains reachable in static mode;
- malformed, cross-Space, cross-LabelSet, stale-revision, or unsupported
  proposals mutate nothing;
- binding a model affects only later jobs and never changes provider ownership;
- realistic corpus metrics and known limitations are frozen for the candidate;
- dictionary growth is never treated as retrieval-quality evidence; adaptive
  lexicon remains experimental and disabled unless it materially beats the
  fixed analyzer on a frozen corpus without regression;
- no quality claim depends solely on synthetic marker queries.

P3 may improve P2 quality but cannot become an availability dependency or a
substitute for P2's zero-token briefing path.

## P4: GA Candidate and External Review

### Goal

Accept the embedded feature as part of the Caelis product release.

### Hard gates

- acknowledged receipt loss: zero;
- idempotent retry duplicate effects: zero;
- unauthorized candidate access or private/shared leakage: zero;
- Replay Memory calls: zero;
- Recall fragment provenance coverage: 100%;
- read-your-writes and Host restart failures: zero;
- canonical Session corpus projection is resumable, idempotent, sanitized, and
  model-free;
- documented bounded active-window and reinforcement behavior passes the frozen
  corpus;
- bounded task briefing beats the empty-context control without authority or
  privacy violations;
- an idle Host and an unbound Steward produce zero Memory model calls;
- required Caelis tests, race tests, architecture checks, builds, docs links,
  and release dry-run pass at the exact candidate revision;
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
configuration, exposes the two tools, passes both Golden Paths, and can enable
or disable model-backed Steward behavior solely by changing its system-agent
model binding.

## P5: Stateful Identity Composition

### Goal

Support a product with persistent identity, personality, relationships, and
work style through LabelSets and a bounded capsule without changing the
stateless Session contract or replacing the flat Memory authority.

### Entry criteria

- P2 evidence and time semantics are accepted;
- a concrete downstream identity consumer defines lifecycle and privacy needs;
- the flat Record-head-plus-search control has accepted longitudinal evidence;
- capsule admission, refinement, provenance, and recovery policies have
  deterministic tests and a zero-token default implementation.

### Independently reviewable slices

1. **Identity mapping.** The downstream product maps its own identity and work
   context to opaque LabelSets. Memory does not acquire Bot, workspace, or
   social-relationship product types.
2. **Identity capsule.** Start with fixed, byte-bounded identity, personality,
   relationship, and work-style blocks backed by active evidence. The capsule
   is assembled at Session start, is inspectable, and rejects overflow rather
   than silently truncating identity.
3. **Optional structure experiment.** Compare the flat capsule plus evidence
   search against
   bounded branches on a longitudinal identity corpus. Introduce parent/child
   structure only if it improves retrieval or update quality after accounting
   for merge and recovery complexity.
4. **Organization action.** A model may propose a bounded consolidation during
   an explicit organization action. Memory validates an inspectable diff;
   destructive or lossy projection changes require the product's confirmation
   policy, and immutable receipt evidence remains recoverable.
5. **Bounded lifecycle and rebuild.** Active-record and capsule budgets affect
   derived state only. Dormancy or projection pruning never serves as receipt
   deletion, and the flat record set and capsule rebuild without a model.

Model-assisted consolidation may be an explicit bounded organization action.
It cannot run continuously, erase receipt evidence, or become the only way to
rebuild identity context.

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
