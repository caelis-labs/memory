# Memory Appliance Roadmap

Status: the original six-milestone line produced `v0.5.0-rc.1`; the GA Closure
line below is now authoritative for the remaining work. RC1 is a candidate for
`darwin/arm64`, not a GA release. Execution evidence belongs in issues, reviews,
commits, CI, and releases rather than edits to this document.

The roadmap favors early vertical evidence. It freezes hard boundaries first,
then produces a durable standalone service, then immediately exercises it
through Caelis before adding advanced cognition. GA Closure corrects the
Steward execution boundary, activates the already-published SDK integration,
and closes corpus, lifecycle, and six-platform release evidence without adding
new Memory domain features.

## Repository and delivery boundary

This repository, `github.com/caelis-labs/memory`, owns the service, versioned
API, Go SDK, conformance suite, migrations, storage, and packaging. Caelis owns
only Runtime binding, tool projection, replay, sidecar composition, and product
diagnostics.

Cross-repository work pins an exact API module version and exact `memoryd`
artifact digest. Caelis never imports `internal/*` from this repository or
accesses the database directly.

Every milestone is an independently reviewable slice. The roadmap does not
prescribe a commit count. A slice is complete only when its contract, code,
tests, failure behavior, diagnostics, and rollback/disable story are reviewable
together.

## Golden Path

This scenario remains the shared product narrative:

1. Create one Realm and one Shared Space.
2. Create Bot-A and Bot-B Identities and private Spaces.
3. Bot-A private Remember: `commit does not authorize push`.
4. Bot-A immediately Recalls it; Bot-B cannot see it.
5. A shared Runtime Remembers: `the project uses Go`.
6. Bot-A and Bot-B can Recall the shared fact.
7. Restart both `memoryd` and Caelis; the facts and isolation remain.
8. Stop `memoryd`; replaying the old Caelis Session remains byte-identical and
   performs zero Memory calls.

Each milestone records how much of this path runs and which new negative or
failure-injection case it adds.

## Dependency and SDLC stages

```text
M0 Contract Baseline
  -> M1 Standalone Durable Core
    -> M2 Caelis End-to-End Alpha
      -> M3 Governance and Production Safety
        -> M4 Semantic Steward Extension
          -> M5 Release Candidate and GA
```

| SDLC stage | Milestone | Exit result |
| --- | --- | --- |
| Requirements and design | M0 | Irreversible contracts compile and pass semantic conformance |
| Construction | M1 | Durable model-free `memoryd` works alone |
| Integration and Alpha | M2 | Real Caelis Remember/Recall/Replay path works |
| Hardening | M3 | Users can inspect, remove, recover, and operate data safely |
| Intelligent extension | M4 | Steward enhances but never gates baseline Recall |
| Verification and release | M5 | Exact artifacts pass system acceptance and rollback |

## M0: Contract Baseline

### Goal

Freeze only external ownership, security, durability, idempotency, consistency,
failure, and replay semantics. Prove the contract with executable tests before
choosing production storage or transport.

### Deliverables

- normative `memory.v1alpha1` types and service interface;
- explicit View definition, Grant, and opaque Runtime capability roles;
- SourceContext, RecallBudget, error envelope, and ReceiptStatus contracts;
- deterministic model-visible projections;
- in-memory reference service;
- reusable Go semantic conformance suite and Golden Path contract tests;
- acceptance, threat-model, and non-normative internals documents.

### Explicit non-goals

- SQLite, FTS engine selection, a production transport, or `memoryd` packaging;
- Caelis code changes or sidecar supervision;
- Steward, semantic records, lifecycle, public memory, or management UI;
- JWT, key rotation, remote federation, or stable `v1` publication.

### Review slice

One Contract Baseline change in the Memory repository. The normative API package
must not depend on reference, storage, SDK implementation, or Caelis packages.

### Validation and exit

