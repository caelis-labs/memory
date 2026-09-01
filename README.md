# Memory

Memory is an independently running memory appliance for Agents. Its stable
model-facing surface is deliberately small:

```text
remember(text)
recall(query)
```

The appliance, rather than an Agent host, owns identity continuity, Spaces,
Views, durable receipts, retrieval, classification, consolidation, lifecycle,
and forgetting. The current repository is at the `memory.v1alpha1` contract
baseline; it does not yet ship the durable SQLite `memoryd` planned for M1.

## Current milestone

M0 provides:

- the normative data-plane and authorization contract;
- transport-neutral Go types and service interfaces;
- a Go SDK with deterministic model-visible projections;
- an in-memory reference service;
- a reusable semantic conformance suite and Golden Path tests.

M0 proves protocol and authorization semantics only. It cannot prove durable
acknowledgement; that claim requires the M1 cross-process crash/restart harness.

Read [the specification](docs/memory-appliance-spec.md),
[roadmap](docs/memory-appliance-roadmap.md), and
[acceptance plan](docs/memory-appliance-acceptance.md) before extending the API.

Run the current gates with:

```sh
make check
make race
```

The repository gates set `GOWORK=off` so the module remains independently
buildable even when cloned beside a parent development workspace.
