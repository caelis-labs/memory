package appliance

// stewardMigrationSQL is the additive M02 read-dependency schema owned by the
// Steward surface. migrateFactsSchema applies it inside the single facts-schema
// transaction, immediately after the semantic_revisions/semantic_evidence
// rebuild (so fact_json exists) and before governanceMigrationSQL. It never
// rebuilds an existing table or touches a governance trigger.
//
// One row records one piece of context the assigned model actually read: the
// exact Record head revision plus one cited evidence receipt. Apply revalidates
// every row before mutating canonical state, so a model proposal can never be
// applied against context that changed after the lease was issued.
var stewardMigrationSQL = []string{
	`CREATE TABLE IF NOT EXISTS steward_read_set (
		job_id TEXT NOT NULL,
		attempt INTEGER NOT NULL CHECK (attempt >= 0),
		ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
		record_id TEXT NOT NULL,
		revision INTEGER NOT NULL CHECK (revision > 0),
		receipt_id TEXT NOT NULL,
		PRIMARY KEY (job_id, attempt, ordinal)
	) STRICT`,
	`CREATE INDEX IF NOT EXISTS steward_read_set_receipt
	 ON steward_read_set(receipt_id)`,
}
