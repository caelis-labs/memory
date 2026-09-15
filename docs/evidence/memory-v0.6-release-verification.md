# Memory v0.6.0 release verification

## Scope and identity

The user requested the latest external PR review be repaired and authorized a
formal Release. This verification covers the embedded Go source package. Model
quality and Caelis Facts product adoption remain separate unqualified goals, as
stated in the [release notes](../memory-v0.6-release.md).

- Reviewed PR: [#2](https://github.com/caelis-labs/memory/pull/2).
- External review baseline: `c5652ec4dd2164265e2d457061e00dab4cae3af1`.
- Final source manifest: [release source](memory-v0.6-release-source.json),
  178 files; SHA-256
  `a8001fb40d556b9608b1d028ef3bb6a78e9b9c02beb49a5160920b8b168e6463`.
- The earlier candidate source/delivery manifests and tests remain historical
  evidence; their source hashes must not be attributed to this final revision.

## Review closure

The [external review](memory-v0.6-pr2-external-review.txt) was read from the latest
conversation turn. Its linked test attachment was unavailable through the
conversation tool. Executable tests were independently written from the stated
failure paths, not presented as the unrun reviewer's own test artifact.

All three reported defects reproduced before repair: corrected onset produced a
missing current fact or wrong history interval; correction allowed a second
confirmed head for both equal/different text; ListRecords and TraceRecord timed
out with seven production-pool connections held. The independent agent then
identified crossing a previous onset and erasing a finite change's onset. Both
were fixed with transactional validation; the crossing case was also reproduced.

[Raw red/green logs and gate output](memory-v0.6-release-gates.json) retain the
reproduction and verification evidence. Six `TestPR2Review` tests now pass. They cover forward/backward/repeated onset
correction, current/history reads and reopen, atomic conflicting correction,
one remaining database connection, invalid/unknown/equal onsets, confirmation
and correction followed by another change, and finite-change non-revival.

Independent reviewer `/root/blocking_trajectory_audit` performed read-only
follow-up of the three production files and all six tests. Its final result:
both additional onset findings closed; no new high-confidence issue found.
The reviewer did not execute tests. Runtime verification is recorded by the
primary agent. The earlier 222 AI trajectory adjudications remain attributed to
their own reviewed fixtures and source; human reviewer count remains zero.

## Final-source gates

Results are being collected before publication. Do not infer completion from a
listed command. The final evidence update records each exit code and raw log.

```sh
make release-candidate
GOWORK=off CGO_ENABLED=0 GOTOOLCHAIN=go1.25.8 go test ./...
make ga-soak GA_SOAK_REPORT=/tmp/memory-v060-pr2-soak.json
FACTS_PERF_SIZES=1000,10000,100000 make facts-perf
```

The first broad gate launch encountered sandbox denial while opening the shared
Go compilation cache. It was rerun with cache access; that environment failure
is not a source failure. No test failure is discarded as a cache issue.

## Downstream consumer snapshot

Caelis `6485d47bd1ae3b34b97a547fa207ed17b0f5849b` was exported with `git archive`
into `/tmp/memory-v060-caelis-consumer`. A temporary `replace` selected the final
Memory source. The original Caelis checkout and dependency pin were not edited.

Focused tests cover the embedded Golden Path, Steward binding, startup
authority, memoryhost, memorybinding and memorytool. This establishes source
compatibility of the existing consumer; it does not integrate the new Facts
product paths or qualify all Caelis platforms. Public release consumption is
verified separately after publication without local `replace`.

## Remote publication checks

The prior PR head's quality run
[34981569898](https://github.com/caelis-labs/memory/actions/runs/34981569898)
passed all nine jobs, including native Windows. It does not qualify the repaired
source. The repaired PR and merged commit require fresh checks. Before tagging,
verify the `main` push workflow's exact commit, all jobs, peeled annotated tag,
formal non-prerelease GitHub state, source archives and public Go module download.
