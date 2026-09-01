# Memory Appliance Release Procedure

Status: RC1 evidence owner plus the GA Closure release contract. A commit or
successful local run does not authorize a tag or public release.

## Release line

`0.5.0-rc.1` has native `darwin/arm64` candidate evidence; buildable artifacts
for other platforms remain previews. The next candidate must close the RoadMap
G0-G5 slices before external review. Formal GA supports exactly Darwin, Linux,
and Windows on both AMD64 and ARM64 and packages both `memoryd` and
operator-facing `memoryctl`. The minimum supported in-place upgrade source is
`0.3.0-alpha.1`/schema 2. Current startup migrations, stopped-generation
upgrade preparation, management-only verification, commit, and old-binary
rollback must all pass from that boundary.

The public data, management, and Steward protocols remain explicitly versioned
`v1alpha1` contracts. Product GA does not silently rename those packages or
widen the Core Profile.

## Exact candidate gate

Run only from a clean checked-out commit:

```sh
make release-candidate
```

For the RC1 support line this executes documentation links, format and
whitespace checks, all tests,
vet, command builds, the separate-process durable suite, the full race suite,
the fixed 200-case RC retrieval corpus, M5 performance measurements, and
supported native packaging. Packaging now emits both `memoryd` and `memoryctl`
with individual manifests and verified detached checksums. It refuses a dirty
tree or a revision other
than exact `HEAD`. The resulting manifest binds service version, source
revision, platform, protocols, executable name, and SHA-256; a detached
checksum is generated and verified before the bundle is accepted.

GitHub quality cross-builds both executables for the six GA targets and uploads
preview bundles containing both manifests and portable detached checksums. It
also runs the portable Core on Ubuntu and the current complete candidate gate
on an ARM64 `macos-15` runner. Workflow actions are pinned to exact source
commits. Cross-build jobs prove buildability only. A platform enters the public support
manifest only after its native endpoint, owner credential, lock, lifecycle,
upgrade, rollback, and Golden Path evidence passes on that platform.

Linux candidate execution uses the local OrbStack Rocky environment rather than
an Ubuntu cross-build. Record `uname`, Rocky release, architecture, and Go
version with the gate output. The current Rocky ARM64 instance is valid evidence
only for `linux/arm64`; `linux/amd64` requires a separate native Rocky AMD64
instance before it can enter the GA support manifest.

The hard functional thresholds are zero acknowledged receipt loss, duplicate
idempotent effects, unauthorized candidate access, private/shared leakage,
Replay Memory calls, provenance gaps, read-your-writes failures, and data loss
through upgrade, rollback, restore, or disable. The executable suites in the
[acceptance plan](memory-appliance-acceptance.md) own those assertions.

## Frozen M5 measurement baseline

The reference run used macOS 26.6.2 on Apple M4 ARM64, Go 1.25.8, local APFS,
and `-benchtime=50x`. Latencies are direct appliance operations with SQLite
WAL and `synchronous=FULL`; process transport is covered separately by the
durable system suite.

| Measure | Fixed dataset | Measured | RC regression budget |
| --- | ---: | ---: | ---: |
| Cold Remember p50/p95/p99 | first write in 50 fresh Stores | 364/543/648 us | p99 <= 25 ms |
| Warm Remember p50/p95/p99 | 50 durable writes after one warmup | 363/494/523 us | p99 <= 5 ms |
| Recall p50/p95/p99 | 200 receipts, 50 queries | 236/501/634 us | p99 <= 5 ms |
| Startup/readiness p50/p95/p99 | 50 fresh stores | 15.0/17.0/17.7 ms | p99 <= 150 ms |
| Receipt plus semantic reindex | 500 entries, 50 runs | 268,312 entries/s | >= 50,000 entries/s |
| Verified backup p50/p95/p99 | 200 receipts, 50 runs | 6.5/7.0/7.6 ms | p99 <= 100 ms |
| Verified restore p50/p95/p99 | 200 receipts, 50 runs | 25.8/27.0/29.2 ms | p99 <= 250 ms |
| Durable backlog recovery | 50 `IGNORE` Jobs | 3,174 jobs/s | >= 500 jobs/s |

