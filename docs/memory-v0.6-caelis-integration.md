# Memory v0.6.0 Caelis integration

Status: read-only consumption audit and integration contract. This document records
what Caelis consumed at the audited revision and the ownership and acceptance
work needed for Memory v0.6.0. The local candidate now defines its exact public
types in the versioned Facts API; this audit does not define wire semantics.

## Audit status and boundary

| Tree | Audited reference | Status |
| --- | --- | --- |
| Caelis | `ef6c4697bdf5af18db141b7d4b1eff4f28c3bbc7` (`v0.56.0`) | Clean worktree at audit time |
| Memory | `51693ff135be8c4149c15117980290aaad6d90da` (`v0.5.2`) | Base for this audit; parent-owned modified and untracked state is present |

This audit changed only this document. Caelis and shared state were not
modified. A later read-only status check during Memory review repairs found
Caelis clean at `f6514aa09cd26e8853e4a6d2420125ac7cb77acd`; the integration map
below was not re-audited at that newer revision and must be refreshed before
product acceptance. The future integration points are `Runtime.Facts()`,
`Runtime.Evidence()`, and the Management governance exposure. Request, response,
capability and cursor names are owned by the versioned API contract and exercised
by the public-only consumer gate.

Memory remains independently runnable and free of Caelis product types. The
versioned packages under `api/memory/*` own public wire semantics. Caelis owns
source interpretation, Session history, admission policy, and the decision to
put a bounded result into model context. Memory owns immutable receipts,
partition authorization, evidence-backed derived state, consistency, backup,
restore, and owner governance.

## Current integration map

### Runtime and host boundary

- `app/gatewayapp/internal/memoryhost/host.go` opens an embedded
  `appliance.Runtime` and owns the host binding seam. `BoundClient` currently
  exposes only `Remember`, `Recall`, and `GetReceiptStatus`.
- The host issues or renews a capability for each operation, binding the actor,
  audience, and selected labels. `ValidateAuthority` is side-effect-free and
  validates the currently supported Remember, Recall, and receipt-status
  authorities before activation.
- `app/gatewayapp/memory_provision.go`, `memory_runtime.go`,
  `memory_labels.go`, and `memory_steward.go` provision one private
  Realm/Identity/Space/View/Grant/issuer binding. They currently construct only
  the Remember and Recall model tools. Source context contains actor, Session,
  workspace, and source type; it is untrusted audit metadata, not an authority
  selector.
- The mandatory workspace label is the SHA-256 digest of the canonical current
  working directory. An optional host label selector can append opaque labels.
  Memory treats these labels as an exact capability-bound partition and does not
  interpret them as product identity.
- Steward is optional and model-backed. It submits proposals through the
  existing governed Steward boundary; baseline Recall remains static and does
  not depend on the model.

### Binding, Session, and tool projection

- `app/gatewayapp/workspace_config_assembler.go` selects one opaque binding,
  preserves a pinned Session binding and label set, and validates the binding
  before Runtime activation.
- `control/memorybinding/binding.go` owns opaque binding selection. It records
  the Runtime actor, principal, issuer reference, audience, View, Grant, version,
  and labels in the binding snapshot.
- `control/memorybinding/session_state.go` owns the immutable Session authority
  pin and the Memory consistency cursor. A Session cannot silently switch its
  binding or labels while it is active.
- `control/memorytool/tools.go` owns the model-visible Remember/Recall
  projection, hidden source and budget metadata, hidden consistency-cursor
  preparation and advancement, idempotent Remember identity, and provenance
  metadata. Model arguments do not select a Space, View, Grant, actor,
  capability, credential, or label.
- The existing golden-path and recovery behavior persists the exact
  model-visible Memory ToolResult and can replay the stored bytes without
  calling Memory. Session history is therefore a separate host-owned boundary,
  not a Memory cache.

### Lifecycle and Memory owner plane

- `app/gatewayapp/stack.go` opens Memory synchronously, validates configured
  authorities, starts and stops Steward, and closes Memory only after
  quiescence.
