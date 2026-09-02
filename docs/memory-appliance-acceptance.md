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
| `LABEL-001` | A Host-issued capability fixes one canonical LabelSet; labels are absent from model tool arguments, results, and Steward model work |
| `LABEL-002` | Remember, Recall, ReceiptStatus, consistency tokens, correction, and derived Recall match the capability LabelSet exactly, including after restart |
| `LABEL-003` | Steward promotion and refinement inherit one LabelSet and reject records or receipt evidence from every other LabelSet |
| `PROV-001` | Every Recall fragment contains complete receipt and/or Record provenance |
| `REPLAY-001` | Canonical Session Replay uses stored ToolResults and performs zero Memory effects |
| `STATIC-001` | With no Steward model binding, Remember/Recall remains useful and consumes zero model tokens |
| `STEWARD-001` | Model output is only a proposal; Memory validates and commits or rejects it deterministically |
| `STEWARD-002` | A proposal cannot widen Space visibility, edit receipts, publish, or delete |

## Package acceptance

| ID | Acceptance |
| --- | --- |
| `PKG-001` | An external Go consumer imports public `appliance`, `api`, and `sdk` packages without importing `internal/*` |
| `PKG-002` | `appliance.Open` synchronously initializes a fresh database, reopens the exact current baseline, and explicitly rejects older unreleased development schemas |
| `PKG-003` | Direct `DataPlane` Remember/Recall passes the shared semantic and durable suites |
| `PKG-004` | Embedded and retained local-transport adapters execute the same Memory authority and contracts |
| `PKG-005` | The public facade exposes no SQL handle, concrete Store, index, schema mutation, or model-provider configuration |
| `PKG-006` | `Close` releases SQLite and the data-directory owner lock after work drains |
| `PKG-007` | Runtime access and shutdown are race-free and repeated concurrent `Close` calls return one stable result |
| `PKG-008` | The single pre-release schema baseline persists empty and non-empty LabelSets without a second authority store; compatibility migrations become a release requirement only after a published schema floor exists |

## Caelis integration acceptance

| ID | Acceptance |
| --- | --- |
| `INT-001` | A fresh offline Caelis Store starts with Memory enabled and no Memory setup input |
| `INT-002` | Successful Host construction implies the embedded Memory runtime is already open at its current schema baseline |
| `INT-003` | The integration contains no downloader, installer, manifest, digest pin, endpoint, supervisor, readiness probe, or runtime compatibility handshake |
| `INT-004` | An activated admitted Runtime sees exactly one `remember(text)` and one `recall(query)` tool |
| `INT-005` | Tool inputs contain only the fact text or query; all authority, source, budget, and consistency fields remain hidden |
| `INT-006` | Session binding state contains logical binding, actor, principal, issuer reference, audience, View, Grant, version, and cursor, but no endpoint or artifact identity |
| `INT-007` | Restarting Caelis preserves Memory data and hidden consistency state |
| `INT-008` | The only ordinary Memory choice is the system-managed Steward model binding |
| `INT-009` | Future product concepts may select an opaque binding without adding Bot, tenant, user, or workspace types to Memory |
| `INT-010` | Runtime assembly may map product context to an opaque LabelSet, but the projected Remember/Recall schemas remain text-only |

## Steward acceptance

| ID | Acceptance |
| --- | --- |
| `STW-001` | No explicit model binding leaves the static path active and creates no later semantic jobs |
| `STW-002` | An explicit binding uses Caelis' existing model profile, provider, credentials, timeout, and accounting |
| `STW-003` | Memory owns the prompt-policy profile, bounded evidence, leases, retry ceiling, validation, and canonical apply |
| `STW-004` | Memory stores no provider endpoint, model name, token, billing, or provider retry configuration |
| `STW-005` | Removing the binding stops later model calls without deleting receipts or semantic history |
| `STW-006` | Malformed, cross-Space, cross-LabelSet, stale-revision, duplicate-evidence, and unsupported proposals produce no mutation |
| `STW-007` | Memory, not Caelis, renders the complete proposal shapes and strictly parses one proposal; correctness does not require provider-native JSON Schema output |
| `STW-008` | With no eligible new evidence or explicit organization action, Steward makes zero model calls regardless of elapsed wall-clock time |
| `COST-002` | Automatic Steward work has a frozen per-receipt model-call budget; any additional model retry waits for an active task or explicit bounded organization action |

## Product-foundation acceptance

