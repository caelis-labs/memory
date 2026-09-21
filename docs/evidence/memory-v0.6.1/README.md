# v0.6.1 validation record

Scope: immutable built-in Steward policy versioning, governance cancellation
status, and upgrades from published source. No production data or live model
provider is used. The review baseline is v0.6.0 at
`f17b0293597dcdf9fad4fcba9ea19e20d1d91674`.

## Frozen inputs

- Public upgrade fixtures: `appliance/testdata/upgrades/v0.5.2` and `v0.6.0`.
  Each manifest identifies its original release commit and exact file hashes.
- Historical cancellation fixture:
  `internal/appliance/testdata/v0.6.0-governance`. Its original v0.6.0 generator
  asserts the inconsistent public statuses before checkpointing the database.
- Policy snapshots: `sdk/go/memory/stewardworker/testdata/released_profiles`.
  v0.5.2 and v0.6.0 specifications are retained independently of current code.

The generator sources are checked in next to the fixtures. All credential files
and receipt text are synthetic test inputs. The upgrade tests copy them into
temporary directories and never alter the frozen images.

## Candidate checks

Executed on Darwin arm64 (Apple M4), Go 1.26.8: `make release-candidate`
passed, including full tests, race, vet/build, durability, corpus, Facts, external
consumer upgrades, and existing M5 benchmarks. The full aggregate output is
retained in `release-candidate.log.txt`. Pure-Go embedded tests and the expanded
Windows test selection also passed locally.

The initial native Windows CI run caught CRLF conversion of synthetic token
files. `.gitattributes` now fixes LF for the hashed text inputs and disables
text conversion for the SQLite images. A local `core.autocrlf=true` checkout
preserves all nine manifest hashes; final native CI remains mandatory.

Commands for this patch:

```sh
make release-candidate
CGO_ENABLED=0 GOWORK=off go test ./appliance ./internal/appliance
make windows-regression
```

The Windows selection is also executed natively by protected GitHub CI; running
that selection on macOS alone is not Windows evidence. Linux and Darwin run the
full suites, with race checks. Published-module consumer verification runs after
the immutable release tag exists.

## Caelis integration reproduction

`caelis-upgrade_test.go.txt` is a test overlay for a disposable archive of Caelis
commit `f7c2b4bdd56f4bcfd78f9956ed246cb5e587e16d`. It uses the real Host,
Steward bridge, model callback adapter, and existing deterministic HTTP provider.

Copy it to `app/gatewayapp/memory_release_upgrade_test.go` in that archive. With
its original dependency on Memory v0.5.2, run:

```sh
GOWORK=off CGO_ENABLED=0 CAELIS_MEMORY_UPGRADE_DIR=/absolute/test-store \
  CAELIS_MEMORY_UPGRADE_MODE=seed \
  go test -count=1 ./app/gatewayapp -run '^TestMemoryReleasedStoreUpgrade$' -v
```

The seed explicitly binds Steward, waits for version 1 activation, drains the
worker without unbinding, and leaves one accepted pending receipt. Then select
the candidate Memory source with a temporary local replace (or the published
version without replace) in this disposable archive and run:

```sh
GOWORK=off CGO_ENABLED=0 CAELIS_MEMORY_UPGRADE_DIR=/absolute/test-store \
  CAELIS_MEMORY_UPGRADE_MODE=upgrade \
  go test -mod=mod -count=1 ./app/gatewayapp \
  -run '^TestMemory(ReleasedStoreUpgrade|EmbeddedGoldenPath|StewardBindingControlsEmbeddedWorker)$' -v
```

The upgrade must activate version 2, retain both profiles, process the old and
new receipts, and make exactly two Steward model callbacks. The other tests
cover fresh startup, default zero-model mode, workspace isolation, restart and
Session Replay, and explicit binding controls.

The recorded seed, upgrade, fresh Golden Path and binding-control runs passed
on Darwin arm64 with CGO disabled. Their aggregate outputs are retained in
`caelis-seed.log.txt` and `caelis-upgrade.log.txt`. The policy expectation change
also passed in the disposable archive (`caelis-policy.log.txt`).

Caelis's separate `TestMemoryStewardSemanticEvaluationFixtureAndPolicy` currently
hard-codes version 1. Its dependency-update PR must advance that expectation to
2; this Memory patch does not change the Caelis workspace or publish Caelis.
The candidate integration uses deterministic model responses and does not
qualify production model accuracy.

## Publication

The implementation PR must pass full protected CI. The release-please PR then
changes only VERSION, CHANGELOG, and the release manifest. Confirm its source
tree matches the fully tested implementation, and verify public Go Proxy `.info`,
`.mod`, `.zip` plus the external consumer pinned to v0.6.1 with no replace.
