# memoryd Operations

Status: deferred standalone-distribution guide. These commands and transport
contracts are retained for future non-Go consumers; they are not part of the
current Caelis installation, release, startup, or GA acceptance path.

`memoryd` runs without Caelis and remains fully useful without a model. An
optional downstream process may claim Steward Jobs through the external Worker
SDK and inject its existing model stack. `memoryd` exposes `memory.v1alpha1`
over an owner-only Unix Socket in the current RC and keeps all authority under
one owner-only data
directory. The versioned owner-management plane is independent from the data
and issuer planes. Its search and trace output may contain receipt text and must
be handled as sensitive operator data.

## Build and start

```sh
GOWORK=off go build -o ./memoryd ./cmd/memoryd
GOWORK=off go build -o ./memoryctl ./cmd/memoryctl
./memoryd -data-dir /tmp/caelis-memory
```

`memoryd` has no model/provider flag, credential, or outbound model transport.
Starting no downstream Worker runs the durable receipt/FTS appliance with no
model egress or semantic background work.

For standalone-framework development, build only from a clean exact revision:

```sh
make sidecar-supported
```

The historical `v0.5.0-rc.1` preview covered macOS on Apple silicon only.
`make sidecar` may still create development artifacts, but no current Memory Go
package or Caelis acceptance claim is derived from those binaries.

The target emits `memoryd-$GOOS-$GOARCH` and
`memoryctl-$GOOS-$GOARCH`, neighboring JSON manifests, and detached `.sha256`
checksums that packaging immediately verifies. Each manifest binds release
version, source revision, protocol, API, Core Profile, platform, executable
name, and SHA-256. A host verifies the `memoryd` checksum and manifest against
its pinned identity before launch; after readiness it compares the handshake
build identity with that manifest. Packaging refuses a dirty worktree or a
`REVISION` different from checked-out `HEAD`.

`-data-dir` is required. On macOS, keep this path short enough for the platform
Unix Socket path limit. `memoryd` refuses a second owner, refuses to replace a
non-Socket node at the transport path, and prints only the Socket and management
credential file paths. It never prints either credential value.

The directory contains:

```text
memory.db             SQLite durable authority
memory.db-wal         SQLite write-ahead log while active
memory.db-shm         SQLite shared state while active
memoryd.lock          advisory single-owner lock
memoryd.sock           owner-only local transport while active on Darwin/Linux
management.token       owner-only management bearer
steward-worker.token   owner-only external Worker bearer
```

Windows derives an opaque `\\.\pipe\caelis-memory-*` name from the absolute data
directory and applies an owner-only pipe security descriptor; no Socket node is
created in the directory. The directory is mode `0700` on Unix; the database,
lock, Socket, and both token files
are protected by that boundary, with the credential, database, lock, and Socket
also set to owner-only modes where applicable.

Check liveness and durable readiness independently:

```sh
./memoryctl -data-dir /tmp/caelis-memory health
./memoryctl -data-dir /tmp/caelis-memory ready

# Unix-only compatibility form:
./memoryctl -socket /tmp/caelis-memory/memoryd.sock health
./memoryctl -socket /tmp/caelis-memory/memoryd.sock ready
```

`-data-dir` is the preferred cross-platform online selector. It derives a Unix
Domain Socket on Darwin/Linux and a named pipe on Windows. `-socket` remains a
Unix-only compatibility flag. Offline restore/rollback/upgrade commands keep
their command-local `-data-dir` because no service endpoint exists while the
owner lock is offline.

Inspect or require an exact packaged identity through the same handshake used
by a host:

```sh
./memoryctl -socket /tmp/caelis-memory/memoryd.sock compatibility \
  -service-version 0.5.0-rc.1 \
  -build-revision FULL_GIT_OBJECT_ID
```

A mismatch returns the stable `incompatible` error and does not fall back to a
different protocol or profile.

## Bootstrap

Bootstrap is an atomic management operation. The following compact example
creates one Bot private Space plus one shared Space. Production callers should
choose their own opaque references and bounded Grant expiration.