| ID | Acceptance |
| --- | --- |
| `CORPUS-001` | Every eligible local Caelis Session is projected from checkpointed canonical Session Service events, never by scanning physical JSONL |
| `CORPUS-002` | Projection is resumable and idempotent and preserves bounded source provenance without duplicating facts after restart |
| `CORPUS-003` | System/developer prompts, hidden reasoning, credentials, approvals, transient progress, and unsanitized tool payloads are excluded with aggregate reason counts |
| `CORPUS-004` | Explicit Remember and observational Session evidence are distinguishable through a trusted model-hidden ingestion boundary; untrusted `SourceContext` labels cannot establish source authority |
| `TIME-001` | Evaluation records the exact bounded active-window, source-intent, reinforcement, admission, and result-budget parameters used by each product projection |
| `TIME-002` | On a frozen corpus whose age range can be exercised in a bounded test, recent supported facts outrank stale keyword-only facts while explicit historical lookup can still return old evidence |
| `TIME-003` | When explicit Recall returns non-current evidence, the model-visible result qualifies its temporal status; automatic briefing omits stale or unresolved evidence unless the task explicitly requests history |
| `RETR-001` | A bounded model-free keyphrase/association projection improves the frozen non-literal task set over exact lexical retrieval without violating result budgets or private isolation |
| `RETR-002` | Explicit Recall and automatic briefing use separately frozen ranking and abstention policies rather than one universal score |
| `BRIEF-001` | A new stateless Session can obtain a byte-bounded task-relevant briefing with evidence provenance and zero model calls |
| `BRIEF-002` | A briefing contains no permission, authorization, imperative instruction, or persistent Session identity and is treated only as advisory context |
| `BRIEF-003` | The frozen task-context benchmark improves over an empty-context control for similar tasks, stable preferences, and prior decisions without increasing private leakage |
| `BRIEF-004` | Empty briefing is a valid result, and the candidate does not increase frozen harmful-context or false-memory errors over the empty-context control |
| `COST-001` | Ingestion, sanitization, indexing, time ranking, retrieval, deduplication, and default briefing generation consume zero model tokens |
| `EXP-LEX-001` | Default runtime and schema initialization do not learn, read, rebuild for, or expose adaptive lexicon terms; focused experiments require an explicit internal option |
| `EXP-RET-001` | Embedding, graph, hierarchy, or adaptive retrieval can enter a default path only after a same-corpus blind holdout shows material end-to-end gain and records dependency, latency, storage, rebuild, privacy, and rollback cost |

## Future identity acceptance

These IDs do not gate the stateless first release. They become mandatory only
when a downstream product ships persistent identity behavior.

| ID | Acceptance |
| --- | --- |
| `IDENT-001` | A fixed byte-bounded identity capsule is evidence-backed, inspectable, and separate from an ordinary stateless Session briefing |
| `IDENT-002` | A product maps identity and work context to opaque LabelSets without introducing Bot, tenant, user, or workspace types into Memory |
| `IDENT-003` | Tree or graph memory must beat the flat Record-head-plus-search control on a frozen longitudinal corpus before becoming default |
| `IDENT-004` | Refinement and promotion affect only derived flat projections; immutable evidence and Revision history remain attributable and support deterministic model-free rebuild |

## Realistic corpus acceptance

Current static-path evidence is recorded in
[Local Memory Registry Corpus Evidence](evidence/memory-registry-corpus-2026-09-02.md).
It satisfies durability, bounded lexical reachability, restart, and private
isolation evidence for that source digest; it does not close semantic-model
quality acceptance.

Every package prerelease also runs the public release corpus in
`internal/appliance/testdata/release_corpus`. Its 224 fixed cases separately
gate Chinese, English, and six other languages across four durable rounds. The
manifest freezes source digests, case counts, Recall@1/5 thresholds, and a
750ms p95 interaction budget. Same-Space/different-LabelSet and
different-Space/same-LabelSet adversarial records must remain searchable in
their own partition and invisible from the target partition.

## Product-experience performance

Performance acceptance uses user-perceived behavior, not workstation
microsecond targets. The reproducible package corpus applies a broad 750ms p95
Recall ceiling; the exact candidate also records p50/p95/p99 for embedded Open,
Remember, Recall, and briefing on a representative user machine. The candidate
fails when Memory crosses the interaction ceiling, introduces an observable
startup stall, blocks interaction past the Host's existing deadline, or shows a
material unexplained regression against the previous accepted candidate.
Finer numeric observations remain evidence rather than machine-specific API
guarantees.

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

Quality reports keep separate tuning and blind holdout partitions and compare
empty-context, exact-lexical, best model-free, and model-assisted variants where
applicable. They include rejected parameter points and measure stale-fact
suppression, current-fact retention, contradiction errors, correct abstention,
and harmful briefing context in addition to Retrieval@k and ranking metrics.
Public long-conversation benchmarks may supplement this corpus but cannot
replace product-shaped Session, privacy, authority, cost, and abstention cases.

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
| `GA-008` | `CORPUS-001..004`, `TIME-001..003`, `RETR-001..002`, `BRIEF-001..004`, and `COST-001..002` pass at the exact candidate revision |

Standalone command buildability does not satisfy a GA artifact gate. Independent
`memoryd` publication has its own deferred acceptance plan when a concrete
external consumer justifies that product.
