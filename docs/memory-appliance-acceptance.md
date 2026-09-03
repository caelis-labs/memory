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
| `AUTH-004` | Host readiness can validate the exact issuer, Grant, View, actor, audience, and operations without issuing a bearer or persisting capability state |
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
| `PKG-002` | `appliance.Open` synchronously initializes a fresh database, reopens the exact `memory-v0.5.0` baseline, promotes the byte-identical final prerelease baseline without data loss, and rejects every other unsupported schema |
| `PKG-003` | Direct `DataPlane` Remember/Recall passes the shared semantic and durable suites |
| `PKG-004` | Embedded and retained local-transport adapters execute the same Memory authority and contracts |
| `PKG-005` | The public facade exposes no SQL handle, concrete Store, index, schema mutation, or model-provider configuration |
| `PKG-006` | `Close` releases SQLite and the data-directory owner lock after work drains |
| `PKG-007` | Runtime access and shutdown are race-free and repeated concurrent `Close` calls return one stable result |
| `PKG-008` | The `memory-v0.5.0` baseline is the first published schema floor; every later schema change supplies an explicit forward migration and preserves empty and non-empty LabelSets without a second authority store |

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

## Corpus and projection acceptance (post-v0.5.0)

These IDs apply to P3-P5 and do not gate the Fact Memory `v0.5.0` release.

| ID | Acceptance |
| --- | --- |
| `CORPUS-API-001` | A separately versioned source-neutral protocol defines Corpus, Leaf, immutable LeafRevision, ordered Item, state, digest, consistency, budget, and provenance without changing current Remember/Recall or Steward semantics |
| `CORPUS-API-002` | The protocol contains no Session, JSONL, event, path, checkpoint, sanitizer, provider, model, or credential semantics and passes conformance with two unrelated producers |
| `CORPUS-API-003` | Distinct capability operations independently authorize commit, lifecycle, exact read, query, and management effects under one exact partition |
| `CORPUS-001` | One LeafRevision contains one ordered producer projection with opaque SourceRef, SourceVersion, ProjectionRef, Item references, and exact content digests |
| `CORPUS-002` | Memory treats source and projection references as opaque, branches on none of their contents, and never reads or mutates the external source |
| `CORPUS-003` | The producer owns source parsing, admission, sanitization, and source versioning; Memory owns shape, digest, byte budget, idempotency, revision, lifecycle, and authority validation after admission |
| `CORPUS-004` | Commit uses an expected head, canonical request digest, and idempotency key; replay has one effect and changed payload under the same key conflicts |
| `CORPUS-005` | Authorization completes before Item access, and every Leaf is bound to one exact `(SpaceID, LabelSetDigest)` partition selected outside model-facing input |
| `CORPUS-006` | The direct Item lexical index is independently useful, rebuildable from LeafRevisions, and remains available with every optional hierarchy, summary, dense, and graph projection disabled |
| `CORPUS-007` | A downstream Caelis example maps one concrete Session JSONL projection to one Leaf without adding Session knowledge to Memory |
| `CORPUS-008` | Retraction preserves owner-auditable history; redaction and erasure prevent resurrection and remove affected Leaf content plus every transitively derived payload before it can be returned |
| `PROJ-001` | RollupManifest, SummaryArtifactRevision, IndexArtifactRevision, and ProjectionSnapshot are separate structures with exact input and policy references; none is evidence authority |
| `PROJ-002` | The same committed Leaf revisions and topology policy reproduce the same structural manifests and deterministic index artifacts; fan-out, grouping, slot, root/forest, and traversal choices remain versioned experimental policy |
| `PROJ-003` | Every summary assertion and expansion entry has bounded support provenance; model output remains an untrusted proposal for an already fixed manifest |
| `PROJ-004` | A changed or invalid Leaf makes every dependent stale artifact unavailable before retrieval can observe it, while the prior complete snapshot remains coherent |
| `PROJ-005` | Crash and retry at commit, materialize, apply, invalidate, and snapshot-activation boundaries produce at most one accepted effect per input digest |
| `PROJ-006` | Projection generation activation is atomic and exposes coverage, consistency, degraded, and truncation state without blocking direct Corpus query or Fact Memory Recall |
| `QUERY-001` | Comparative evaluation isolates direct Item indexing, collapsed cross-level retrieval, controlled expansion, and summary assistance instead of attributing every gain to a hierarchy |
| `QUERY-002` | Every query authorizes first, revalidates active Leaf evidence before disclosure, and independently bounds candidates, expansion, returned Items, encoded bytes, and time |
| `QUERY-003` | No optional query strategy becomes a product default until it materially beats direct Item retrieval on a frozen corpus without privacy, abstention, harmful-context, latency, storage, or rebuild regression |
| `PROJ-COST-001` | Reports record materializer and summarizer calls, tokens, latency, failures, storage, and rebuild cost per projection generation; idle time and unchanged input digests make zero calls |
| `EXP-LEX-001` | Default runtime and schema initialization do not learn, read, rebuild for, or expose adaptive lexicon terms; focused experiments require an explicit internal option |
| `EXP-RET-001` | Embedding, graph, adaptive, or hierarchical retrieval can enter a default path only after a same-corpus blind holdout shows material end-to-end gain and records dependency, latency, storage, rebuild, privacy, and rollback cost |

## Future identity acceptance

These IDs do not gate the stateless first release. They become mandatory only
when a downstream product ships persistent identity behavior.

| ID | Acceptance |
| --- | --- |
| `IDENT-001` | A fixed byte-bounded identity view is evidence-backed, inspectable, and separate from ordinary Corpus query |
| `IDENT-002` | A product maps identity and work context to opaque LabelSets without introducing Bot, tenant, user, or workspace types into Memory |
| `IDENT-003` | An identity view selects only accepted Corpus evidence from exact authorized LabelSets; it cannot add another ownership or authorization system |
| `IDENT-004` | Refinement and promotion affect only derived projections; immutable receipts and committed Leaf revisions remain attributable and rebuildable |

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
Remember, and Recall on a representative user machine. The candidate
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
the current Fact Memory Recall control, direct Item indexing, collapsed
cross-level candidates, controlled expansion, and optional summary assistance.
Corpus reports measure Leaf request fidelity, ordered-Item and content-digest
integrity, direct-index quality, manifest completeness, artifact support
provenance, stale-artifact exclusion, Recall@k, candidates, expansion, bytes,
rebuild consistency, isolation, model calls, and tokens. They also include
rejected parameter points and measure contradiction errors, correct abstention,
and harmful returned context.
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
| `GA-008` | The checked-in 224-case multilingual release corpus and fixed 100-Space, 100,000-receipt, 10,000-Record GA soak pass at the exact candidate revision and retain aggregate evidence |

Standalone command buildability does not satisfy a GA artifact gate. Independent
`memoryd` publication has its own deferred acceptance plan when a concrete
external consumer justifies that product.