The command prints raw benchmark and allocation data. Same-hardware regression
comparisons use the same hardware class, Go version, dataset, and command; the
absolute budgets also gate the slower supported GitHub Apple M1 Virtual runner.
Cold first-write behavior is measured across fresh Stores independently from
the warm write distribution. The cold Remember budget was frozen at 25ms after
two unchanged pre-split native runs exposed 13.2ms and 13.4ms cold outliers on
that runner; the warm budget remains 5ms. The startup budget was reset from
100ms to 150ms after an earlier native run measured a 115ms cold-start p99. A
budget miss blocks the candidate until classified and reviewed; changing a
budget, corpus, prompt, provider, or ranking policy starts a new RC run.

The original fixed RC retrieval corpus has 100 receipt and 100 semantic marker
queries. A separate reviewed Chinese/mixed corpus now exercises realistic facts
and semantic projections across three restart rounds. These deterministic tests
are necessary application checks but are not, by themselves, the G4 model
quality gate. GA additionally freezes at least 200 labeled realistic cases and
the 100-Space/100,000-receipt/10,000-Record soak described in the RoadMap.
Run the latter with `make ga-soak` and retain its aggregate JSON beside the
candidate evidence. It is intentionally separate from ordinary `make check`
and from the RC1 `release-candidate` target because it is a long-running GA
qualification rather than a per-change unit gate.

## Upgrade and rollback acceptance

Retain the exact old binary and management tool. With the old service stopped:

1. run old `memoryctl prepare-upgrade` against the exact stopped generation;
2. start the candidate and verify health, manifest/handshake identity,
   generation, schema, receipt and Steward projection diagnostics;
3. prove Runtime writes remain unavailable while pending;
4. either commit with candidate `restore-commit`, or stop it and use old
   `restore-rollback`;
5. verify every pre-stop acknowledged sentinel and that stale cursors fail
   explicitly.

The automated migration, restore, stopped-upgrade, and rollback tests are
necessary evidence. A native candidate still performs this operator smoke with
copied production-like data before publication.

## Incident ownership

| Incident | Accountable role | First response |
| --- | --- | --- |
| storage exhaustion or projection drift | Memory service owner | stop new intake if needed, inspect capacity, preserve receipts, rebuild disposable projections |
| SQLite corruption | Memory service owner | stop service, preserve files, verify backup, stage restore; never rewrite acknowledged evidence in place |
| backup or restore failure | Memory service owner | retain current/rollback generation and keys, do not commit pending restore |
| Worker outage or poisoned Jobs | Memory cognition owner | disable Space bindings or stop Workers, retain receipt Recall, inspect bounded Job codes |
| capability or issuer incident | Memory security owner | revoke Grant or rotate issuer/management credential; inspect Session-copy scope separately |
| Caelis output or Replay leak | Caelis product owner | disable Memory tools, retain appliance data, investigate canonical Session history |
| upgrade or release regression | Release manager | stop candidate, execute old-binary rollback, preserve exact SHA evidence |

No alert or remediation may log receipt text, prompts, response bodies, raw
bearers, or private Worker/model payloads.

## Authorized publication and post-release smoke

Publication requires a separate explicit authorization after the exact commit's
required GitHub jobs pass and an external reviewer has mapped every finding to
an acceptance ID. An unresolved finding or undocumented accepted risk blocks
the GA tag. The release manager then:

1. verifies `VERSION`, candidate commit, workflow commit, manifest revision, and
   binary SHA-256 are identical;
2. creates an annotated version tag at that exact commit and publishes only the
   verified native `memoryd`/`memoryctl` binaries, manifests, source, and
   checksums for the six supported platforms;
3. verifies a clean external consumer can download the public artifact, verify
   its manifest before launch, and match the runtime compatibility handshake;
4. runs the eight-step Golden Path, crash/restart, feature disable, and offline
   zero-call Replay smoke using the public artifact;
5. records artifact URLs, digests, CI runs, upgrade source, rollback result,
   corpus result, and benchmark output in the release evidence.

A missing public consumer smoke means the code is a validated candidate, not a
completed GA release.