- The Memory version consumed at the audited revision exposes `DataPlane()`,
  `Management()`, `Backup`, `CommitRestore`, and the Steward worker through
  `appliance/runtime.go`. `Management` is the
  owner plane; model tools and ordinary Runtime calls do not receive its
  authority.
- Memory-side authorization, provenance, governance, and generation behavior
  are owned by `internal/appliance/dataplane.go`,
  `internal/appliance/governance.go`, `internal/appliance/semantic_recall.go`,
  and `internal/appliance/store.go`. Public owner and restore contracts are
  under `api/memory/management/v1alpha1` and `appliance/runtime.go`.

Existing evidence includes:

- `app/gatewayapp/memory_golden_path_e2e_test.go`;
- `app/gatewayapp/internal/memoryhost/host_test.go`;
- `app/gatewayapp/memory_labels_test.go`;
- `app/gatewayapp/memory_shutdown_test.go`;
- `app/gatewayapp/session_runtime_registry_test.go`;
- `control/memorybinding/binding_test.go` and `session_state_test.go`;
- `control/memorytool/tools_test.go`; and
- `appliance/backup_public_test.go`, `restore_public_test.go`,
  `internal/appliance/governance_test.go`, and
  `internal/appliance/backup_restore_test.go`.

No host path at the audited revision admits a v0.6 facts subject/scope, calls
`Runtime.Facts()` or `Runtime.Evidence()`, or consumes governance visibility for
facts/evidence projections.

## v0.6 limitations, owners, and acceptance requirements

### 1. Stable host subject and scope mapping

**Current limitation.** The host has a stable actor, opaque binding, audience,
and workspace `LabelSet`, but it does not yet map those admission decisions to a
v0.6 facts subject and scope. Model arguments and `SourceContext` must not fill
that gap.

**Owning locations.** The mapping belongs in:

- `app/gatewayapp/internal/memoryhost/host.go`;
- `app/gatewayapp/memory_runtime.go`; and
- `app/gatewayapp/workspace_config_assembler.go`.

The mapping is a Caelis admission concern. Memory must receive an opaque,
already-admitted product mapping and must not learn how a Caelis identity,
Session, or workspace is named.

**Acceptance.**

- Runtime/Session admission fixes exactly one subject and scope before any
  `Runtime.Facts()` or `Runtime.Evidence()` operation.
- The pinned subject/scope cannot be changed by model arguments, source metadata,
  a later tool call, or a Session continuation.
- Every read and write path reuses that admission snapshot; no path can widen a
  Space, View, Grant, or exact label partition.
- A capability for one workspace, binding, or label set cannot read or write
  another. Tests cover cross-workspace, cross-label, private, and shared
  isolation, including attempted selector injection.
- Subject/scope and the credentials used to enforce them never appear in model
  tool schemas, model-facing results, or ordinary Session history.

### 2. Read-only delegation

**Current limitation.** `memoryhost.BoundClient`, `runtimeMemoryHost`, and
`Host.Bind` have no facts read seam. A read-only worker cannot yet request a
bounded current facts/background result through the Runtime.

**Owning locations.** Extend the seam in:

- `app/gatewayapp/internal/memoryhost/host.go`;
- the `runtimeMemoryHost` implementation and `Host.Bind` path;
- `control/memorybinding/session_state.go` for the hidden Session cursor; and
- `appliance.Runtime.Facts()` once the versioned public contract is final.

`Runtime.Evidence()` and the Management governance exposure are not part of a
read-only worker's authority.

**Acceptance.**

- Facts reads require a current capability carrying read authority for the
  exact admitted subject/scope; capability rejection happens before candidate
  generation or storage/index access.
- A read-only client cannot invoke Remember, `Runtime.Evidence()`, correction,
  denial, forget, restore, or any governance operation, directly or through a
  model tool.
- Candidate generation occurs only after authorization of the exact boundary;
  there is no private-Space query followed by host-side filtering.
- Results are bounded by the host budget and contain no capability, credential,
  Space/View/Grant, raw scope selector, or hidden cursor data.
- Focused host and tool tests cover rejected capabilities, authorization-before-
  candidate-generation, bounded results, and no leakage of subject, scope, or
  credentials.