```json
{
  "realms": [{"id": "realm-default"}],
  "identities": [{"id": "identity-bot-a", "realm_id": "realm-default"}],
  "spaces": [
    {"id": "space-shared", "realm_id": "realm-default", "class": "shared"},
    {"id": "space-bot-a", "realm_id": "realm-default", "identity_id": "identity-bot-a", "class": "private"}
  ],
  "views": [
    {
      "id": "view-bot-a",
      "realm_id": "realm-default",
      "read_space_ids": ["space-shared", "space-bot-a"],
      "write_space_id": "space-bot-a",
      "max_disclosure_class": "private",
      "version": 1
    }
  ],
  "grants": [
    {
      "id": "grant-bot-a",
      "principal_ref": "principal:bot-a",
      "actor_ref": "actor-bot-a",
      "view_ref": "view-bot-a",
      "allowed_operations": ["remember", "recall", "receipt_status"],
      "allowed_audiences": ["private"],
      "expires_at": "2099-01-01T00:00:00Z",
      "version": 1
    }
  ],
  "issuer_principals": ["principal:bot-a"]
}
```

Save it as `bootstrap.json`, then reserve a new owner-only output file for the
issuer credential:

```sh
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  bootstrap -file bootstrap.json -issuer-output issuer.json
```

`memoryctl` creates secret outputs with mode `0600` and `O_EXCL`; it never
overwrites a credential file. Bootstrap returns a new issuer credential only
once, so retain `issuer.json` securely. If the Bootstrap response or local
credential write is lost after the topology commits, recover without rebuilding
the topology by rotating that principal into another new owner-only file:

```sh
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  rotate-issuer -principal 'principal:bot-a' -issuer-output issuer-recovered.json
```

Rotation invalidates the prior issuer credential. A lost rotation response is
recovered by rotating again; Runtime capabilities already issued from the
Grant remain governed by their own expiration and revocation state.

## Issue Runtime authority

Create an owner-only `issue.json` containing only binding references and
requested bounds:

```json
{
  "principal_ref": "principal:bot-a",
  "grant_ref": "grant-bot-a",
  "actor_ref": "actor-bot-a",
  "audience": "private",
  "operations": ["remember", "recall", "receipt_status"],
  "ttl_seconds": 1800
}
```

```sh
chmod 600 issue.json
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -issuer-credential issuer.json \
  issue -file issue.json -authorization-output bot-a.authorization.json
```

`memoryctl` accepts the Bootstrap credential map, a rotated issuer response, or
a raw credential file. The issuer secret travels only as a transport bearer;
it is absent from the request body and management authorization is neither
required nor accepted by the issuer plane. Repeating the same issue request
before capability expiry produces a fresh bearer, which is the renewal path for
an active Runtime. The authorization output contains the opaque capability,
actor, audience, and expiration. It is Runtime authority, not a model or
Session artifact.

## Remember and Recall

```sh
./memoryctl -socket /tmp/caelis-memory/memoryd.sock remember \
  -authorization bot-a.authorization.json \
  -text 'commit does not authorize push' \
  -idempotency-key golden-private-1

./memoryctl -socket /tmp/caelis-memory/memoryd.sock recall \
  -authorization bot-a.authorization.json \
  -query 'commit push'
```

A successful Remember returns only after the receipt, idempotency identity,
processing state, consistency cursor, and Space FTS projection commit in one
SQLite transaction. If the transport fails around that boundary, the Go local
client returns `unknown_outcome`; retry the exact request with the same
idempotency key.

## Optional Steward profiles and external Workers

Steward is an appliance-internal organization policy with externally injected
generation. The Agent surface remains only Remember and Recall. Enabling it
requires two separate owner decisions:

1. the management plane binds an immutable prompt-policy profile to selected
   Spaces for future receipts;
2. a downstream host starts a Worker using `sdk/go/memory/stewardworker` and its
   own provider, model, credentials, budgets, and egress policy.

The Worker authenticates with `steward-worker.token`. That bearer can call only
claim/apply/fail routes; it cannot call management, issuer, or Runtime routes.
Conversely those other bearers cannot claim work. To rotate it, stop `memoryd`,
replace the file atomically with a new owner-only random token, and restart.

Create an immutable profile request as `profile.json`:

