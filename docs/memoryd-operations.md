# memoryd Operations

Status: current operator guide for the standalone durable Core and packaged
local sidecar.

`memoryd` runs without Caelis and without a model. It exposes
`memory.v1alpha1` over an
owner-only Unix Socket and keeps all authority under one owner-only data
directory. The local management protocol remains internal; the data plane,
compatibility handshake, issuer plane, SDK, and sidecar manifest are the
host-integration boundary.

## Build and start

```sh
GOWORK=off go build -o ./memoryd ./cmd/memoryd
GOWORK=off go build -o ./memoryctl ./cmd/memoryctl
./memoryd -data-dir /tmp/caelis-memory
```

For a host-managed native sidecar, build only from a clean exact revision:

```sh
make sidecar
```

The target emits `dist/memoryd-$GOOS-$GOARCH` and a neighboring JSON manifest.
The manifest binds service version, source revision, protocol, API, Core
Profile, platform, executable name, and SHA-256. A host verifies the manifest
against its pinned identity and verifies the executable bytes before launch;
after readiness it performs the compatibility handshake and compares the
reported service version and revision with that manifest. Packaging refuses a
dirty worktree or a `REVISION` different from checked-out `HEAD`.

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
memoryd.sock          owner-only local transport while active
management.token      owner-only management bearer
```

The directory is mode `0700`; the database, lock, Socket, and management token
are protected by that boundary, with the credential, database, lock, and Socket
also set to owner-only modes where applicable.

Check liveness and durable readiness independently:

```sh
./memoryctl -socket /tmp/caelis-memory/memoryd.sock health
./memoryctl -socket /tmp/caelis-memory/memoryd.sock ready
```

Inspect or require an exact packaged identity through the same handshake used
by a host:

```sh
./memoryctl -socket /tmp/caelis-memory/memoryd.sock compatibility \
  -service-version 0.2.0-alpha.1 \
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

## Inspect, rebuild, revoke, and stop

```sh
./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token inspect

./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token rebuild-fts

./memoryctl \
  -socket /tmp/caelis-memory/memoryd.sock \
  -management-credential /tmp/caelis-memory/management.token \
  revoke-grant -id grant-bot-a
```

Inspect reports schema version, storage generation, topology, and row counts;
it omits receipt text and all bearer values. Rebuild deletes only disposable
per-Space FTS state and repopulates it from immutable receipts. Grant revocation
invalidates all derived capabilities on their next call.

Send `SIGTERM` or interrupt `memoryd` for a bounded graceful drain. A crash may
leave the Socket node behind, but the process owner lock is released by the OS
and the next owner safely replaces that stale Socket. A non-Socket file is
never removed.