### 3. Selected trusted source ingestion

**Current limitation.** The current `control/memorytool.Remember` path permits
model input to write through normal Remember. `SourceContext` is explicitly
untrusted audit metadata and cannot establish that a source was selected or
canonicalized by Caelis.

**Owning locations.** Trusted source admission belongs to Caelis
source-history/Control code, not to Memory source parsing:

- `agent-sdk/source_event.go`;
- `app/gatewayapp/guardian_projection.go`;
- `app/gatewayapp/guardian_window.go`;
- `app/gatewayapp/guardian_prompt.go`;
- `app/gatewayapp/control_client_backend.go`; and
- `app/gatewayapp/control_client_observer.go`.

The host should pass only owner-admitted records through `Runtime.Evidence()`
when the final contract is available. Memory validates the resulting evidence
and its attribution; it does not open, parse, sanitize, select, or reconstruct
Caelis source history.

**Acceptance.**

- Only selected canonical user, tool, and source records are admitted, with
  stable producer, event, revision, and fragment attribution.
- Unselected, malformed, forged, or otherwise untrusted source records are
  rejected before they can create evidence or derived facts. Untrusted
  `SourceContext` cannot upgrade a record.
- Repeating one effect identity is idempotent; changing its request under that
  identity conflicts and cannot create a second effect.
- Late, duplicated, cancelled, or out-of-order source results cannot overwrite
  a newer admitted revision or bypass the current binding, scope, or governance
  state.
- A private source cannot become shared through projection, model output,
  labels, subject/scope arguments, or a retry.
- Model output remains a proposal/evidence input only. It is never ingestion,
  persistence, deletion, or governance authority.
- Tests cover selected-source admission, rejected/untrusted sources, duplicate
  effect identity, late-result handling, attribution, and private-to-shared
  isolation.

### 4. Facts, current background, and cursor invalidation

**Current limitation.** There is no host integration for a bounded current
facts/background read. Existing Memory consistency state is for Remember/Recall
and is not yet bound to a v0.6 subject/scope contract.

**Owning locations.** The read and hidden-state integration spans:

- `app/gatewayapp/internal/memoryhost/host.go` and
  `app/gatewayapp/memory_runtime.go` for the Runtime adapter;
- `app/gatewayapp/workspace_config_assembler.go` and
  `control/memorybinding/session_state.go` for immutable admission and cursor
  persistence; and
- `control/memorytool/tools.go` for bounded model projection and cursor
  prepare/advance behavior.

Memory generation and governance enforcement remain owned by
`appliance.Runtime` and `internal/appliance`; Caelis must not reimplement or
reinterpret them.

**Acceptance.**

- `Runtime.Facts()` returns only a bounded current facts/background result for
  the admitted subject/scope and capability.
- Every fact and background item remains attributable to immutable receipt IDs;
  derived state never becomes independent evidence and never widens the
  authorized Space boundary.
- Memory cursors bind the authorized Space/LabelSet scope and storage generation.
  Caelis must additionally bind its cached facts/background to the exact subject,
  query/context, budget and host binding. These extra cache keys are not fields
  enforced by the Memory cursor. Keep cursors out of model input and ordinary
  history.
- Poll `Changes` with live authorization; correction, denial and forget events
  invalidate cached adoption and require a fresh facts/background read. Such
  writes do not automatically invalidate a same-scope incremental cursor.
  `ResetRequired`, restore generation changes or changed host admission require
  discarding the affected cache and synchronizing from a fresh snapshot.
- On Session restart, re-authorize and resynchronize before reusing background.
  Honor `RefreshAt` for time-driven changes even without a write. An already
  running snapshot read may predate a mutation; already-sent context cannot be
  retracted. Refresh before the next model call after observed invalidation.
- Tests cover receipt attribution, bounded output, cursor scope/generation
  binding, each invalidation trigger, Session restart, restore, and generation
  rotation.

### 5. Correction, denial, and forget