```json
{
  "profile": {
    "profile_id": "default-steward",
    "version": 1,
    "system_prompt": "Organize the receipt using only supplied evidence.",
    "max_context_records": 16,
    "max_input_bytes": 262144,
    "max_output_bytes": 32768
  }
}
```

Bind it to future receipts in explicitly listed Spaces with `binding.json`:

```json
{
  "profile_id": "default-steward",
  "version": 1,
  "space_ids": ["space-bot-a"]
}
```

```sh
./memoryctl -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  put-steward-profile -file profile.json

./memoryctl -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  bind-steward-profile -file binding.json

./memoryctl -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  steward-configuration
```

Profile versions cannot be edited. Put a new version and move the binding;
already-created Jobs keep their captured version. Receipts accepted before the
first binding have no retroactive Job. Receipt status reports accepted,
processing, organized, or a bounded terminal code independently from baseline
Recall.

A downstream process implements only the callback:

```go
type ModelGenerator interface {
    Generate(context.Context, stewardworker.GenerationRequest) (stewardworker.GenerationResponse, error)
}
```

It then constructs `stewardworker.Runner` with a local Worker client, its
`ModelGenerator`, a whole-second lease within 1s..10m, and a 10ms..10s polling
interval. Memory renders the complete instructions, bounded JSON input,
optional native JSON Schema, and output budget before invoking the callback.
The callback returns untrusted text and its envelope mode; Memory extracts,
parses, and validates the proposal. It does not receive Space, Job, lease,
SourceContext, actor, audience, View, Grant, capability, management bearer,
Worker bearer, provider, model, endpoint, or provider credentials.

`memoryd` owns lease expiry, five-attempt retry ceilings, exponential retry
delay, proposal and evidence validation, revision conflict handling, and atomic
canonical application. A Worker must retry an `unknown_outcome` apply with the
identical lease and proposal; the SDK runner does this once before leaving
recovery to lease expiry. Model egress, provider retention, billing, and
jurisdiction remain explicit downstream decisions.

To stop semantic work without deleting any receipt, Record, Revision,
Evidence, or baseline projection, create `disable.json`:

```json
{"space_ids":["space-bot-a"]}
```

```sh
./memoryctl -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  disable-steward -file disable.json
```

This removes future-Job bindings and terminally cancels pending or leased Jobs.
Stopping either process during generation is safe: the durable lease expires
and a later Worker may reclaim the Job.

## Inspect, search, correct, delete, rebuild, revoke, and stop

```sh
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token inspect

./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  search -query 'commit push' -space space-bot-a

./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  trace-receipt -id receipt-RECEIPT_ID

./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  correct-receipt -id receipt-RECEIPT_ID \
  -text 'commit does not authorize push or tag' \
  -reason 'operator verified correction' \
  -idempotency-key correction-20260901-1

./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  delete-receipt -id receipt-REPLACEMENT_ID \
  -reason 'approved appliance erasure request' \
  -idempotency-key deletion-20260901-1

./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token rebuild-fts

./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  revoke-grant -id grant-bot-a
```

Inspect reports management protocol, schema and storage generations, pending
restore and rollback state, topology, filesystem and database capacity,
receipt and processing counts, projection drift and last rebuild, and bounded
capability counts. Steward diagnostics add profile/binding counts, durable Job
states, active/invalidated Record counts,
oldest outstanding work, and semantic projection health. They omit prompts,
receipt text, paths derived from content, and all bearer
values. Search returns active receipt content by default;
`-include-corrected` exposes shadowed originals for audit. Trace connects a
Recall evidence ID to active evidence, correction links, or a content-free
tombstone.

Correction appends same-Space replacement evidence and leaves the original
payload immutable. Deletion physically removes appliance receipt content, but
text already copied into a canonical Caelis Session remains in that separate
history until the Session is deleted or redacted. An exact management retry
uses the same idempotency key. Reusing a key with changed input conflicts; an
old Runtime Remember retry also conflicts after deletion, preventing content
resurrection.

Rebuild deletes only disposable per-Space FTS state and repopulates receipt
entries from remaining immutable receipts and semantic entries from active
current Revisions. It makes no provider call. Durable correction relations
continue to hide superseded originals, invalidated Records remain excluded, and
deleted receipts have no payload to rebuild. Grant revocation invalidates all
derived capabilities on their next call.

