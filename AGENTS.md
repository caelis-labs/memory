# Working agreement

- Keep Memory independently runnable and free of Caelis product types.
- The versioned packages under `api/memory/*` own public wire semantics.
- `sdk/go/memory` may depend on the API package; the API package never depends
  on the SDK, reference implementation, storage, or model integration.
- Treat receipts as immutable evidence. Derived state must remain attributable
  to receipt IDs and must never widen a Space boundary.
- Keep authorization ahead of candidate generation. Do not query a private
  Space and filter it out afterward.
- Model output is a proposal, never persistence or deletion authority.
- Add fields and mechanisms only when a shipped milestone needs them. Preserve
  compatibility through versioned APIs and explicit migrations.
- Format touched Go files, run focused tests, `go test -race ./...`, and
  `git diff --check` before commit.

