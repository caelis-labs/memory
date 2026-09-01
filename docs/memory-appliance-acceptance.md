# Memory Appliance Acceptance

Status: acceptance owner for the original six milestones and the GA Closure
line after `v0.5.0-rc.1`.

## Hard invariants

These requirements are never traded for latency or retrieval quality:

| ID | Invariant | First executable proof |
| --- | --- | --- |
| `INV-DUR-001` | An acknowledged receipt has committed to the owning service's durable authority | M1 process crash/restart |
| `INV-IDEM-001` | Retrying one effect identity cannot create a second receipt | M0 semantic; M1 durable retry |
| `INV-AUTH-001` | Unauthorized Spaces are excluded before candidate generation | M0 semantic |
| `INV-LEAK-001` | Private evidence never appears in a shared result or sink | M0 service; M2 output sink |
| `INV-RYW-001` | A successful Remember is visible to the next authorized Recall | M0 semantic; M1 restart |
| `INV-RPL-001` | Session Replay performs zero Memory calls and reproduces stored model-visible bytes | M2 |
| `INV-PROV-001` | Every fragment and derived record has valid same-Space evidence | M0 receipt; M4 derived record |
| `INV-OWN-001` | Caelis never becomes a receipt, record, index, or Steward authority | Architecture review from M0 |

## Golden Path cases

| ID | Case | First required |
| --- | --- | --- |
| `GP-001` | Bootstrap Realm, shared Space, two identities, and private Spaces | M0 reference |
| `GP-002` | Bot-A private Remember and immediate Recall | M0 reference |
| `GP-003` | Bot-B cannot access Bot-A private receipt | M0 reference |
| `GP-004` | Shared Remember is visible to Bot-A and Bot-B | M0 reference |
| `GP-005` | Receipt and lexical Recall survive `memoryd` restart | M1 |
| `GP-006` | Caelis persists hidden consistency metadata and reconstructs it after restart | M2 |
| `GP-007` | Offline Session Replay is byte-identical and makes zero Memory calls | M2 |
| `GP-008` | Feature disable removes tools and retains appliance data | M2 |

## M0 contract matrix

| ID | Behavior |
| --- | --- |
| `C-001` | Remember model schema exposes only `text` |
| `C-002` | Recall model schema exposes only `query` |
| `C-003` | Remember projection is exactly `{"accepted":true}` |
| `C-004` | Recall projection contains only ordered fragment strings |
| `C-005` | Empty Recall succeeds with an empty fragment list |
| `C-006` | Unavailable is a typed error and cannot become C-005 |
| `C-007` | SourceContext and RecallBudget bounds fail explicitly |
| `C-008` | ReceiptStatus state is separate from immutable receipt payload |
| `C-009` | `snapshot_id` is absent from v1alpha1 |

| ID | Authorization behavior |
| --- | --- |
| `A-001` | Missing or unknown capability is rejected |
| `A-002` | Wrong actor is rejected |
| `A-003` | Wrong Runtime audience is rejected |
| `A-004` | Wrong operation is rejected |
| `A-005` | Expired or revoked capability is rejected |
| `A-006` | Shared View candidate generation never reads a private Space |
| `A-007` | View, Grant, Session, actor, and Space references are not credentials |

| ID | Receipt and Recall behavior |
| --- | --- |
| `R-001` | Same Space/key/digest returns the original receipt |
| `R-002` | Same effect identity with changed digest conflicts |
| `R-003` | Same text with different keys preserves distinct receipts |
| `R-004` | Capability renewal does not change effect identity |
| `R-005` | Read-your-writes does not require Steward |
| `R-006` | Every old receipt remains eligible for baseline Recall |
| `R-007` | Consistency token from an unauthorized Space is rejected |
| `R-008` | Stale storage generation is distinct from unauthorized |
| `R-009` | Fragment count and byte budgets are both enforced |
| `R-010` | Same idempotency key in another Space is isolated and does not reveal prior use |

M0 is a semantic suite over an intentionally volatile reference service. It
does not satisfy `INV-DUR-001`; only M1 may call the combined semantic plus
cross-process suite durable Core conformance.

## M1 durable Core matrix

| ID | Behavior |
| --- | --- |
| `D-001` | A separate `memoryd` process is killed immediately after accepted Remember; restart Recalls the receipt |
| `D-002` | Retrying the same effect after crash returns the original receipt |
| `D-003` | Restart preserves capability, View, Space, idempotency, cursor, and receipt state |
| `D-004` | Every Space has an independent FTS projection entered only after View authorization |
| `D-005` | Deleted FTS state rebuilds solely from immutable receipts |
| `D-006` | Receipt payload updates are rejected while processing state remains separate |
| `D-007` | Owner lock, SQLite write lock, pre-commit failure, and migration failure are explicit |
| `D-008` | Post-commit response failure is `unknown_outcome`; exact retry resolves one receipt |
| `D-009` | Health, readiness, graceful shutdown cleanup, crash restart, and stale Socket replacement are exercised |
| `D-010` | Management, issuer, and Runtime bearers remain distinct; lost issuer delivery is recoverable by repeatable rotation |

