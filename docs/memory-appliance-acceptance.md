# Memory Package Acceptance Plan

This document owns executable acceptance IDs for the Go package and its Caelis
integration. Evidence applies only to the exact source revisions and datasets
that produced it.

## Core invariants

| ID | Acceptance |
| --- | --- |
| `DUR-001` | `accepted=true` is returned only after the immutable receipt and baseline projection commit durably |
| `DUR-002` | Restarting the Caelis Host retains every acknowledged receipt |
| `IDEM-001` | Retrying the same Remember effect identity produces one receipt and the original result |
| `CONS-001` | A successful Remember is immediately Recallable under its returned consistency token |
| `AUTH-001` | Authorization completes before a Space index is queried |
| `AUTH-002` | Private/shared Views cannot query or mutate an unauthorized Space |
| `AUTH-003` | Model arguments cannot choose Identity, Space, View, Grant, actor, audience, or capability |
| `PROV-001` | Every Recall fragment contains complete receipt and/or Record provenance |
| `REPLAY-001` | Canonical Session Replay uses stored ToolResults and performs zero Memory effects |
| `STATIC-001` | With no Steward model binding, Remember/Recall remains useful and consumes zero model tokens |
| `STEWARD-001` | Model output is only a proposal; Memory validates and commits or rejects it deterministically |
| `STEWARD-002` | A proposal cannot widen Space visibility, edit receipts, publish, or delete |

## Package acceptance

| ID | Acceptance |
| --- | --- |
| `PKG-001` | An external Go consumer imports public `appliance`, `api`, and `sdk` packages without importing `internal/*` |
| `PKG-002` | `appliance.Open` synchronously opens and migrates a fresh or existing database |
| `PKG-003` | Direct `DataPlane` Remember/Recall passes the shared semantic and durable suites |
| `PKG-004` | Embedded and retained local-transport adapters execute the same Memory authority and contracts |
| `PKG-005` | The public facade exposes no SQL handle, concrete Store, index, schema mutation, or model-provider configuration |
| `PKG-006` | `Close` releases SQLite and the data-directory owner lock after work drains |

## Caelis integration acceptance

| ID | Acceptance |
| --- | --- |
| `INT-001` | A fresh offline Caelis Store starts with Memory enabled and no Memory setup input |
| `INT-002` | Successful Host construction implies the embedded Memory runtime is already open and migrated |
| `INT-003` | The integration contains no downloader, installer, manifest, digest pin, endpoint, supervisor, readiness probe, or runtime compatibility handshake |
| `INT-004` | An activated admitted Runtime sees exactly one `remember(text)` and one `recall(query)` tool |
| `INT-005` | Tool inputs contain only the fact text or query; all authority, source, budget, and consistency fields remain hidden |
| `INT-006` | Session binding state contains logical binding, actor, principal, issuer reference, audience, View, Grant, version, and cursor, but no endpoint or artifact identity |
| `INT-007` | Restarting Caelis preserves Memory data and hidden consistency state |
| `INT-008` | The only ordinary Memory choice is the system-managed Steward model binding |
| `INT-009` | Future product concepts may select an opaque binding without adding Bot, tenant, user, or workspace types to Memory |

## Steward acceptance

| ID | Acceptance |
| --- | --- |
| `STW-001` | No explicit model binding leaves the static path active and creates no later semantic jobs |
| `STW-002` | An explicit binding uses Caelis' existing model profile, provider, credentials, timeout, and accounting |
| `STW-003` | Memory owns the prompt-policy profile, bounded evidence, leases, retry ceiling, validation, and canonical apply |
| `STW-004` | Memory stores no provider endpoint, model name, token, billing, or provider retry configuration |
| `STW-005` | Removing the binding stops later model calls without deleting receipts or semantic history |
| `STW-006` | Malformed, cross-Space, stale-revision, duplicate-evidence, and unsupported proposals produce no mutation |

## Realistic corpus acceptance

Current static-path evidence is recorded in
[Local Memory Registry Corpus Evidence](evidence/memory-registry-corpus-2026-09-02.md).
It satisfies durability, bounded lexical reachability, restart, and private
isolation evidence for that source digest; it does not close semantic-model
quality acceptance.

## Product-experience performance

Performance gates use user-perceived latency, not workstation microbenchmark
targets. With 200 fixed samples on the candidate development host:

- hot Remember and Recall p99 must each remain at or below 100ms;
- first-use Remember p99 must remain at or below 250ms;
- synchronous embedded Open p99 must remain at or below one second.

Index rebuild, backup, restore, and Steward backlog metrics are retained as
descriptive operational evidence. Their correctness, bounded completion, and
failure recovery remain hard tests, but development-machine throughput does not
block the product candidate.

The fixed corpus includes Chinese, English, and mixed-language facts across at
least three Remember/Recall rounds and one Host restart. Cases cover:

- durable preferences and project facts;
- paraphrased queries and sparse keywords;
- repeated facts and idempotent versus independent effects;
- contradiction, correction, merge, supersession, and unrelated noise;
- private/shared isolation;
- static fallback before, during, and after Steward use;
- provenance and Session Replay.

Deterministic correctness cases gate every candidate. Model-assisted quality
reports record model, prompt profile, dataset revision, latency, token use, and
failure distribution, but cannot replace deterministic gates or silently change
their thresholds.

## GA gates

| ID | Acceptance |
| --- | --- |
| `GA-001` | Zero acknowledged receipt loss, duplicate idempotent effects, unauthorized candidate reads, private/shared leaks, Replay calls, provenance gaps, and read-your-writes failures |
| `GA-002` | Memory and Caelis focused tests, race tests, full tests, builds, architecture checks, docs links, and diff checks pass at exact revisions |
| `GA-003` | Caelis builds/tests the imported package on Darwin, Linux, and Windows AMD64/ARM64 |
| `GA-004` | Linux native evidence is recorded from the local OrbStack Rocky environment |
| `GA-005` | Upgrade from the supported pre-GA Memory schema retains acknowledged facts and authority |
| `GA-006` | A clean offline Caelis installation completes the full Golden Path with no Memory download or configuration |
| `GA-007` | An external reviewer maps every finding to a fix, acceptance ID, or explicitly accepted risk before GA authorization |

Standalone command buildability does not satisfy a GA artifact gate. Independent
`memoryd` publication has its own deferred acceptance plan when a concrete
external consumer justifies that product.
