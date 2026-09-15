# Memory v0.6.0 release verification

## Scope and identity

The user requested the latest external PR review be repaired and authorized a
formal Release. This verification covers the embedded Go source package. Model
quality and Caelis Facts product adoption remain separate unqualified goals, as
stated in the [release notes](../memory-v0.6-release.md).

- Reviewed PR: [#2](https://github.com/caelis-labs/memory/pull/2).
- External review baseline: `c5652ec4dd2164265e2d457061e00dab4cae3af1`.
- Repaired implementation checkpoint: [release source](memory-v0.6-release-source.json),
  178 files; SHA-256
  `a8001fb40d556b9608b1d028ef3bb6a78e9b9c02beb49a5160920b8b168e6463`.
- This checkpoint and its delivery snapshot predate the subsequent CI,
  release-please, security-policy and release-documentation changes. Those
  additions preserve the Go implementation, tests, fixtures and dependencies.
  Earlier candidate source/delivery manifests are also historical evidence;
  none of these whole-tree hashes identifies the later automation revision.

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

The final-source package candidate gate, minimum Go 1.25.8 / CGO=0 full suite,
and focused Caelis consumer tests passed. The [100k-receipt soak](memory-v0.6-release-soak.json)
also passed: 100 Spaces, 100,000 Receipts, 10,000 Records; zero private leaks
before/after restoration; 10,000 completed jobs and zero pending jobs in both
stores; both projections healthy. The full performance rerun was stopped at the user's explicit request after
engineering feasibility was accepted; it has no final aggregate result. Each
completed gate's exit code and raw log is retained in the JSON evidence.

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
source. The repaired source at `130b729e6a918d3ccc785f9ffadd8746d8aab5a9` then passed
[all nine quality jobs](https://github.com/caelis-labs/memory/actions/runs/34986041895),
including native Windows embedded Open. The final evidence commit
`88bcee5c7c12e2a016afd58b3ac39478fa2f4335` passed
[all nine PR jobs](https://github.com/caelis-labs/memory/actions/runs/34988916676)
before PR #2 merged as `cba337f44655225eb65cd4599ffc29841617e23c`; their source
trees are identical.

The subsequent release automation change adopts protected-PR qualification and
removes duplicate full `main` push checks. Its own workflow/script/security/docs
PR requires full checks. The bot's metadata-only release PR then checks version
consistency without rerunning native tests. Verify the resulting tag resolves
to that merged release PR, its implementation tree matches the fully tested
tree, GitHub state is formal/non-prerelease, and public source archives and the
Go module download work. See the
[current procedure](../memory-appliance-release.md#automated-source-releases).

## Performance rerun disposition

After asking whether the completed work demonstrates feasibility, the user
explicitly requested that the long-running measurement no longer delay delivery.
The primary agent terminated only this run's identified test process at about
1,198.5 seconds (20 minutes). `make` exited 2 following `signal: terminated`.
The termination log is preserved in the [gate record](memory-v0.6-release-gates.json).

The harness reached its 100k seed phase after the 1k/10k phases, but writes its
aggregate metrics only at the end. No aggregate report was produced, so **this
final-source run has no 33/33 result and no reported partial latency scores**.
This is an explicitly stopped validation, not a test assertion failure or a pass.

Engineering feasibility is supported by the final-source functional/regression,
race, durable, public-consumer, native-platform CI and complete 100k-receipt soak
results. The previous 177-file candidate's 33/33 performance result and earlier
32/33 overrun remain historical evidence, tied to their original source hashes.
All frozen performance limit bytes remain unchanged. Full final-source latency
qualification and statistical repeatability remain deferred; production model
and end-to-end Bot quality are also not claimed.