`go test ./...` runs the semantic suite and the real-storage separate-process
harness. `make durable` reruns the cross-process proof without cached results.

## M2 End-to-End Alpha matrix

| ID | Behavior |
| --- | --- |
| `I-001` | The host rejects a transport, API, Core Profile, service version, or build revision mismatch before adding Memory tools |
| `I-002` | A native sidecar executable is verified against a pinned platform and SHA-256 manifest before launch |
| `I-003` | Issuer credentials remain outside request bodies, management, Runtime authority, Session history, and ordinary diagnostics |
| `I-004` | One immutable Runtime binding renews a fresh bounded capability without changing Remember effect identity |
| `I-005` | Model schemas expose exactly `remember(text)` and `recall(query)` while all authority and request controls remain hidden |
| `I-006` | Restarted Caelis and `memoryd` preserve the hidden consistency cursor and read-your-writes |
| `I-007` | Offline Session Replay reproduces exact model-visible bytes and records zero Memory API calls |
| `I-008` | Feature disable or kill switch removes both tools without deleting or rewriting appliance data |
| `I-009` | Private/shared audience admission fails before a result reaches an incompatible model or output sink |
| `I-010` | Sidecar start failure, service loss, version mismatch, and unknown Remember outcome remain distinguishable |

## M3 Governance and Production Safety matrix

| ID | Behavior |
| --- | --- |
| `G-001` | Management bearer, issuer credential, and Runtime capability remain non-interchangeable on every management endpoint |
| `G-002` | Owner search and trace resolve a Recall evidence reference without exposing bearer values |
| `G-003` | Correction appends same-Space replacement evidence, never updates the original payload, and exact retry returns one replacement |
| `G-004` | Corrected originals remain auditable but do not reappear in baseline Recall after restart or FTS rebuild |
| `G-005` | Delete removes receipt text and leaves only a content-free audit tombstone; an old Remember effect cannot resurrect it |
| `G-006` | Deleted content remains absent after restart, FTS rebuild, and repeated deletion requests |
| `G-007` | Every delete response and operator guide identifies Caelis Session history as a separate erasure scope |
| `G-008` | Backup is confidential and integrity checked; restore rotates storage generation and stale cursors fail explicitly |
| `G-009` | Corrupted backup, partial migration, and failed upgrade preserve the last acknowledged usable generation |
| `G-010` | Diagnostics bound capacity, storage, receipt, projection, capability, restore, and rollback state without receipt text or credentials |
| `G-011` | Management credential rotation revokes the old bearer immediately and across restart; either interrupted rename outcome recovers deterministically |
| `G-012` | Offline upgrade preparation captures the exact stopped generation, gates all acknowledgements until commit, and permits old-version rollback without acknowledged loss |
| `G-013` | The RC1 `darwin/arm64` sidecar binds exact version, revision, platform, and SHA-256 and is explicitly labeled as a candidate rather than the formal GA matrix |

## M4 Semantic Steward Extension matrix

| ID | Behavior |
| --- | --- |
| `S-001` | Worker output accepts only `ADD`, `MERGE`, `SUPERSEDE`, or `IGNORE`; malformed, oversized, duplicate-evidence, and unsupported proposals mutate nothing |
| `S-002` | A proposal contains no Space, Job, profile, ACL, publication, tombstone, or physical-deletion authority; the appliance generates new Record IDs |
| `S-003` | Every applied Revision cites its Job receipt and only existing Evidence in that same Space |
| `S-004` | Record heads are mutable only through application logic; Revision and Evidence rows reject update and delete |
| `S-005` | `MERGE` retains current Evidence and all target operations require the exact current Revision; stale proposals conflict without mutation |
| `S-006` | Job completion, semantic mutation, projection, and receipt status commit together; an unknown response retry returns exactly one effect |
| `S-007` | Correcting or deleting cited evidence invalidates the active Record and removes its projection without rewriting immutable history |
| `S-008` | A prompt-policy profile version is immutable and captured by each Job; profile replacement changes only later Jobs and contains no provider, model, endpoint, or credential configuration |
| `S-009` | Concurrent Workers cannot own one Job; lease expiry, Worker crash, retry, and terminal failure are appliance-owned, durable, and bounded |
| `S-010` | Worker timeout, Worker absence, disabled work, and poisoned output leave baseline receipt Recall available and observably degraded |
| `S-011` | Recall enters only authorized per-Space semantic indexes, merges deterministically, deduplicates presentation, and retains complete receipt provenance |
| `S-012` | Disabling Steward stops new semantic work without deleting receipts, Records, Revisions, Evidence, or baseline projections |
| `S-013` | Worker claim/apply/fail uses a credential distinct from management, issuer, and Runtime capabilities; each other bearer fails closed on Worker routes |
| `S-014` | The model-facing `Generator` receives only a bounded WorkRequest; Space, Job, lease, bearer, actor, audience, View, Grant, SourceContext, provider, and model configuration are absent |
| `S-015` | `memoryd` starts and passes receipt Recall with no Worker and has no provider endpoint, provider credential, model name, or outbound model transport configuration |

