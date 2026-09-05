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
make corpus-gate
```

The gate covers public API shape, embedded facade behavior, the current SQLite
schema baseline, durable Remember/Recall, authorization, governance, Steward
application, command buildability, formatting, documentation links, and
whitespace. GitHub `quality` additionally runs native Windows amd64 embedded
Open regression (`windows-regression`); Darwin/Linux `make check` and
Windows cross-compilation are not that evidence.

`corpus-gate` separately names the checked-in release baseline: 64
Chinese cases, 64 English cases, and 96 cases spanning Spanish, French, German,
Japanese, Korean, and Arabic. It writes four durable batches, restarts between
batches, and gates per-cohort Recall@1/5, zero-result count, provenance, and a
750ms user-perceived Recall p95 budget. The same run requires zero crossover
through Recall, ReceiptStatus, or consistency tokens for both same-Space
different-LabelSet and different-Space same-LabelSet adversarial records.

The corpus source files and thresholds are frozen by
`internal/appliance/testdata/release_corpus/manifest.json`. They contain only
authored, de-identified product-shaped facts; private source text is never a
release input or repository artifact. A package candidate additionally runs
the Caelis embedded Golden Path against the exact selected module revision.

Long-running corpus and soak evidence remains separate from ordinary per-change
tests. Run the fixed 100-Space, 100,000-receipt, 10,000-Record soak for a GA
candidate and retain the aggregate result beside the external review evidence.
The v0.5.0 records are [Architecture Review Resolution](evidence/memory-v0.5.0-architecture-review-2026-09-03.md)
and [GA Soak Evidence](evidence/memory-v0.5.0-ga-soak-2026-09-03.md).

The Corpus ledger, Leaf protocol, direct Item query, and optional projection
substrate are post-v0.5 milestones. They are not package candidate gates,
Caelis Golden Path requirements, or artifacts in the `v0.5.0` release. A
downstream Caelis Session-to-Leaf projector is one integration concern rather
than a Memory protocol or release artifact. Future hierarchy, summary, dense,
or graph projections remain independently disableable and cannot affect flat
Recall.

After a release-candidate commit is pushed, wait for the GitHub `quality`
workflow at that exact revision. Create an annotated prerelease tag only for the
approved revision, then create a GitHub prerelease containing source archives
and the reviewed release notes.

For GA, advance `VERSION` to `0.5.0` on the final reviewed commit, rerun the
package candidate gate plus `make ga-soak`, validate the exact Caelis consumer
revision, push the commit, and wait for remote `quality` success at that SHA.
Only then create the annotated `v0.5.0` tag and a non-prerelease GitHub source
release. A local commit, local tag, earlier RC result, or tag on another SHA is
not release authority. The Memory package publishes no standalone binaries in
this release line.

The current package version is `0.5.1`. `memory-v0.5.0` remains the first
published schema compatibility floor. The final
prerelease baseline `memory-development-baseline-1` has the same schema and is
promoted in place by changing only its metadata marker; accepted data must
survive that transition. Every other older development baseline remains
unsupported. Any post-GA Corpus or projection table is introduced only by an
explicit additive migration from this floor.

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
6. Verify Caelis uses the final ModelGenerator surface, the deprecated pre-GA
   Generator bridge is absent, and the supported RC-to-GA schema floor is exact.
7. Run the Caelis quality, architecture, race, documentation, build, and
   release-matrix gates selected by the integration change.
8. Verify Linux natively in the local OrbStack Rocky environment.
9. Complete external review and resolve or explicitly accept every finding
   before the Caelis GA tag.

The Caelis release procedure owns platform archives, installers, R2 mirroring,
GitHub Releases, npm packages, and public installation smoke. Memory adds no
files to those archives beyond the code already linked into the Caelis binary.

## Incidents

| Incident | Accountable owner | First response |
| --- | --- | --- |
| storage exhaustion or projection drift | Memory package owner | preserve receipts, inspect capacity, rebuild disposable projections |
| SQLite corruption or schema initialization failure | Memory package owner | stop Host startup, preserve files, verify backup, and follow the supported migration or recovery procedure |
| Steward Worker or model outage | Caelis integration owner | remove the Steward binding; retain static receipt Recall |
| capability or Space-boundary defect | Memory security owner | revoke affected Grant and block the candidate |
| Session Replay leak | Caelis product owner | preserve canonical history and block the candidate |
| post-v0.5 Corpus projection leak or stale artifact | producer and Memory owners | disable the affected projection, preserve producer source and committed Leaf history, invalidate derived generations, and rebuild after repair |
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