- public data-plane wire types compile and their JSON fixtures remain stable;
- wrong actor, audience, operation, expired/revoked capability fail closed;
- Agent-visible inputs contain no identity, View, Space, audience, or policy;
- empty Recall differs from unavailable;
- idempotent retry and read-your-writes pass;
- private/shared candidates are isolated before result merging;
- model projection is byte-exact;
- `go test ./...`, `go test -race ./...`, format, and diff checks pass.

Golden Path: runs in memory through step 6. Restart and offline replay remain M1
and M2 work because the reference service is intentionally non-durable.
M0 must not claim that `INV-DUR-001` passed; the first executable proof of
durable acknowledgement is the M1 crash/restart harness.

## M1: Standalone Durable Core

### Goal

Ship a useful `memoryd` that depends on neither Caelis nor a model.

### Deliverables

- SQLite-backed `memoryd` with schema migration and owner lock;
- immutable receipt payload plus separate processing state;
- durable Realm, Identity, private/shared Space, View, Grant, capability, and
  idempotency state;
- FTS over every receipt, per-Space candidate generation, and rebuild;
- read-your-writes across service restart;
- local opaque capability issuer and minimum bootstrap/inspect CLI;
- health, readiness, graceful shutdown, and local transport.

### Explicit non-goals

- Caelis tools, product UI, Steward, semantic records, vector/graph search;
- public/mixed audience, remote multi-tenancy, or signed token format;
- correction, publication, automatic retention, or rich backup UX.

### Review slice

The durable service, migration, CLI, and failure-injection evidence form one
standalone behavior slice. Storage schema is private and may evolve by migration;
API behavior is the compatibility boundary.

### Validation and exit

- kill the process immediately after accepted Remember; restart retains it;
- same effect retry produces one receipt; same text with different keys produces
  independent evidence;
- an old receipt remains Recallable with Steward permanently disabled;
- Bot-A cannot query Bot-B private data; shared query touches no private index;
- deleting FTS state and rebuilding from receipts restores results;
- disk-full, locked database, migration failure, and unknown outcome are
  explicit and never produce false acceptance;
- focused race tests cover concurrent Remember, Recall, revoke, and shutdown.
- the durable conformance harness proves `INV-DUR-001` against a separate
  process and real storage, not the M0 reference maps.

Golden Path: standalone steps 1-6 survive `memoryd` restart.

## M2: Caelis End-to-End Alpha

### Goal

Deliver the first real user value chain before Governance or Steward expansion.

### Memory deliverables

- versioned local transport and compatibility handshake;
- managed-sidecar artifact with exact version and digest;
- capability issuance/renewal needed by one active Runtime;
- SDK behavior required by Caelis integration and conformance fixtures.

### Caelis deliverables

- an opaque host-selected `BindingRef` that resolves to a Control-owned
  `RuntimeActorRef`, `OutputAudience`, and immutable Memory binding snapshot;
- private/shared single-audience admission rules;
- managed-sidecar supervisor and pure SDK client;
- exactly two model tools: `remember(text)` and `recall(query)`;
- hidden idempotency, SourceContext, budget, capability, and consistency data;
- canonical model-visible ToolResult persistence and zero-call replay;
- feature flag and kill switch.

### Explicit limits

One Runtime has one actor; one canonical Session has one audience; public,
mixed-audience, private child-to-shared parent flow, `remember_shared`, Steward,
and management UI are unsupported.

### Review slices

First publish an exact Memory API/SDK and service release candidate. Then review
the Caelis actor/audience foundation independently from supervisor/tool/replay
composition. The final Alpha acceptance pins both exact revisions.

### Validation and exit

- the complete Golden Path passes with real Caelis Sessions;
- restart of Caelis and `memoryd` preserves read-your-writes;
- offline Replay is byte-identical and performs zero Memory calls;
- unavailable is visible as unavailable rather than empty;
- capabilities, bearer credentials, and raw private text do not enter logs;
- consistency cursors appear only in model-hidden audience-protected metadata;
- disabling the feature removes tools without deleting appliance data.

Golden Path: all eight steps pass on the first supported native platform.

