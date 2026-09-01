# Memory Appliance Acceptance

Status: acceptance owner for the six-milestone roadmap.

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

## Failure injection by milestone

M1 adds process kill after commit, database lock, disk-full simulation,
migration interruption, FTS deletion/rebuild, concurrent revocation, and graceful
shutdown races. M2 adds sidecar start failure, version mismatch, Caelis restart,
service loss during Recall, unknown Remember outcome, Session round trip, and
zero-call Replay instrumentation. M3 adds restore generation changes, corrupted
backup, partial migration rollback, delete/reindex/restart, and credential
revocation. M4 adds malformed proposals, invalid evidence, stale revision,
provider timeout, worker crash after model completion, and duplicate job claim.

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