**Current limitation.** Memory owns governance, but Caelis has not yet consumed
that state for v0.6 facts/evidence projections. Existing receipt correction appends
replacement evidence and removes the original from baseline Recall. Deletion
leaves tombstone and anti-resurrection state; it intentionally does not erase
copies already held in Caelis Session history.

**Owning locations.** Memory behavior is owned by:

- `api/memory/management/v1alpha1`;
- `internal/appliance/governance.go`;
- `internal/appliance/semantic_recall.go`;
- `internal/appliance/store.go`; and
- the governance and restore tests under `internal/appliance` and `appliance`.

Caelis owner-only wiring belongs around `app/gatewayapp/stack.go`,
`app/gatewayapp/internal/memoryhost/host.go`,
`control/memorybinding/session_state.go`, and the Control/session projection
owners. Caelis consumes the Management governance exposure as invalidation or
rejection; it must not mirror Memory records or reinterpret Memory governance.

**Acceptance.**

- The trusted host must authorize structured fact edits and owner Management
  mutations. Only the owner path may invoke governance writes or management
  diagnostics; model tools and read-only workers receive no mutation authority.
  Capability-authorized `ReadFacts`, `FactHistory` and `Changes` can observe
  scoped results and invalidations without acquiring owner authority.
- Owner receipt correction invalidates dependent current facts and derived
  projections and admits replacement evidence within its authorized scope.
  Structured fact correction updates the selected fact lineage with an audit
  revision; it does not by itself delete the original Receipt.
- Fact denial prevents the denied assertion from current or historical adoption
  and from generated Facts background. Marked `FactHistory` audit remains
  available under authorization. Original Remember/Recall receipt evidence is
  not automatically deleted by fact denial; deletion is a separate owner action.
  Caelis must invalidate any previously cached adoption of the denied fact.
- Forget cleans selected managed receipt content and dependent derived payloads.
  Tombstones, source-suppression/effect digests, opaque identifiers and governance
  audit metadata remain, including owner reasons. Hosts must keep sensitive
  payloads out of retained identity/audit fields; this is not forensic erasure.
- Revalidate capabilities and synchronize changes before reuse. Grant revocation
  or generation changes reject affected authority; ordinary fact edits do not
  revoke every capability. Discard invalidated background and honor cursor reset
  results. Managed barriers survive retry, restart and rebuild; restoring an old
  backup additionally requires explicit deletion reconciliation.
- Host Session copies receive an explicit, separately owned redaction/deletion
  result; Memory governance does not silently claim to erase Session history.
- Tests cover owner authorization, correction, denial, forget, stale cursors,
  derived-fact invalidation, retries, and no resurrection.

### 6. Host history copies

**Current limitation.** The existing acceptance path persists exact
model-visible Memory ToolResults and replays stored bytes without contacting
Memory. Facts/evidence projections need a separate copy policy so that this
useful replay guarantee does not become an unauthorized Memory replica.

**Owning locations.** The current copy/replay boundary is evidenced by:

- `app/gatewayapp/memory_golden_path_e2e_test.go`;
- `app/gatewayapp/session_recovery_context_test.go`;
- `control/memorytool/tools.go`; and
- Session event/history owners in `agent-sdk/session/event.go`,
  `agent-sdk/session/event_validation.go`,
  `agent-sdk/session/file/event_log.go`, and
  `agent-sdk/session/file/store.go`.

The v0.6 copy policy is a Caelis Session/history decision. Memory remains the
authority for receipts, evidence, provenance, and invalidation state.

**Acceptance.**

- A host history copy preserves receipt references, selected source references,
  provenance, and invalidation state needed for audit, but does not copy raw
  Memory content that the Session is not authorized to retain.
- A historical ToolResult replay remains byte-exact and does not call Memory;
  however, an invalidated result cannot be treated as current facts/background
  or reintroduced into a new model context without the owner-defined handling.
- Correction and forget define and test separate Session redaction/deletion
  behavior. Memory deletion is not represented as a claim that all Session
  bytes were erased.
- Session history, Memory backup, and Memory restore remain separate
  authorities and failure domains. A history replay cannot bypass current
  Memory authorization or governance.
