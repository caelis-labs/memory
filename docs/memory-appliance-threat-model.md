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
| Global index reveals private existence | Per-Space search or proven authorization pre-filter |
| Error or metric exposes content | IDs, sizes, digests, and typed codes only |
| Memory delete is mistaken for global erasure | Explicitly disclose separate Caelis Session copy boundary |
| Restore reuses an invalid causal cursor | Change storage generation and return stale-token error |

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
exact replay metadata, and output-sink enforcement. M3 must add backup confidentiality, hardened
management authorization, deletion verification, and incident diagnostics. M4
must add provider data-egress policy, prompt-injection resistance, cost bounds,
and job poisoning controls.
