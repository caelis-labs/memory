# Memory Appliance Threat Model

Status: Core Profile security baseline.

## Protected assets

- private receipt text and source context;
- shared receipt integrity;
- Realm, Identity, Space, View, and Grant configuration;
- capabilities and endpoint credentials;
- consistency and idempotency semantics;
- Recall provenance and Caelis Session copies;
- backup, migration, and deletion state.

## Trust boundaries

```text
model output and Agent arguments       untrusted
SourceContext and external references  untrusted audit data
Caelis Control                         product authority for actor and audience
Runtime capability                     temporary bearer authority
Memory authorization layer             data access enforcement
Memory canonical store                 appliance persistence authority
Steward model output                    untrusted proposal
Caelis Session store                    separate replay authority
```

Management credentials and Runtime capabilities are separate. Possessing a View
ID, Grant ID, actor ID, Session ID, Space ID, consistency token, or receipt ID
does not grant access.

## Primary threats and controls

| Threat | Required control |
| --- | --- |
| Agent chooses another identity or Space | Agent schemas omit all authority fields; capability selects View |
| Shared query leaks private candidates | Authorize and partition before candidate generation |
| Private context flows to shared output | Caelis binds one audience and rejects incompatible sinks |
| Stolen or stale capability is reused | Short lifetime, server-side state, explicit revoke, fail closed |
| Grant reference is treated as credential | Authenticate the delegating principal separately before issuance |
| Replaced sidecar impersonates the pinned service | Verify native platform and executable SHA-256 before launch, then match handshake build identity |
| Capability enters durable history or logs | Keep it outside request body and redact authentication material |
| Issuer credential enters a request body | Carry it only as an issuer-plane bearer and reject management or Runtime bearers |
| SourceContext forges actor or Workspace authority | Treat every field as bounded untrusted audit metadata |
| Retry duplicates a receipt | Stable Space/key identity and request digest |
| Consistency token is used as authority | Reauthorize its Space on every Recall |
| Steward invents or widens facts | Same-Space evidence and deterministic proposal validation |
| Model deletes evidence | No model-accessible physical deletion operation |
| Worker/model receives appliance authority | Keep lease beside the model-facing request and omit Space, Job, capability, View, Grant, actor, audience, SourceContext, and all bearers |
| Worker credential is confused with root or Runtime authority | Use a distinct owner-local bearer accepted only by claim/apply/fail routes |
| Model-provider egress exposes private receipts | Keep all provider configuration and egress in the downstream Worker host and require an explicit operator deployment decision |
| Prompt injection grants mutation authority | Treat receipt and Record text as untrusted data; accept only the closed proposal vocabulary and revalidate canonical state |
| Worker outage or poisoned output blocks memory | Baseline receipt Recall is independent; durable leases, attempts, byte limits, panic containment, and terminal codes bound work |
| Global index reveals private existence | Per-Space search or proven authorization pre-filter |
| Error or metric exposes content | IDs, sizes, digests, and typed codes only |
| Memory delete is mistaken for global erasure | Explicitly disclose separate Caelis Session copy boundary |
| Restore reuses an invalid causal cursor | Change storage generation and return stale-token error |
| Backup file discloses raw receipts | Chunked authenticated encryption with a separate random owner-only key |
| Corrupt or partial backup replaces live data | Authenticate, integrity-check, migrate, and stage completely before atomic replacement |
| Rollback loses writes accepted after restore | Keep restored generation management-only until explicit commit removes rollback |
| Online backup leaves a pre-upgrade acknowledgement gap | Stop first, snapshot the exact database offline, and start the new binary behind the pending-generation write barrier |
| Stolen management bearer remains valid after response loss | Atomically replace the fixed owner-only token and recover current versus pending digests on restart |
| Cross-compiled preview is presented as supported | Keep an explicit native-evidence support matrix and reject unsupported release packaging |

## Core Profile information flow

A private Runtime may receive private and shared fragments and may emit only to
a private sink. A shared Runtime receives only shared fragments and emits only
to a shared sink. The Core Profile has no public audience, mixed Session, or
private-to-shared publication path.

Recall results are evidence below system policy, current user instruction, and
current task facts. Text resembling a command cannot grant tools, approvals,
sandbox access, or handoff authority.

## Residual risks by milestone

M0 had no durable process or network transport; it proved semantics only. M1
adds owner-only local credentials and Unix Socket, a `0700` data boundary,
single-owner locking, digest-backed opaque capabilities, transactional receipt
acceptance, and storage failure injection. The Memory-owned M2 boundary adds
sidecar artifact integrity, exact compatibility identity, and a distinct public
issuer plane. Caelis integration must still add in-process capability handling,
exact replay metadata, and output-sink enforcement. The M3 governance plane adds
versioned root management authorization, same-Space append-only correction,
idempotent deletion tombstones, and restart/reindex deletion verification.
The recoverability slice adds encrypted streaming backup, staged restore,
generation rotation, and a no-write pending state for lossless rollback. M3
also adds secret-free incident diagnostics, crash-recoverable management-token
rotation, stopped-generation upgrade preparation, and an initial
`darwin/arm64` RC boundary. The local owner account and its data directory remain
one trust domain; remote organization tenancy and hardware key custody are not
provided. M4 and GA Closure add structured prompt/data separation, a distinct
Worker bearer, profile byte and context budgets, durable bounded attempts, panic
containment, closed proposal validation, and job poisoning controls. A deployed
downstream Worker still sends receipt content to its selected model; endpoint
trust, provider retention, billing, credentials, and jurisdiction remain that
downstream operator's responsibility. Formal GA additionally requires native
security and lifecycle evidence on all six RoadMap platforms. GA Closure adds a
transport-neutral endpoint, Windows named pipe restricted to the current owner
SID, Windows byte-range owner locking, and portable filesystem diagnostics.
Cross-compilation of those paths does not close the residual risk: Windows file
ACL inheritance, named-pipe attach/restart, service-account ownership, and every
native upgrade/rollback path still require G5 execution evidence.