## M3: Governance and Production Safety

### Goal

Make the Alpha controllable, recoverable, and safe enough for default use.

### Deliverables

- minimum inspect/search, delete, export, and correction management APIs;
- `memoryctl` management commands before a rich product Surface;
- backup, restore, generation semantics, migration, and rollback;
- capacity, storage, receipt, projection, and capability diagnostics;
- documented Memory deletion versus Caelis Session-history boundary;
- supported-platform sidecar packaging and upgrade/disable procedures.

### Explicit non-goals

Rich TUI, public publication, reinterpretation, automatic retention, full
bitemporal query, and remote organization tenancy remain later extensions.

### Review slice

Management authorization is independent from Runtime capability issuance.
Caelis configuration stores references only; appliance policy and state do not
become a second Control-owned mirror.

### Validation and exit

- users can trace a Recall fragment to its receipt;
- deleted appliance data does not reappear after restart or reindex;
- Session copies remain explicitly discoverable as a separate erasure scope;
- backup/restore changes storage generation and stale tokens fail explicitly;
- failed upgrades roll back without acknowledged receipt loss;
- management credential rotation is recoverable across either rename outcome;
- platform artifacts identify exact version and digest.

## M4: Semantic Steward Extension

### Goal

Add model-managed organization as a replaceable enhancement over the durable
receipt/FTS path.

### Deliverables

- versioned Record, Revision, and Evidence structures;
- durable jobs, leases, proposal validation, and canonical application owned by
  the appliance;
- versioned prompt-policy profiles owned by the appliance, with no provider,
  model, credential, or outbound model transport configuration;
- a versioned external Steward Worker protocol and Go SDK runner through which
  a downstream host injects its existing model stack;
- typed `ADD`, `MERGE`, `SUPERSEDE`, and `IGNORE` proposals;
- deterministic validation, optimistic revision checks, and retry semantics;
- Recall merge/dedup between receipt and semantic candidates;
- observable fallback to receipt-only Recall.

### Explicit non-goals

Model physical deletion, ACL changes, cross-Space consolidation, automatic
private-to-shared publication, RELATE/PROMOTE/DEMOTE/ARCHIVE, model-planned
Recall, and hot/warm/cold physical tiers are excluded from the first extension.

### Review slice

Steward can be disabled without schema loss, data-plane unavailability, or
different authorization. Model output never writes directly to canonical state.

### Validation and exit

- malformed or unsupported proposals produce no mutation;
- every derived claim has evidence in the same Space;
- private and shared jobs cannot cite or mutate across boundaries;
- unknown worker outcome does not duplicate a mutation;
- worker/model outage and profile replacement leave baseline Recall working;
- changing the downstream model or an appliance prompt policy affects only
  later jobs unless an explicit future reinterpretation operation is invoked.

## M5: Release Candidate and GA

### Goal

Close cross-system reliability, security, quality, packaging, and operations.
No new Memory domain features enter this milestone.

### Hard release gates

- acknowledged receipt loss: zero;
- idempotent retry duplicate effects: zero;
- unauthorized candidate access: zero;
- private/shared leakage: zero;
- Replay Memory API calls: zero;
- Recall fragment provenance coverage: 100%;
- read-your-writes failures: zero;
- upgrade, rollback, restore, and disable lose no acknowledged data;
- API module, SDK, `memoryd`, Caelis, and package digests are exactly pinned.

Performance and retrieval-quality budgets are frozen from measured M1/M4
baselines rather than guessed at M0. The RC corpus reaches 200 fixed cases; its
model, prompt, and evaluation set do not change during one acceptance run.

### Not GA blockers

Public memory, mixed-audience Sessions, rich management UI, graph/vector-only
Recall, automatic publication, remote organization multi-tenancy, and a
permanent Steward conversation are not Core GA requirements.

### Release and operations evidence

- native validation for every supported platform, not cross-compilation alone;
- exact-SHA CI, signed or checksummed artifacts, compatibility handshake, and
  upgrade-from-minimum-supported-version tests;