Rotate the root management bearer in place after suspected exposure or on the
operator's schedule:

```sh
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  rotate-management
```

The response contains only the fixed credential-file path. Read the new value
from that owner-only file; the old bearer is rejected immediately and after
restart. If the process stops during rotation, startup reconciles the current
or pending digest with whichever token file rename became durable.

## Export, backup, restore, and rollback

Plaintext export is intended for deliberate inspection or migration. It always
uses a new owner-only output file:

```sh
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  export -output /secure/memory-export.ndjson \
  -include-corrected -include-deleted
```

Create an encrypted consistent backup while `memoryd` is running:

```sh
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  backup -output /secure/memory.backup \
  -key-output /separate-location/memory.backup.key
```

Both paths are reserved with `O_EXCL` and mode `0600`. Store the key separately
from the ciphertext. The appliance database does not retain it, and a lost key
cannot be recovered.

Restore is offline. Stop `memoryd`, retain the current management token (or
provide a different owner token when restoring into a new empty directory),
then run:

```sh
./memoryctl \
  -management-credential /tmp/caelis-memory/management.token \
  restore -data-dir /tmp/caelis-memory \
  -backup /secure/memory.backup \
  -key /separate-location/memory.backup.key
```

The command authenticates every encrypted chunk, verifies and migrates the
snapshot, rotates storage generation, and installs it only after all checks
pass. When an old database existed, `memory.db.rollback` retains its consistent
pre-restore state. A corrupted or truncated backup leaves the current database
untouched.

Start `memoryd` after restore. It reports health and permits authenticated
`inspect`, `search`, and `trace-receipt`, but readiness and all Runtime data
calls remain unavailable while `restore_pending` is true. Validate the expected
generation and evidence without allowing any new receipt. Then either commit
online:

```sh
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  restore-commit
```

or stop `memoryd` and roll back offline:

```sh
./memoryctl \
  -management-credential /tmp/caelis-memory/management.token \
  restore-rollback -data-dir /tmp/caelis-memory
```

Commit deletes the rollback image and then enables readiness and the data
plane. Rollback reinstalls the pre-restore image and rotates generation again.
All consistency tokens from before either transition fail explicitly as stale.

## Upgrade, failed-upgrade rollback, and disable

Keep the old `memoryd` and `memoryctl` binaries until acceptance completes.
First stop `memoryd`; then use the old control binary to capture the exact
stopped state and erect the pending-generation write barrier:

```sh
./old-memoryctl \
  -management-credential /tmp/caelis-memory/management.token \
  prepare-upgrade -data-dir /tmp/caelis-memory
```

This operation refuses a running owner, verifies SQLite and management
authority, writes `memory.db.rollback`, and marks the live generation pending.
It is safe to repeat after an interrupted preparation. Start the new exact
digest-verified artifact. Health, inspect, search, trace, export, backup, FTS
repair, compatibility, and management-token recovery remain available, while
readiness, Runtime calls, capability issuance, topology changes, corrections,
deletions, issuer rotation, and Grant revocation remain unavailable.

Verify the new manifest and handshake, inspect schema/projection/capability
diagnostics, and exercise read-only evidence queries. To accept the upgrade:

```sh
./new-memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  restore-commit
```

If any check fails, stop the new service and use the old control binary so the
rollback image is interpreted by the pre-upgrade schema owner:

```sh
./old-memoryctl \
  -management-credential /tmp/caelis-memory/management.token \
  restore-rollback -data-dir /tmp/caelis-memory
```

Rollback rotates storage generation, so cached consistency cursors fail stale;
it does not lose an effect acknowledged before the old service stopped. Start
the old exact artifact and rerun readiness plus the Golden Path.

An independent future consumer may stop its standalone process without deleting
or rewriting the appliance directory. Caelis has no Memory sidecar, feature
kill switch, or separate process lifecycle; it embeds the Go package. Erasure
is always an explicit deletion workflow, never a side effect of stopping a
consumer or rolling back a binary.

Send `SIGTERM` or interrupt `memoryd` for a bounded graceful drain. A crash may
leave the Socket node behind, but the process owner lock is released by the OS
and the next owner safely replaces that stale Socket. A non-Socket file is
never removed.
