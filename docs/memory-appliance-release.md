# Memory Package Release Procedure

Status: Go module and Caelis integration release contract. A commit, local
validation, or Caelis dependency update does not authorize a tag or publication.

## Current product

`github.com/caelis-labs/memory` is currently delivered as a Go package. Caelis
pins one reviewed module revision and compiles the complete Memory runtime into
the Caelis Host binary. Users install, upgrade, and roll back Caelis as one
product; there is no downloaded Memory executable, runtime handshake, or second
platform release matrix.

`cmd/memoryd`, `cmd/memoryctl`, local transport, manifests, and packaging code
remain buildable scaffolding for a future standalone product. They are not
published or consumed by Caelis in the current release line.
`make standalone-preview` retains the historical native packaging checks
without adding them to the package candidate gate.

## Package candidate gate

A candidate revision must be clean and pass:

```sh
make check
make race
make durable
```

The gate covers public API shape, embedded facade behavior, SQLite migrations,
durable Remember/Recall, authorization, governance, Steward application,
realistic corpus checks, command buildability, formatting, documentation links,
and whitespace. A package candidate additionally runs the Caelis embedded
Golden Path against the exact selected module revision.

Long-running corpus and soak evidence remains separate from ordinary per-change
tests. Run it for a GA candidate with the RoadMap's frozen dataset and retain
the aggregate result beside the external review evidence.

## Version coordination

Caelis records the exact module version in `go.mod`. Compatibility is checked at
compile and test time, not negotiated on a user's machine. A Memory API change
and its Caelis consumer may be reviewed in separate repositories, but the Caelis
candidate is accepted only after its dependency revision and Golden Path are
both exact.

Local multi-repository development may use `go.work`. Published Caelis source
must not depend on a local `replace`, copied Memory source, or an uncommitted
workspace checkout.

## Caelis product acceptance

Before accepting the imported revision:

1. Build Caelis with the exact Memory module revision.
2. Start a fresh offline Caelis Store with no Memory configuration.
3. Verify automatic topology, `remember`, immediate `recall`, restart, and
   byte-identical Session Replay.
4. Verify unbound Steward mode makes zero model calls.
5. Bind the system-managed Memory Steward and verify a downstream Caelis model
   callback produces an appliance-validated semantic Record.
6. Run the Caelis quality, architecture, race, documentation, build, and
   release-matrix gates selected by the integration change.
7. Verify Linux natively in the local OrbStack Rocky environment.
8. Complete external review and resolve or explicitly accept every finding
   before the Caelis GA tag.

The Caelis release procedure owns platform archives, installers, R2 mirroring,
GitHub Releases, npm packages, and public installation smoke. Memory adds no
files to those archives beyond the code already linked into the Caelis binary.

## Incidents

| Incident | Accountable owner | First response |
| --- | --- | --- |
| storage exhaustion or projection drift | Memory package owner | preserve receipts, inspect capacity, rebuild disposable projections |
| SQLite corruption or migration failure | Memory package owner | stop Host startup, preserve files, verify backup or migration repair |
| Steward Worker or model outage | Caelis integration owner | remove the Steward binding; retain static receipt Recall |
| capability or Space-boundary defect | Memory security owner | revoke affected Grant and block the candidate |
| Session projection or Replay leak | Caelis product owner | preserve canonical history and block the candidate |
| imported revision regression | Caelis release manager | revert the module revision and rebuild one Caelis rollback release |

No diagnostic or remediation may log receipt text, prompts, response bodies,
raw credentials, or private Steward payloads.

## Deferred standalone publication

Standalone publication requires a separate future RoadMap and explicit
authorization. It must establish real external consumers, supported native
platforms, artifact sources, signatures/checksums, endpoint security,
installation ownership, update and rollback, compatibility policy, and public
consumer smoke.

The existence of packaging commands or cross-buildable binaries is not a
support or publication claim. A future standalone release must remain optional
and must never become a runtime dependency of the Caelis embedded path.
