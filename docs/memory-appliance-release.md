# Memory Appliance Release Procedure

Status: M5 release-candidate and GA gate owner. A commit or successful local
run does not authorize a tag or public release.

## Release line

The first release-candidate line is `0.5.0-rc.1`. The supported native service
platform is `darwin/arm64`; buildable artifacts for other platforms remain
previews. The minimum supported in-place upgrade source is
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

This executes documentation links, format and whitespace checks, all tests,
vet, command builds, the separate-process durable suite, the full race suite,
the fixed 200-case RC retrieval corpus, M5 performance measurements, and
supported native packaging. Packaging refuses a dirty tree or a revision other
than exact `HEAD`. The resulting manifest binds service version, source
revision, platform, protocols, executable name, and SHA-256; a detached
checksum is generated and verified before the bundle is accepted.

GitHub quality runs the portable Core on Ubuntu and the complete candidate gate
on an ARM64 `macos-15` runner. Workflow actions are pinned to exact source
commits. Only the native job's binary and manifest are candidate artifacts.

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
| Remember p50/p95/p99 | 50 durable writes | 282/494/517 us | p99 <= 5 ms |
| Recall p50/p95/p99 | 200 receipts, 50 queries | 236/501/634 us | p99 <= 5 ms |
| Startup/readiness p50/p95/p99 | 50 fresh stores | 15.0/17.0/17.7 ms | p99 <= 150 ms |
| Receipt plus semantic reindex | 500 entries, 50 runs | 268,312 entries/s | >= 50,000 entries/s |
| Verified backup p50/p95/p99 | 200 receipts, 50 runs | 6.5/7.0/7.6 ms | p99 <= 100 ms |
| Verified restore p50/p95/p99 | 200 receipts, 50 runs | 25.8/27.0/29.2 ms | p99 <= 250 ms |
| Durable backlog recovery | 50 `IGNORE` Jobs | 3,174 jobs/s | >= 500 jobs/s |

The command prints raw benchmark and allocation data. Same-hardware regression
comparisons use the same hardware class, Go version, dataset, and command; the
absolute budgets also gate the slower supported GitHub Apple M1 Virtual runner.
The startup budget was reset from 100ms to 150ms after the first pre-RC native
run measured a 115ms cold-start p99 on that runner. A budget miss blocks the
candidate until classified and reviewed; changing a budget, corpus, prompt,
provider, or ranking policy starts a new RC run.

The fixed RC retrieval corpus has 100 receipt and 100 semantic queries. Every
case requires Recall@1, precision 1.0, current provenance, no unsupported
fragment, and no degradation. Security and governance suites separately cover
abstention/empty Recall, stale invalidation, and private/shared leakage.

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
| provider outage or poisoned Jobs | Memory cognition owner | disable Space bindings or Workers, retain receipt Recall, inspect bounded Job codes |
| capability or issuer incident | Memory security owner | revoke Grant or rotate issuer/management credential; inspect Session-copy scope separately |
| Caelis output or Replay leak | Caelis product owner | disable Memory tools, retain appliance data, investigate canonical Session history |
| upgrade or release regression | Release manager | stop candidate, execute old-binary rollback, preserve exact SHA evidence |

No alert or remediation may log receipt text, prompts, response bodies, raw
bearers, or private provider payloads.

## Authorized publication and post-release smoke

Publication requires a separate explicit authorization after the exact commit's
required GitHub jobs pass. The release manager then:

1. verifies `VERSION`, candidate commit, workflow commit, manifest revision, and
   binary SHA-256 are identical;
2. creates an annotated version tag at that exact commit and publishes only the
   verified native binary, manifest, source, and checksums;
3. verifies a clean external consumer can download the public artifact, verify
   its manifest before launch, and match the runtime compatibility handshake;
4. runs the eight-step Golden Path, crash/restart, feature disable, and offline
   zero-call Replay smoke using the public artifact;
5. records artifact URLs, digests, CI runs, upgrade source, rollback result,
   corpus result, and benchmark output in the release evidence.

A missing public consumer smoke means the code is a validated candidate, not a
completed GA release.
