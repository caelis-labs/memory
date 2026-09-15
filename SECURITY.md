# Security policy

## Supported versions

Security fixes target the latest published minor release of Memory. Use the
latest patch in that release line; older minor releases do not receive routine
backports. Published versions and fixes are listed in
[GitHub Releases](https://github.com/caelis-labs/memory/releases).

Memory is distributed as an embedded Go package. Hosts must update their module
dependency and rebuild to receive a fix. Review the
[migration instructions](docs/memory-v0.6-migration.md) before upgrading stored
data; an older binary cannot open a newer unsupported schema.

## Reporting a vulnerability

Use GitHub's private
[Report a vulnerability](https://github.com/caelis-labs/memory/security/advisories/new)
form. Private vulnerability reporting is enabled for this repository. Please
keep vulnerability details out of public issues, pull requests and discussions
until maintainers have coordinated disclosure with you.

Include, when available:

- The affected Memory version or commit, Go version, operating system and
  architecture.
- The public API entry point and relevant identity, Space, View, Grant or
  LabelSet setup, using synthetic identifiers.
- Minimal, deterministic reproduction steps or a test with synthetic data,
  the expected result and the actual result.
- The impact and the permissions an attacker would need.

Do not submit live credentials, private receipts, user histories, database
backups or other people's personal information. Maintainers will triage reports,
request further details as needed, and coordinate a fix and disclosure. Response
times depend on maintainer availability; no fixed response deadline is promised.

## Security boundaries

Memory owns authorization before retrieval, exact Space and LabelSet isolation,
trusted evidence admission, and validation of derived-state mutations. Model
output is a proposal and must not grant access, confirm facts, or authorize
deletion. Reports that demonstrate violations of these boundaries are welcome.

Managed forgetting invalidates future adoption and cleanses payloads in owned
state and supported projections. It cannot retract a context already sent to a
model, an already-running snapshot read, old backups, exports, or host-maintained
history and caches. It is not forensic media erasure. Opaque identifiers and
audit/control metadata may remain; hosts must not put sensitive payloads in
these fields. A read that starts after the forgetting barrier and exposes
forgotten payloads through a governed API is still a security concern.

The embedding host owns credential protection, access to database and backup
files, transport exposure, and reconciliation of its external copies before
restore or reuse. See the [governance contract](docs/memory-v0.6-governance.md)
and [release boundaries](docs/memory-v0.6-release.md#material-boundaries) for the
precise scope.
