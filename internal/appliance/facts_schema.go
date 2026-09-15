package appliance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const factsSchemaBaselineID = "memory-v0.6.0"

var factsMigrationSQL = []string{
	`ALTER TABLE semantic_records ADD COLUMN subject TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE semantic_records ADD COLUMN fact_key TEXT NOT NULL DEFAULT ''`,
	`CREATE INDEX semantic_records_fact ON semantic_records(space_id,label_set_digest,subject,fact_key,status)`,
	`CREATE TABLE evidence_sources (
 space_id TEXT NOT NULL REFERENCES spaces(id), label_set_digest TEXT NOT NULL,
 source_key TEXT NOT NULL, receipt_id TEXT NOT NULL UNIQUE, source_json TEXT NOT NULL,
 request_digest TEXT NOT NULL, suppressed INTEGER NOT NULL DEFAULT 0 CHECK(suppressed IN (0,1)),
 PRIMARY KEY(space_id,label_set_digest,source_key)) STRICT`,
	`CREATE TABLE ingestion_policies (
 space_id TEXT NOT NULL REFERENCES spaces(id), label_set_digest TEXT NOT NULL,
 producer TEXT NOT NULL, subject TEXT NOT NULL, denied INTEGER NOT NULL CHECK(denied IN (0,1)), reason TEXT NOT NULL,
 PRIMARY KEY(space_id,label_set_digest,producer,subject)) STRICT`,
	`CREATE TABLE memory_changes (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, space_id TEXT NOT NULL REFERENCES spaces(id),
 label_set_digest TEXT NOT NULL, kind TEXT NOT NULL, receipt_id TEXT NOT NULL DEFAULT '',
 record_id TEXT NOT NULL DEFAULT '', changed_at TEXT NOT NULL) STRICT`,
	`CREATE INDEX memory_changes_scope ON memory_changes(space_id,label_set_digest,sequence)`,
}

// migrateFactsSchema is a single SQLite transaction. A killed migration either
// leaves schema 1 intact or publishes all schema 2 state; Open safely retries.
// Evidence is copied before dropping either FK table, avoiding cascade loss.
func migrateFactsSchema(ctx context.Context, db *sql.DB, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exec := func(q string) error { _, e := tx.ExecContext(ctx, q); return e }
	for _, q := range factsMigrationSQL {
		if err = exec(q); err != nil {
			return fmt.Errorf("migrate facts schema: %w", err)
		}
	}
	if err = exec(`CREATE TABLE migration_evidence AS SELECT * FROM semantic_evidence`); err != nil {
		return err
	}
	if err = exec(`CREATE TABLE migration_revisions AS SELECT * FROM semantic_revisions`); err != nil {
		return err
	}
	for _, q := range []string{`DROP TABLE semantic_evidence`, `DROP TABLE semantic_revisions`} {
		if err = exec(q); err != nil {
			return err
		}
	}
	var evidenceDDL string
	for _, q := range baselineSchema {
		switch {
		case strings.HasPrefix(q, "CREATE TABLE semantic_revisions ("):
			q = strings.Replace(q, "job_id TEXT NOT NULL UNIQUE", "job_id TEXT", 1)
			q = strings.Replace(q, "created_at TEXT NOT NULL,", "created_at TEXT NOT NULL,\n fact_json TEXT NOT NULL DEFAULT '',", 1)
			if err = exec(q); err != nil {
				return err
			}
		case strings.HasPrefix(q, "CREATE TABLE semantic_evidence ("):
			evidenceDDL = q
		}
	}
	if err = exec(`INSERT INTO semantic_revisions(record_id,revision,space_id,kind,text,operation,job_id,created_at) SELECT record_id,revision,space_id,kind,text,operation,job_id,created_at FROM migration_revisions`); err != nil {
		return err
	}
	if err = exec(evidenceDDL); err != nil {
		return err
	}
	if err = exec(`INSERT INTO semantic_evidence SELECT * FROM migration_evidence`); err != nil {
		return err
	}
	for _, q := range baselineSchema {
		if strings.HasPrefix(q, "CREATE TRIGGER semantic_") || strings.HasPrefix(q, "CREATE INDEX semantic_evidence_receipt") {
			if err = exec(q); err != nil {
				return err
			}
		}
	}
	for _, q := range []string{`DROP TABLE migration_revisions`, `DROP TABLE migration_evidence`} {
		if err = exec(q); err != nil {
			return err
		}
	}
	for _, group := range [][]string{stewardMigrationSQL, governanceMigrationSQL} {
		for _, q := range group {
			if err = exec(q); err != nil {
				return fmt.Errorf("migrate lifecycle governance: %w", err)
			}
		}
	}
	// Existing leases have no recorded read set. Force a fresh Claim, never accept
	// a pre-migration model response whose dependencies cannot be reconstructed.
	if err = exec(`UPDATE steward_jobs SET state='pending',lease_token_digest='',lease_expires_at=NULL WHERE state='leased'`); err != nil {
		return err
	}
	if err = normalizeStewardSchedule(ctx, tx); err != nil {
		return fmt.Errorf("normalize steward schedule: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE metadata SET value=? WHERE key='schema_baseline'`, factsSchemaBaselineID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(2,?)`, formatTime(now)); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	bad := rows.Next()
	closeErr := rows.Close()
	if bad {
		return fmt.Errorf("migration foreign key violation")
	}
	if closeErr != nil {
		return closeErr
	}
	return tx.Commit()
}
