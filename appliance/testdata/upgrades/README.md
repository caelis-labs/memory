# Released Steward upgrade fixtures

These closed SQLite databases were produced by the exact tagged source, not by
editing a current database to look old:

| Directory | Source commit | Schema |
| --- | --- | --- |
| v0.5.2 | 51693ff135be8c4149c15117980290aaad6d90da | 1 |
| v0.6.0 | f17b0293597dcdf9fad4fcba9ea19e20d1d91674 | 2 |

Each manifest freezes file hashes, the actual built-in profile, a custom profile,
an active binding, a leased job and a pending job, original receipt responses,
and a synthetic issuer credential. All credentials and text are test-only.
Tests copy the files into isolated temporary directories; never use this fixture
as a production store. The grant expires in 2099; tests issue fresh capabilities.

To regenerate, archive the corresponding source commit into an empty temporary
directory, copy `generate_test.go.txt` into its `internal/appliance` directory as
`generate_upgrade_test.go`, and run in that source directory:

```sh
GOWORK=off RELEASE_FIXTURE_OUTPUT=/absolute/empty/output \
  RELEASE_FIXTURE_TAG=v0.5.2 \
  RELEASE_FIXTURE_COMMIT=51693ff135be8c4149c15117980290aaad6d90da \
  go test ./internal/appliance -run '^TestGenerateReleasedStewardUpgradeFixture$' -count=1
```

Use the second row's tag and SHA for v0.6.0. Review any changed fixture hashes.
`appliance/upgrade_public_test.go` checks immutable profiles, queued work,
receipt identity, restart, and authorization through public Runtime/Runner APIs.
The external consumer gate compiles the same test outside this module.
