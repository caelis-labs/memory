# Memory

Memory is an independently running memory appliance for Agents. Its stable
model-facing surface is deliberately small:

```text
remember(text)
recall(query)
```

The appliance, rather than an Agent host, owns identity continuity, Spaces,
Views, durable receipts, retrieval, classification, consolidation, lifecycle,
and forgetting. The repository ships the standalone durable Core plus the M2
host-integration boundary: exact compatibility handshake, issuer-plane client,
and digest-verifiable native sidecar manifest.

## Current milestone

M0 froze the contract and M1 provides:

- immutable durable receipts with separate processing state;
- Realm, Identity, private/shared Space, View, Grant, issuer, capability, and
  idempotency state in a migrated SQLite authority;
- independent per-Space FTS5 projections rebuildable from receipts;
- owner locking, owner-only credentials and Unix Socket transport;
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
- `make sidecar`, which accepts only a clean exact HEAD and emits a native
  executable plus SHA-256 manifest.

The appliance remains deliberately model-free and has no completed Caelis integration, Steward,
semantic records, vector/graph retrieval, governance deletion, backup, or rich
management Surface. Those boundaries remain assigned to later milestones.

Read [the specification](docs/memory-appliance-spec.md),
[roadmap](docs/memory-appliance-roadmap.md), and
[acceptance plan](docs/memory-appliance-acceptance.md) before extending the API.
Use the [memoryd operations guide](docs/memoryd-operations.md) to run the standalone
Golden Path.

Run the current gates with:

```sh
make check
make race
make durable
```

The repository gates set `GOWORK=off` so the module remains independently
buildable even when cloned beside a parent development workspace.