## M5 and GA Closure verification matrix

| ID | Behavior |
| --- | --- |
| `V-001` | One clean exact commit passes documentation, format, whitespace, full test, vet, build, durable separate-process, and full race gates |
| `V-002` | Portable Core gates pass and the complete candidate lifecycle gate runs natively on `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, and `windows/arm64` |
| `V-003` | The frozen 200-case privacy-safe Chinese/mixed receipt/semantic corpus records Recall@k, precision, stale-current, abstention, and unsupported-claim metrics with complete provenance and zero private/shared leakage |
| `V-004` | Remember, Recall, startup, reindex, backup, restore, and backlog measurements remain within the frozen same-hardware RC budgets |
| `V-005` | The minimum supported schema-2 generation migrates transactionally with acknowledged receipt preservation and rollback remains lossless |
| `V-006` | Packaged `memoryd` and public operator `memoryctl`, the manifest, and verified detached checksums bind exact service version, commit, platform, protocols, filename, and SHA-256 for all six platforms |
| `V-007` | API module, SDK, service artifact, Caelis integration, and package consumers pin the same reviewed compatibility identity |
| `V-008` | Storage, corruption, backup, Worker, capability, Session-copy, upgrade, and release incidents have an accountable owner and safe first response |
| `V-009` | Publication occurs only after explicit release authority and required exact-SHA CI succeeds |
| `V-010` | A clean external consumer verifies the public artifact and passes Golden Path, crash/restart, disable, and zero-call Replay smoke |
| `V-011` | `make ga-soak` creates 100 Spaces, 100,000 receipts, and 10,000 semantic Record heads and verifies restart, every receipt status, per-Space Recall provenance and isolation, reindex, backup/restore, then repeats those data-plane probes against the restored generation with bounded aggregate reporting |
| `V-012` | Local source import sanitizes selected MEMORY text or Caelis Session JSONL into an aggregate report without committing or uploading raw private source text |
| `V-013` | Darwin/Linux Unix sockets and Windows named pipes expose the same application routes, handshake, authorization failures, and shutdown semantics |
| `V-014` | An external review maps findings to acceptance IDs and blocks the GA tag until every finding is resolved or recorded as an explicitly accepted risk |

`V-001` through `V-008` plus `V-011` through `V-013` produce a validated
candidate. `V-009`, `V-010`, and `V-014` are mandatory to call that candidate a
completed GA release.

## Failure injection by milestone

M1 adds process kill after commit, database lock, disk-full simulation,
transactional migration failure, FTS deletion/rebuild, concurrent revocation,
and shutdown races. M2 adds sidecar start failure, version mismatch, Caelis restart,
service loss during Recall, unknown Remember outcome, Session round trip, and
zero-call Replay instrumentation. M3 adds restore generation changes, corrupted
backup, partial migration rollback, delete/reindex/restart, and credential
revocation. M4 adds malformed proposals, invalid evidence, stale revision,
Worker timeout, Worker crash after model completion, wrong-bearer calls, and
duplicate job claim. GA Closure adds sidecar orphan attachment, multi-process
binding, named-pipe lifecycle, six native platform runs, and 100,000-receipt
soak recovery.

## Empirical budgets

Performance budgets are baselined on M1 reference hardware and frozen before M5.
The report records hardware, OS, dataset, concurrency, warmup, percentiles, disk
growth, and raw result artifact. Required measures are Remember p50/p95/p99,
Recall p50/p95/p99, startup/readiness, reindex throughput, backup/restore time,
and backlog recovery.

Retrieval quality grows by milestone:

| Milestone | Fixed corpus |
| --- | ---: |
| M0 | 15-20 protocol and security cases |
| M1 | 30-50 receipt and lexical cases |
| M4 | 80-100 semantic cases |
| M5 | At least 200 release cases |

One release-candidate run cannot change the model, prompt, ranking policy, or
evaluation corpus. Quality reports include Recall@k, precision, stale-current
rate, unsupported-claim count, abstention, and private/shared leakage.

## Evidence rules

A check proves only the exact source revision and artifact digest it executed.
Native-platform evidence is required when lifecycle or packaging differs by OS.
Cross-compilation is not native evidence. Failures classified as environmental
include the original error and an unchanged rerun in an appropriate environment.