- documented owner for alerts, storage exhaustion, corruption, backup recovery,
  capability incidents, and rollback;
- post-release smoke of the Golden Path with public artifact consumers.

## GA Closure after `v0.5.0-rc.1`

RC1 proves the standalone contract and the first native sidecar candidate. It
does not close the product activation, external Steward execution, realistic
long-run retrieval, process-attachment, or formal platform matrix required for
GA. The following slices are ordered, independently reviewable, and must not be
collapsed into one long-running release task:

```text
G0 Memory Contract Correction
  -> G1 Caelis SDK Activation
    -> G2 Caelis Sidecar Lifecycle
      -> G3 External Steward Worker
        -> G4 Corpus and Soak Qualification
          -> G5 Six-Platform Distribution
            -> G6 RC2, External Review, and GA
```

G0, G3, the Memory half of G4, and the Memory artifact half of G5 belong to
this repository. G1, G2, Caelis product packaging, and zero-call Session Replay
evidence belong to the Caelis repository. Cross-repository acceptance pins the
exact source revisions and artifact digests from both repositories.

### G0: Memory Contract Correction

Goal: remove model-provider ownership from `memoryd` before a stable API freezes.

Deliverables:

- revise Spec, threat model, operations, and acceptance language so the
  appliance owns policy, durable work, evidence, validation, and mutation only;
- remove provider/model/credential fields from the public Steward profile;
- define a least-authority Worker credential and claim/apply/fail protocol;
- retain a documented private-schema upgrade residue only when required to
  upgrade RC1 data, with a named removal condition.

Exit: `memoryd` has no model-provider endpoint, model name, model credential,
or outbound model HTTP configuration; baseline Recall remains independent of a
Worker. This slice changes no Caelis code.

### G1: Caelis SDK Activation

Goal: make the already-published SDK tools visible to eligible Caelis Runtime
models without teaching Caelis Memory domain or concrete product concepts.

Deliverables:

- initialize the Memory client and opaque binding resolver from product
  configuration, with one explicit default binding;
- inject exactly `remember(text)` and `recall(query)` during Runtime assembly;
- preserve hidden actor, audience, capability, idempotency, budget, and cursor
  values and byte-exact zero-call Replay;
- add configuration diagnostics that distinguish disabled, unconfigured,
  incompatible, and unavailable.

Exit: a real local model sees both tools only when a valid binding exists. This
contract does not define a Bot, user, tenant, workspace, or other product
identity. A future product layer may map any such concept to a `BindingRef`
without changing Runtime assembly or the Memory SDK.

### G2: Caelis Sidecar Lifecycle

Goal: make a managed `memoryd` reliable across multiple local Caelis processes.

Deliverables:

- per-Runtime immutable opaque binding rather than one process-global actor
  binding;
- attach to a healthy compatible owner process after supervisor or Caelis
  restart instead of treating every extant lock as a fatal collision;
- exact manifest/digest verification, readiness, shutdown ownership, version
  mismatch, orphan, and kill-switch behavior;
- crash/restart and concurrent-process product tests.

Exit: one owner appliance can safely serve independently bound Runtimes and an
orphaned healthy sidecar can be reattached without acknowledged-memory loss.

### G3: External Steward Worker

Goal: keep semantic organization replaceable without embedding a second model
provider stack in Memory.

Deliverables:

- external Worker claim/apply/fail API authenticated independently from Runtime
  capabilities and owner management;
- Go SDK `Generator` callback and bounded runner; the callback receives only a
  bounded `WorkRequest`, never a Space, capability, lease, or bearer;
- appliance-owned leases, retry ceilings, stable failure codes, proposal shape,
  evidence checks, revision conflicts, and atomic application;
- worker-crash, lease-expiry, malformed-output, duplicate-apply, and no-worker
  fallback tests.

Exit: downstream code can reuse its existing provider configuration and inject
a model callback, while `memoryd` makes no outbound provider call and receipt
Recall remains useful with zero Workers.