- Tests cover authorized and unauthorized copies, attribution preservation,
  exact replay, correction/forget handling, Session restart, and no stale
  context leakage.

### 7. Backup, restore, and governance exposure

**Current limitation.** `appliance.Runtime` already owns `DataPlane()`,
`Management()`, `Backup`, `CommitRestore`, and the Steward worker, but Caelis
has no v0.6 governance visibility for facts/evidence state. Restore and pending
restore gates must also govern new reads and writes without conflating Memory
state with Session history.

**Owning locations.** Memory snapshot, restore, and generation behavior is
owned by:

- `appliance/runtime.go`;
- `internal/appliance/store.go`;
- `internal/appliance/governance.go`;
- `appliance/backup_public_test.go`;
- `appliance/restore_public_test.go`; and
- `internal/appliance/backup_restore_test.go`.

Caelis startup, shutdown, and owner integration remain in
`app/gatewayapp/stack.go` and
`app/gatewayapp/internal/memoryhost/host.go`. Add v0.6 governance visibility
through the Management governance exposure, without exposing Management
authority to Runtime/model tools.

**Acceptance.**

- A consistent Memory backup includes facts, evidence, provenance, governance,
  tombstone, and cursor-generation state required to resume safely; the exact
  serialized public shape is owned by the versioned API contract.
- Restore preserves Memory-owned governance semantics, rotates storage
  generation as required, and invalidates old cursors and capabilities as
  appropriate.
- A pending restore blocks Runtime facts/evidence reads and writes as well as
  existing data-plane operations until the restore is committed or rolled back.
- Governance visibility is owner-authorized and read-only to Caelis consumers;
  it is never placed in model tool arguments, model context, or ordinary
  read-only worker authority.
- Host Session history is not part of the Memory snapshot. Backup/restore tests
  verify that the separate history policy cannot resurrect forgotten content or
  reuse a cursor from the prior generation.
- Tests cover consistent snapshots, pending-restore rejection, rollback,
  generation rotation, stale consistency tokens, facts/evidence state, and
  governance visibility.

## M07 external consumer gate

`GOWORK=off go run ./scripts/facts_consumer_gate` creates a system-temporary Go
module, replaces `github.com/caelis-labs/memory` with this Memory checkout, and
runs `go test ./...`. The generated test imports only public `appliance` and
`api/memory/*` packages; its temporary `go.mod`, `go.sum`, and harness are
removed on exit and never enter this repository.

The gate covers stable subject/Space/label isolation, read-only Remember and
trusted-evidence rejection, explicit evidence attribution, ReadFacts and
FactHistory background/cursors, Changes after correction/denial/deletion, and
owner Management `TraceRecord`, `SearchReceipts`, `DeleteReceipt`, and
`CleanupStatus`. It was last verified with:

```sh
GOWORK=off go run ./scripts/facts_consumer_gate
```

References: [M07 runner](../scripts/facts_consumer_gate/main.go), [public
Runtime topology example](../appliance/runtime_test.go), [public restore
example](../appliance/restore_public_test.go), and [facts contract](../api/memory/facts/v1alpha1/types.go).

## v0.6 completion gate

The integration is complete only when all of the following hold together:

1. Runtime/Session admission fixes one opaque subject/scope and one exact
   capability-bound partition before candidate generation.
2. `Runtime.Facts()` is a capability-read path with bounded, receipt-attributable
   output and hidden, generation-bound cursor state.
3. `Runtime.Evidence()` is reachable only from owner-admitted trusted source
   ingestion; model tools and read-only workers cannot use it as a write path.
4. The Management governance exposure is owner-only and drives invalidation or
   rejection rather than a Caelis mirror of Memory state.
5. Correction, denial, forget, restore, binding, label, and scope changes leave
   no reusable stale cursor, derived fact, cached background, or private-to-
   shared widening.
6. Session history preserves its explicit replay/copy contract and has tested
   handling for correction and forget independent of Memory backup/restore.
7. Memory remains independently buildable and free of Caelis product types, and
   the implementation is covered by focused boundary tests plus the repository's
   required race and diff checks when code changes land.
