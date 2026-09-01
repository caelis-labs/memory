# Memory

Memory is an independently running memory appliance for Agents. Its stable
model-facing surface is deliberately small:

```text
remember(text)
recall(query)
```

The appliance, rather than an Agent host, owns identity continuity, Spaces,
Views, durable receipts, retrieval, classification, consolidation, lifecycle,
and forgetting. The repository ships the standalone durable Core, the M2
host-integration boundary, and the versioned owner-management foundation.

## Current capabilities

M0 froze the contract and M1 provides:

- immutable durable receipts with separate processing state;
- Realm, Identity, private/shared Space, View, Grant, issuer, capability, and
  idempotency state in a migrated SQLite authority;
- independent per-Space FTS5 projections rebuildable from receipts;
- owner locking, distinct owner-only management/Worker credentials, Unix Domain
  Socket transport, and a buildable Windows named-pipe transport preview;
- health, readiness, graceful shutdown, bootstrap, issue, issuer rotation,
  inspect, revoke, Remember, Recall, and FTS-rebuild commands;
- semantic conformance plus a separate-process crash/restart harness proving
  acknowledged durability and restart idempotency.

The Memory-owned M2 boundary additionally provides:

- exact local transport, API, and Core Profile handshake;
- immutable service version and source revision in packaged `memoryd` binaries;
- a public issuer plane that keeps issuer credentials outside request bodies;
- a Go SDK for handshake, Runtime capability issuance/renewal, and native
  sidecar manifest verification;
- `make sidecar`, which accepts only a clean exact HEAD and emits native
  `memoryd` and `memoryctl` executables with SHA-256 manifests and detached
  checksums.

M3 Governance and Production Safety additionally provides:

- versioned `memory.management.v1alpha1` wire types and Go client;
- owner-authorized receipt search and provenance trace;
- append-only, same-Space corrections that shadow rather than rewrite evidence;
- idempotent hard deletion with content-free tombstones and resurrection
  prevention;
- secret-free capacity, storage, receipt, projection, capability, restore, and
  rollback diagnostics;
- recoverable management-bearer rotation plus `memoryctl` governance commands;
- sensitive NDJSON export, streaming encrypted backup with a separate
  owner-only key, offline verified restore, storage-generation rotation, and a
  management-only verification state with explicit commit or rollback;
- lossless offline upgrade preparation and supported native sidecar packaging
  for macOS on Apple silicon (`darwin/arm64`).

The baseline appliance remains useful with every model and Worker disabled.
Other buildable platforms are preview-only until they receive native lifecycle
evidence. Caelis integration is independently owned and pins an exact published
Memory revision and sidecar digest.

The M4 Semantic Steward foundation additionally provides:

- a versioned Steward protocol and typed `ADD`, `MERGE`, `SUPERSEDE`, and
  `IGNORE` proposal vocabulary;
- migrated Record, immutable Revision/Evidence, profile, and durable Job state;
- deterministic same-Space validation, optimistic Revisions, governance
  invalidation, and idempotent unknown-outcome recovery;
- immutable prompt-policy profile versions and future-Job Space bindings through
  the owner management plane;
- a versioned external Worker claim/apply/fail protocol and Go SDK `Generator`
  callback, letting downstream hosts reuse their existing provider, model, and
  credentials;
- appliance-owned durable leases, retry ceilings, evidence validation, atomic
  application, and terminal poisoning controls, with no outbound model adapter
  in `memoryd`;
- authorization-first per-Space semantic Recall, deterministic receipt/Record
  merge and deduplication with complete provenance;
- receipt-only fallback with `degraded=true`, semantic projection rebuild, and
  secret-free profile, backlog, Record, and projection diagnostics.

`v0.5.0-rc.1` has native `darwin/arm64` candidate evidence. The GA RoadMap now
requires `darwin`/`linux`/`windows` on both `amd64` and `arm64`, packages both
`memoryd` and the operator-facing `memoryctl`, and pauses for external review
before publication. Agent-facing CLI/MCP adapters are deferred; the preferred
post-GA ecosystem surface is a publishable Plugin over the same SDK contracts.
The transport-neutral endpoint and Windows named-pipe/ACL/lock implementations
now cross-build on both Windows architectures; native lifecycle evidence remains
a release gate. Linux native gates run in the local OrbStack Rocky environment,
with the current ARM64 machine covering only `linux/arm64`.

Read [the specification](docs/memory-appliance-spec.md),
[roadmap](docs/memory-appliance-roadmap.md), and
[acceptance plan](docs/memory-appliance-acceptance.md) before extending the API.
Use the [memoryd operations guide](docs/memoryd-operations.md) to run the standalone
Golden Path. The [release procedure](docs/memory-appliance-release.md) owns M5
and GA Closure quality, native artifact, incident, upgrade, and publication gates. The
[corpus evaluation procedure](docs/memory-appliance-evaluation.md) measures
privacy-safe multi-round behavior with local Markdown or Session JSONL sources.

Run the current gates with:

```sh
make check
make race
make durable
make release-candidate
```

The repository gates set `GOWORK=off` so the module remains independently
buildable even when cloned beside a parent development workspace.
