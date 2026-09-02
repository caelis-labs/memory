package appliance

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type migration struct {
	version    int
	statements []string
	apply      func(context.Context, *sql.Tx) error
}

var migrations = []migration{
	{
		version: 1,
		statements: []string{
			`CREATE TABLE metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		) STRICT`,
			`CREATE TABLE realms (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL
		) STRICT`,
			`CREATE TABLE identities (
			id TEXT PRIMARY KEY,
			realm_id TEXT NOT NULL REFERENCES realms(id),
			created_at TEXT NOT NULL
		) STRICT`,
			`CREATE TABLE spaces (
			id TEXT PRIMARY KEY,
			realm_id TEXT NOT NULL REFERENCES realms(id),
			identity_id TEXT REFERENCES identities(id),
			class TEXT NOT NULL CHECK (class IN ('private', 'shared')),
			created_at TEXT NOT NULL,
			CHECK ((class = 'private' AND identity_id IS NOT NULL) OR
			       (class = 'shared' AND identity_id IS NULL))
		) STRICT`,
			`CREATE TABLE views (
			id TEXT PRIMARY KEY,
			realm_id TEXT NOT NULL REFERENCES realms(id),
			write_space_id TEXT REFERENCES spaces(id),
			max_disclosure_class TEXT NOT NULL CHECK (max_disclosure_class IN ('private', 'shared')),
			recall_policy_ref TEXT NOT NULL,
			version INTEGER NOT NULL CHECK (version > 0),
			created_at TEXT NOT NULL
		) STRICT`,
			`CREATE TABLE view_read_spaces (
			view_id TEXT NOT NULL REFERENCES views(id) ON DELETE CASCADE,
			space_id TEXT NOT NULL REFERENCES spaces(id),
			ordinal INTEGER NOT NULL,
			PRIMARY KEY (view_id, space_id),
			UNIQUE (view_id, ordinal)
		) STRICT`,
			`CREATE TABLE grants (
			id TEXT PRIMARY KEY,
			principal_ref TEXT NOT NULL,
			actor_ref TEXT NOT NULL,
			view_id TEXT NOT NULL REFERENCES views(id),
			expires_at TEXT NOT NULL,
			revoked INTEGER NOT NULL CHECK (revoked IN (0, 1)),
			version INTEGER NOT NULL CHECK (version > 0),
			created_at TEXT NOT NULL
		) STRICT`,
			`CREATE TABLE grant_operations (
			grant_id TEXT NOT NULL REFERENCES grants(id) ON DELETE CASCADE,
			operation TEXT NOT NULL CHECK (operation IN ('remember', 'recall', 'receipt_status')),
			PRIMARY KEY (grant_id, operation)
		) STRICT`,
			`CREATE TABLE grant_audiences (
			grant_id TEXT NOT NULL REFERENCES grants(id) ON DELETE CASCADE,
			audience TEXT NOT NULL CHECK (audience IN ('private', 'shared')),
			PRIMARY KEY (grant_id, audience)
		) STRICT`,
			`CREATE TABLE issuer_credentials (
			principal_ref TEXT PRIMARY KEY,
			credential_digest BLOB NOT NULL,
			created_at TEXT NOT NULL
		) STRICT`,
			`CREATE TABLE capabilities (
			token_digest BLOB PRIMARY KEY,
			grant_id TEXT NOT NULL REFERENCES grants(id),
			principal_ref TEXT NOT NULL,
			view_version INTEGER NOT NULL,
			actor_ref TEXT NOT NULL,
			audience TEXT NOT NULL CHECK (audience IN ('private', 'shared')),
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		) STRICT`,
			`CREATE TABLE capability_operations (
			token_digest BLOB NOT NULL REFERENCES capabilities(token_digest) ON DELETE CASCADE,
			operation TEXT NOT NULL CHECK (operation IN ('remember', 'recall', 'receipt_status')),
			PRIMARY KEY (token_digest, operation)
		) STRICT`,
			`CREATE TABLE receipts (
			commit_sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			receipt_id TEXT NOT NULL UNIQUE,
			space_id TEXT NOT NULL REFERENCES spaces(id),
			text TEXT NOT NULL,
			source_context TEXT NOT NULL,
			occurred_at TEXT,
			received_at TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_digest TEXT NOT NULL,
			consistency_token TEXT NOT NULL UNIQUE,
			UNIQUE (space_id, idempotency_key)
		) STRICT`,
			`CREATE TABLE receipt_processing (
			receipt_id TEXT PRIMARY KEY REFERENCES receipts(receipt_id),
			state TEXT NOT NULL CHECK (state IN ('accepted', 'processing', 'organized', 'failed')),
			attempts INTEGER NOT NULL DEFAULT 0,
			last_attempt_at TEXT,
			terminal_error_code TEXT NOT NULL DEFAULT '',
			semantic_generation TEXT NOT NULL DEFAULT ''
		) STRICT`,
			`CREATE TABLE consistency_cursors (
			token TEXT PRIMARY KEY,
			generation TEXT NOT NULL,
			space_id TEXT NOT NULL REFERENCES spaces(id),
			commit_sequence INTEGER NOT NULL
		) STRICT`,
			`CREATE TABLE space_indexes (
			space_id TEXT PRIMARY KEY REFERENCES spaces(id),
			table_name TEXT NOT NULL UNIQUE
		) STRICT`,
			`CREATE TRIGGER receipts_immutable_update
		BEFORE UPDATE ON receipts BEGIN
			SELECT RAISE(ABORT, 'receipt payload is immutable');
		END`,
			`CREATE TRIGGER receipts_immutable_delete
		BEFORE DELETE ON receipts BEGIN
			SELECT RAISE(ABORT, 'receipt payload is immutable');
		END`,
			`CREATE INDEX receipts_space_sequence ON receipts(space_id, commit_sequence DESC)`,
			`CREATE INDEX view_read_spaces_space ON view_read_spaces(space_id)`,
		},
	},
	{
		version: 2,
		statements: []string{
			`CREATE TABLE receipt_tombstones (
				tombstone_id TEXT PRIMARY KEY,
				receipt_id TEXT NOT NULL UNIQUE,
				space_id TEXT NOT NULL REFERENCES spaces(id),
				idempotency_key TEXT NOT NULL,
				request_digest TEXT NOT NULL,
				deleted_at TEXT NOT NULL,
				reason TEXT NOT NULL,
				UNIQUE (space_id, idempotency_key)
			) STRICT`,
			`CREATE TABLE receipt_corrections (
				original_receipt_id TEXT PRIMARY KEY,
				replacement_receipt_id TEXT NOT NULL UNIQUE,
				space_id TEXT NOT NULL REFERENCES spaces(id),
				created_at TEXT NOT NULL,
				reason TEXT NOT NULL
			) STRICT`,
			`CREATE TABLE management_effects (
				operation TEXT NOT NULL,
				idempotency_key TEXT NOT NULL,
				request_digest TEXT NOT NULL,
				result_json TEXT NOT NULL,
				created_at TEXT NOT NULL,
				PRIMARY KEY (operation, idempotency_key)
			) STRICT`,
			`DROP TRIGGER receipts_immutable_delete`,
			`CREATE TRIGGER receipts_governed_delete
			BEFORE DELETE ON receipts
			WHEN NOT EXISTS (
				SELECT 1 FROM receipt_tombstones WHERE receipt_id = OLD.receipt_id
			) BEGIN
				SELECT RAISE(ABORT, 'receipt deletion requires a governance tombstone');
			END`,
			`CREATE INDEX receipt_corrections_replacement ON receipt_corrections(replacement_receipt_id)`,
			`CREATE INDEX receipt_tombstones_space ON receipt_tombstones(space_id, deleted_at)`,
		},
	},
	{
		version: 3,
		statements: []string{
			`CREATE TABLE steward_profiles (
				profile_id TEXT NOT NULL,
				version INTEGER NOT NULL CHECK (version > 0),
				provider_ref TEXT NOT NULL,
				model TEXT NOT NULL,
				system_prompt TEXT NOT NULL,
				max_context_records INTEGER NOT NULL CHECK (max_context_records BETWEEN 0 AND 64),
				max_input_bytes INTEGER NOT NULL CHECK (max_input_bytes BETWEEN 1024 AND 1048576),
				max_output_bytes INTEGER NOT NULL CHECK (max_output_bytes BETWEEN 1024 AND 131072),
				created_at TEXT NOT NULL,
				PRIMARY KEY (profile_id, version)
			) STRICT`,
			`CREATE TABLE space_steward_bindings (
				space_id TEXT PRIMARY KEY REFERENCES spaces(id),
				profile_id TEXT NOT NULL,
				profile_version INTEGER NOT NULL,
				bound_at TEXT NOT NULL,
				FOREIGN KEY (profile_id, profile_version) REFERENCES steward_profiles(profile_id, version)
			) STRICT`,
			`CREATE TABLE steward_jobs (
				job_id TEXT PRIMARY KEY,
				receipt_id TEXT NOT NULL UNIQUE,
				space_id TEXT NOT NULL REFERENCES spaces(id),
				profile_id TEXT NOT NULL,
				profile_version INTEGER NOT NULL,
				state TEXT NOT NULL CHECK (state IN ('pending', 'leased', 'completed', 'failed')),
				attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
				available_at TEXT NOT NULL,
				lease_expires_at TEXT,
				lease_token_digest TEXT NOT NULL DEFAULT '',
				proposal_digest TEXT NOT NULL DEFAULT '',
				result_json TEXT NOT NULL DEFAULT '',
				terminal_error_code TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				UNIQUE (job_id, space_id),
				FOREIGN KEY (profile_id, profile_version) REFERENCES steward_profiles(profile_id, version)
			) STRICT`,
			`CREATE INDEX steward_jobs_claim ON steward_jobs(state, available_at, created_at, job_id)`,
			`CREATE INDEX steward_jobs_space_state ON steward_jobs(space_id, state)`,
			`CREATE TABLE semantic_records (
				record_id TEXT PRIMARY KEY,
				space_id TEXT NOT NULL REFERENCES spaces(id),
				kind TEXT NOT NULL,
				status TEXT NOT NULL CHECK (status IN ('active', 'invalidated')),
				current_revision INTEGER NOT NULL CHECK (current_revision > 0),
				invalidated_reason TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				UNIQUE (record_id, space_id)
			) STRICT`,
			`CREATE INDEX semantic_records_space_status ON semantic_records(space_id, status, updated_at)`,
			`CREATE TABLE semantic_revisions (
				record_id TEXT NOT NULL,
				revision INTEGER NOT NULL CHECK (revision > 0),
				space_id TEXT NOT NULL REFERENCES spaces(id),
				kind TEXT NOT NULL,
				text TEXT NOT NULL,
				operation TEXT NOT NULL CHECK (operation IN ('ADD', 'MERGE', 'SUPERSEDE')),
				job_id TEXT NOT NULL UNIQUE,
				created_at TEXT NOT NULL,
				PRIMARY KEY (record_id, revision),
				UNIQUE (record_id, revision, space_id),
				FOREIGN KEY (record_id, space_id) REFERENCES semantic_records(record_id, space_id),
				FOREIGN KEY (job_id, space_id) REFERENCES steward_jobs(job_id, space_id)
			) STRICT`,
			`CREATE TABLE semantic_evidence (
				record_id TEXT NOT NULL,
				revision INTEGER NOT NULL,
				ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
				receipt_id TEXT NOT NULL,
				space_id TEXT NOT NULL REFERENCES spaces(id),
				PRIMARY KEY (record_id, revision, ordinal),
				UNIQUE (record_id, revision, receipt_id),
				FOREIGN KEY (record_id, revision, space_id)
				 REFERENCES semantic_revisions(record_id, revision, space_id) ON DELETE CASCADE
			) STRICT`,
			`CREATE INDEX semantic_evidence_receipt ON semantic_evidence(receipt_id, record_id, revision)`,
			`CREATE TABLE semantic_space_indexes (
				space_id TEXT PRIMARY KEY REFERENCES spaces(id),
				table_name TEXT NOT NULL UNIQUE
			) STRICT`,
			`CREATE TRIGGER semantic_revisions_immutable_update
			BEFORE UPDATE ON semantic_revisions BEGIN
				SELECT RAISE(ABORT, 'semantic revision is immutable');
			END`,
			`CREATE TRIGGER semantic_revisions_immutable_delete
			BEFORE DELETE ON semantic_revisions BEGIN
				SELECT RAISE(ABORT, 'semantic revision is immutable');
			END`,
			`CREATE TRIGGER semantic_evidence_immutable_update
			BEFORE UPDATE ON semantic_evidence BEGIN
				SELECT RAISE(ABORT, 'semantic evidence is immutable');
			END`,
			`CREATE TRIGGER semantic_evidence_immutable_delete
			BEFORE DELETE ON semantic_evidence BEGIN
				SELECT RAISE(ABORT, 'semantic evidence is immutable');
			END`,
		},
		apply: migrateSemanticSpaceIndexes,
	},
	{
		version: 4,
		statements: []string{
			// RC1 columns remain temporarily so schema-3 foreign keys do not require
			// rebuilding the complete semantic audit graph. Schema 4 never reads or
			// exposes them; preserving old bytes also keeps prepared RC1 rollback exact.
			`INSERT INTO metadata(key, value) VALUES ('steward_execution_mode', 'external_worker')
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		},
	},
	{
		version: 5,
		apply:   migrateLexicalProjection,
	},
}

func migrate(ctx context.Context, db *sql.DB, now time.Time) error {
	return migrateTo(ctx, db, now, CurrentSchemaVersion)
}

func migrateTo(ctx context.Context, db *sql.DB, now time.Time, targetVersion int) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	) STRICT`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > targetVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, targetVersion)
	}
	for _, item := range migrations {
		if item.version > targetVersion {
			break
		}
		if item.version <= version {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin schema migration %d: %w", item.version, err)
		}
		for _, statement := range item.statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply schema migration %d: %w", item.version, err)
			}
		}
		if item.apply != nil {
			if err := item.apply(ctx, tx); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply schema migration %d: %w", item.version, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
			item.version, formatTime(now)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record schema migration %d: %w", item.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema migration %d: %w", item.version, err)
		}
	}
	return nil
}