### G4: Corpus and Soak Qualification

Goal: validate useful behavior across multi-round remembering and evolving
facts rather than only protocol fixtures.

Deliverables:

- a reviewed, privacy-safe Chinese and mixed-language corpus covering exact
  recall, paraphrase, chronology, corrections, contradictions, supersession,
  abstention, and private/shared isolation;
- a repeatable importer/evaluator for sanitized Caelis Session JSONL or an
  operator-selected local MEMORY text source; raw private source text is never
  committed or uploaded by the harness;
- at least 100 Spaces, 100,000 receipts, and 10,000 semantic Record heads in the
  release soak, including restart, reindex, backup/restore, and Worker backlog;
- frozen Recall@k, precision, stale-current, unsupported-claim, leakage,
  latency, database, WAL, and backlog-recovery reports for one candidate.

Exit: the fixed release corpus contains at least 200 labeled cases, has complete
provenance and zero private/shared leakage, and meets budgets frozen before the
candidate run. Synthetic fixtures remain useful but are not sufficient alone.

### G5: Six-Platform Distribution

Goal: give Memory the same formal desktop/server matrix as Caelis.

The GA support matrix is exactly:

- `darwin/amd64`, `darwin/arm64`;
- `linux/amd64`, `linux/arm64`;
- `windows/amd64`, `windows/arm64`.

Deliverables:

- an endpoint abstraction with Unix Domain Socket implementations on Darwin
  and Linux and a named-pipe implementation on Windows, preserving identical
  application protocol and authorization semantics;
- `memoryd` and public operator `memoryctl` artifacts for every platform;
- exact version, revision, protocols, platform, filename, and SHA-256 in one
  detached-checksum manifest contract;
- native lifecycle, credential-permission, lock, upgrade, rollback, and Golden
  Path evidence for every supported platform; cross-compilation alone is only a
  buildability check.

Linux native evidence is collected in Rocky Linux under the local OrbStack
environment. The report records Rocky version, kernel, architecture, Go
toolchain, and whether execution was native. The current `rocky` machine is
Linux/ARM64; it can close `linux/arm64` but cannot substitute for a native
Rocky Linux/AMD64 run.

Exit: all twelve binaries (two executables times six platforms) are produced and
each platform has native or explicitly approved equivalent execution evidence.
Unsupported build previews are never listed in the support manifest.

### G6: RC2, External Review, and GA

Goal: create one immutable candidate, pause for independent review, then publish
the exact accepted bytes.

Deliverables:

- exact-SHA Memory and Caelis gates, compatibility pins, SBOM/checksums, upgrade
  from the minimum supported schema, rollback, and clean-consumer smoke;
- an RC2 evidence bundle mapping every acceptance ID to logs and artifacts;
- an explicit external-review hold point: no GA tag or publication while review
  findings remain unresolved or accepted-risk decisions remain undocumented;
- post-approval publication and public-artifact Golden Path smoke.

Exit: GA is the reviewed RC2 bytes, not a rebuild. The overall RoadMap goal may
close after the evidence bundle passes external acceptance and before any
post-GA ecosystem integration begins.

### Post-GA integration extensions

CLI and MCP as Agent-facing Remember/Recall entry points are deferred. The
preferred ecosystem surface is a publishable Plugin that packages the stable
SDK and external Worker callback for additional Agents. Plugin work may be
designed before GA but cannot change the Core data-plane or Worker contracts and
is not a GA blocker. `memoryctl` is different: it is the required operator and
recovery artifact in G5, not an Agent integration surface.

## Milestone tracking template

Each milestone issue uses:

```text
Goal
Architectural invariants touched
Deliverables (at most five review groups)
Explicit non-goals
Golden Path delta
Acceptance test IDs
Demo command
Rollback or disable path
Evidence links
```

The [acceptance plan](memory-appliance-acceptance.md) owns stable test IDs and
matrices. The roadmap owns ordering and exit conditions only.
