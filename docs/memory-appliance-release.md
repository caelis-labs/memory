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

The v0.6.0 source-package release is tracked in the
[v0.6 release notes and checklist](memory-v0.6-release.md). Formal package publication was authorized after PR review repairs. Protected-PR
native CI qualifies implementation changes before publication. Production longitudinal quality and
Caelis Facts product integration are separately unqualified; a version-file edit
does not mean the release exists.

## Package candidate gate

A candidate revision must be clean and pass:

```sh
make check
make race
make durable
make corpus-gate
make facts-gate
GOWORK=off go run ./scripts/facts_consumer_gate
```

The facts gate has its own frozen longitudinal fixtures and admission/lifecycle/
governance assertions. Candidate-generated cases are labeled unreviewed and do
not themselves satisfy review. This candidate has completed the user-authorized
independent AI-agent substitute review, with human count 0 and per-case evidence.
Production model/holdout quality remains required for end-to-end Bot acceptance,
not claimed by this scoped package release. Follow the
[longitudinal evaluation procedure](memory-v0.6-evaluation.md) for controlled
comparison arms and 1k/10k/100k hot-partition baseline measurements.

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

## Automated source releases

`quality.yml` runs on pull requests targeting `main`. The branch rules require
the PR to be up to date and all nine existing quality contexts to pass before
merge, including native Windows and Darwin checks. It tests GitHub's combined
PR merge tree. A merge does not repeat the same full suite on `main`; instead,
the `release-please` workflow maintains a release PR from merged Conventional
Commits. Use `feat:` for new features, `fix:` for fixes, and explicitly review
breaking API or schema changes before accepting the proposed version.

Only nonempty changes wholly within `CHANGELOG.md`, `VERSION`, and
`.release-please-manifest.json` use the release metadata path. The classifier
checks regular files, a single stable version, increasing manifest versions,
nondecreasing `VERSION`, and a matching first changelog release heading. Source,
fixtures, build inputs, workflows, scripts, docs and unknown paths run the full
suite. Classification errors fail the required `portable-core` job. Native
tests and cross-build steps are skipped only when metadata validation succeeds;
the six matrix jobs still report each required context. Required job names and
branch protections are unchanged.

The bot uses the shared organization or repository `RELEASE_PLEASE_TOKEN`
Actions secret. Its permissions must allow reading repository history, opening
and updating release PRs, and creating tags and GitHub Releases. An empty token
fails explicitly. The default `GITHUB_TOKEN` is read-only; PR tests never receive
the release token. A personal access token also lets bot-generated PRs trigger
their required Actions checks. See the
[release-please action documentation](https://github.com/googleapis/release-please-action).

The `simple` strategy updates this repository's bare `VERSION` file, manifest
and Changelog. The manifest starts at the last published version `0.5.2`; the
bootstrap commit is its exact tag target. The prepared candidate's `VERSION`
is already `0.6.0`. Do not advance the manifest manually during bootstrap.
`always-update` keeps the release PR compatible with strict up-to-date rules.

To publish:

1. Merge reviewed implementation changes after full protected-PR checks. Retain
   the PR head, tested merge commit and source attribution for expensive gates.
2. Inspect the generated release PR's version and Changelog. For this release
   they must identify `0.6.0`. Its only changes should be the three metadata
   files above; any additional path requires full checks.
3. Merge the release PR only with publication authorization and successful
   required checks. The workflow does not automatically merge it. Merging this
   PR authorizes release-please to create `v<version>` and a formal GitHub source
   release. Do not create a competing manual tag.
4. Verify the tag resolves to the merged release PR commit, the release is
   neither draft nor prerelease, and the source archives are available. Check
   the implementation tree against its fully tested protected-PR tree; release
   metadata checks do not constitute another native runtime test.
5. Download the exact published module via the public Go Proxy and run the
   standalone Facts consumer without local `replace` or `go.work`. Reconcile
   the public module with the tagged source and publish verification links.

If automation fails, inspect its logs and existing PR/tag/release state before
using `workflow_dispatch` on `main` to retry. Do not move an existing version tag
or treat a version-file edit as publication. Memory publishes no standalone
binaries in this release line. Report security defects through the
[security policy](../SECURITY.md).

For this v0.6.0 publication, the user explicitly accepted engineering feasibility
and stopped the approximately 20-minute final performance rerun. Record that run
as stopped with no final aggregate result; do not transfer the previous source's
33/33 score to this release. This disposition does not change the frozen limits
or qualify production-model or end-to-end Bot performance.

The package version is `0.6.0` (previous released baseline: `0.5.2`).
`memory-v0.5.0` remains the first supported source schema floor. v0.6 writes
schema **2**, marker `memory-v0.6.0`, through an explicit atomic migration from
schema 1, including the final byte-identical prerelease baseline. Old binaries
reject the new ledger and must not be used for in-place writes. See
[migration and recovery](memory-v0.6-migration.md). Unknown legacy adoption and
onset remain unknown; `updated_at` is not an effective fact time. No Corpus or
projection platform is added by this migration.

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
