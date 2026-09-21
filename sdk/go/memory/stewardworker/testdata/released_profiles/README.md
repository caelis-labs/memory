# Immutable released policy specifications

These snapshots are release contracts independent of BuiltInProfile(). v0.5.2
and v0.6.0 accidentally shipped different content as memory-default@1; preserve
both historical specifications. v0.6.1 keeps the v0.6.0 prompt and assigns version
2. Its exact release commit is resolved by the immutable release tag.

Do not update a released snapshot when changing the current policy. Allocate a
new profile version and add a snapshot for the new release. The test compares
the complete current specification with the highest released policy version.
