# Memory v0.6 schema migration

M01 opens a v0.5.2 appliance database in place. The migration is a forward-only
compatibility step from schema ledger version `1` (`memory-v0.5.0`) to ledger
version `2` (`memory-v0.6.0`). A fresh v0.6 database records both ledger rows:
the original baseline and the v0.6 migration.

## Supported input and atomicity

The supported on-disk input is a database produced by the tagged v0.5.2 source
commit:

```
51693ff135be8c4149c15117980290aaad6d90da
```

Migration runs in one SQLite transaction. It adds the M01 fact columns and
indexes, creates the source/policy/change tables, copies and rebuilds the
semantic revision/evidence tables, applies the additive Steward and governance
schema, checks foreign keys, then publishes the v0.6 marker and ledger row.
SQLite DDL and data changes are not published independently. A conflict or
process interruption therefore leaves either the complete v0.5.2 image or the
complete v0.6 image. Reopening after a rollback retries the migration.

Do not edit a database while `Open` is migrating it. Take the normal owner
backup first; there is no supported downgrade from schema version `2` to the
v0.5.2 schema.

## Compatibility invariants

The migration preserves, byte-for-byte, the v0.5.2 values for:

- receipt IDs, text, source context, occurrence/receive timestamps, request
  digests, idempotency keys, consistency tokens and cursor rows;
- Space/View/Grant authority, issuer digests, capability rows, and their exact
  label sets and label-set digests; and
- semantic record/revision/evidence IDs, text, job references, timestamps,
  processing state, and same-Space evidence links.

The only legacy scheduling text rewritten by migration is the mutable
`steward_jobs.available_at` and `steward_jobs.lease_expires_at` pair.
`normalizeStewardSchedule` parses each value as an RFC3339 instant and re-emits
it in UTC with exactly nine fractional-second digits. The instant is unchanged,
including sub-millisecond values; fixed-width encoding makes SQL text ordering
chronological (for example, `.120Z` remains before `.123Z`). Immutable receipt,
semantic-record, and semantic-revision timestamps are not normalized. No new
schedule fields are introduced; the existing two columns are the sole mutable
schedule representation.

`semantic_records.subject` and `semantic_records.fact_key`, and
`semantic_revisions.fact_json`, are empty for a legacy semantic row. M01 must
not infer a confirmed fact, subject, key, onset, or other fact metadata from an
old generic semantic record. The old semantic row remains available through
ordinary v0.5.2 Recall, with its original evidence references. A v0.5.2 source
context is retained on its receipt; the new `evidence_sources` table starts
empty because no source identity can safely be inferred from that context.

The migration runs `PRAGMA foreign_key_check` before commit. Every retained
semantic evidence row must still join to its same-Space semantic revision,
record, and receipt. Authorization is checked using the migrated Grant and
capability rows; private label partitions are never widened during migration.

The frozen v0.5.2 schema guard accepts exactly one current ledger row. Once the
v0.6 row is committed, that guard rejects the upgraded database rather than
opening it as an older schema. The test
`TestUpgradedDatabaseIsRejectedByFrozenV052SchemaGuard` mirrors that published
v0.5.2 check without importing an old module at runtime.

## Reproducible fixture

`internal/appliance/testdata/v0.5.2/memory.db` is a bounded, closed, checkpointed
SQLite image generated from the commit above. Its manifest records the exact
SHA-256 and data references:

```
database SHA-256: 68e02a7031d05e7baa3d5271019911728c42a9ef3fbcba4658bf44500e9f7708
receipt:          receipt-42424242424242424242424242424242
record:           record-42424242424242424242424242424242
semantic evidence: receipt-42424242424242424242424242424242
grants:           grant:migration, grant:shared-reader
```

The fixture generator source is retained as the non-built
`generate_test.go.txt`. To regenerate it without a worktree or writes under
`.git`:

```sh
set -eu
commit=51693ff135be8c4149c15117980290aaad6d90da
tmp=$(mktemp -d /tmp/memory-v052-XXXXXX)
out=$(mktemp -d /tmp/memory-v052-fixture-XXXXXX)
git archive "$commit" | tar -x -C "$tmp"
cp internal/appliance/testdata/v0.5.2/generate_test.go.txt \
  "$tmp/internal/appliance/z_migration_fixture_generator_test.go"
(
  cd "$tmp"
  MIGRATION_FIXTURE_OUT="$out" \
    go test ./internal/appliance -run '^TestGenerateMigrationFixture$' -count=1
)
# The test has checkpointed and closed SQLite before this cleanup. Owner files
# and sidecars are deliberately not part of the checked-in fixture.
rm -f "$out/management.token" "$out/steward-worker.token" \
  "$out/memoryd.lock" "$out/fixture-generated.txt" \
  "$out/memory.db-wal" "$out/memory.db-shm"
sha256sum "$out/memory.db"
```

The generated owner credentials are all-`B` synthetic values used only by the
fixture generator. They are not real credentials. The checked-in
`management.token.txt` and `steward-worker.token.txt` files are explicitly
synthetic provenance inputs; migration tests copy them to the expected owner
paths only inside `t.TempDir`. No credential file from a real appliance is
included.

## v0.6.1 data repair

The patch keeps schema 2 and adds an atomic, once-only repair for receipt
processing rows left inconsistent by governance cancellation in v0.6.0. It also
versions the built-in Steward profile independently of the schema. See the
[patch recovery instructions](memory-v0.6.1-release.md) before upgrading or
resuming affected work.
