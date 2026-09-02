package appliance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// Inspect returns bounded topology and row counts without bearer credentials or
// receipt text.
func (s *Store) Inspect(ctx context.Context) (Inspection, error) {
	result := Inspection{
		ProtocolVersion: managementv1alpha1.ProtocolVersion,
		SchemaVersion:   CurrentSchemaVersion,
		Generation:      s.generation,
		Counts:          make(map[string]int64),
		RestorePending:  s.restorePending.Load(),
	}
	rollbackPath := filepath.Join(s.dataDir, RollbackDatabaseFilename)
	if err := requireRegularOrAbsent(rollbackPath); err != nil {
		return Inspection{}, fmt.Errorf("inspect rollback database: %w", err)
	}
	if _, err := os.Lstat(rollbackPath); err == nil {
		result.RollbackAvailable = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return Inspection{}, fmt.Errorf("inspect rollback database: %w", err)
	}
	for _, table := range []string{
		"realms", "identities", "spaces", "views", "grants", "capabilities",
		"receipts", "receipt_corrections", "receipt_tombstones",
		"steward_profiles", "space_steward_bindings", "steward_jobs", "semantic_records",
		"space_lexicons", "lexicon_terms", "lexicon_term_evidence",
	} {
		var count int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			return Inspection{}, fmt.Errorf("count %s: %w", table, err)
		}
		result.Counts[table] = count
	}
	var ftsCount int64
	indexRows, err := s.db.QueryContext(ctx, `SELECT table_name FROM space_indexes ORDER BY space_id`)
	if err != nil {
		return Inspection{}, fmt.Errorf("list Space indexes: %w", err)
	}
	for indexRows.Next() {
		var tableName string
		if err := indexRows.Scan(&tableName); err != nil {
			_ = indexRows.Close()
			return Inspection{}, fmt.Errorf("read Space index: %w", err)
		}
		var count int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableName).Scan(&count); err != nil {
			result.Projection.Status = "unavailable"
			continue
		}
		ftsCount += count
	}
	if err := indexRows.Err(); err != nil {
		_ = indexRows.Close()
		return Inspection{}, fmt.Errorf("list Space indexes: %w", err)
	}
	if err := indexRows.Close(); err != nil {
		return Inspection{}, fmt.Errorf("close Space indexes: %w", err)
	}
	result.Counts["receipt_fts"] = ftsCount
	result.Projection.Entries = ftsCount
	result.Projection.ExpectedEntries = result.Counts["receipts"]
	result.Projection.Drift = result.Projection.Entries - result.Projection.ExpectedEntries
	if result.Projection.Status == "" {
		if result.Projection.Drift == 0 {
			result.Projection.Status = "ok"
			result.Projection.Healthy = true
		} else {
			result.Projection.Status = "drift"
		}
	}
	if rebuiltAt, err := s.metadata(ctx, "last_fts_rebuild_at"); err == nil {
		value, err := parseTime(rebuiltAt)
		if err != nil {
			return Inspection{}, fmt.Errorf("read FTS rebuild time: %w", err)
		}
		result.Projection.LastRebuiltAt = &value
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Inspection{}, fmt.Errorf("read FTS rebuild time: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, realm_id, identity_id, class FROM spaces ORDER BY id`)
	if err != nil {
		return Inspection{}, fmt.Errorf("list Spaces: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var space Space
		var identity sql.NullString
		if err := rows.Scan(&space.ID, &space.RealmID, &identity, &space.Class); err != nil {
			return Inspection{}, fmt.Errorf("read Space: %w", err)
		}
		if identity.Valid {
			space.IdentityID = v1alpha1.IdentityID(identity.String)
		}
		result.Spaces = append(result.Spaces, space)
	}
	if err := rows.Err(); err != nil {
		return Inspection{}, fmt.Errorf("list Spaces: %w", err)
	}
	result.Projection.Spaces = int64(len(result.Spaces))
	if err := s.inspectReceiptDiagnostics(ctx, &result); err != nil {
		return Inspection{}, err
	}
	if err := s.inspectCapabilityDiagnostics(ctx, &result); err != nil {
		return Inspection{}, err
	}
	if err := s.inspectStewardDiagnostics(ctx, &result); err != nil {
		return Inspection{}, err
	}
	if err := s.inspectLexiconDiagnostics(ctx, &result); err != nil {
		return Inspection{}, err
	}
	storage, err := inspectStorage(s.dataDir)
	if err != nil {
		return Inspection{}, err
	}
	result.Storage = storage
	return result, nil
}

func (s *Store) inspectLexiconDiagnostics(ctx context.Context, result *Inspection) error {
	result.Lexicon.AlgorithmVersion = lexiconAlgorithmVersion
	result.Lexicon.Spaces = result.Counts["space_lexicons"]
	result.Lexicon.EvidenceLinks = result.Counts["lexicon_term_evidence"]
	if err := s.db.QueryRowContext(ctx,
		`SELECT
		 COALESCE(SUM(generation), 0),
		 COALESCE(SUM(CASE WHEN generation != indexed_generation THEN 1 ELSE 0 END), 0)
		 FROM space_lexicons`).Scan(&result.Lexicon.GenerationSum, &result.Lexicon.PendingRebuilds); err != nil {
		return fmt.Errorf("inspect lexicon generations: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT
		 COALESCE(SUM(CASE WHEN status = 'candidate' THEN 1 ELSE 0 END), 0),
		 COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0),
		 COALESCE(SUM(CASE WHEN status = 'retired' THEN 1 ELSE 0 END), 0)
		 FROM lexicon_terms`).Scan(
		&result.Lexicon.CandidateTerms, &result.Lexicon.ActiveTerms, &result.Lexicon.RetiredTerms,
	); err != nil {
		return fmt.Errorf("inspect lexicon terms: %w", err)
	}
	return nil
}

func (s *Store) inspectReceiptDiagnostics(ctx context.Context, result *Inspection) error {
	result.Receipts.Stored = result.Counts["receipts"]
	result.Receipts.Deleted = result.Counts["receipt_tombstones"]
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM receipts r
		 WHERE EXISTS(SELECT 1 FROM receipt_corrections c WHERE c.original_receipt_id = r.receipt_id)`).Scan(
		&result.Receipts.Corrected); err != nil {
		return fmt.Errorf("count corrected receipts: %w", err)
	}
	result.Receipts.Active = result.Receipts.Stored - result.Receipts.Corrected
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM receipt_processing GROUP BY state`)
	if err != nil {
		return fmt.Errorf("count receipt processing states: %w", err)
	}
	for rows.Next() {
		var state v1alpha1.ProcessingState
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read receipt processing state: %w", err)
		}
		switch state {
		case v1alpha1.ProcessingStateAccepted:
			result.Receipts.Accepted = count
		case v1alpha1.ProcessingStateProcessing:
			result.Receipts.Processing = count
		case v1alpha1.ProcessingStateOrganized:
			result.Receipts.Organized = count
		case v1alpha1.ProcessingStateFailed:
			result.Receipts.Failed = count
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("count receipt processing states: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close receipt processing states: %w", err)
	}
	var oldest, newest sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(received_at), MAX(received_at) FROM receipts`).Scan(&oldest, &newest); err != nil {
		return fmt.Errorf("read receipt time range: %w", err)
	}
	if oldest.Valid {
		value, err := parseTime(oldest.String)
		if err != nil {
			return fmt.Errorf("read oldest receipt time: %w", err)
		}
		result.Receipts.OldestReceivedAt = &value
	}
	if newest.Valid {
		value, err := parseTime(newest.String)
		if err != nil {
			return fmt.Errorf("read newest receipt time: %w", err)
		}
		result.Receipts.NewestReceivedAt = &value
	}
	return nil
}

func (s *Store) inspectCapabilityDiagnostics(ctx context.Context, result *Inspection) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.expires_at, g.expires_at, g.revoked
		 FROM capabilities c JOIN grants g ON g.id = c.grant_id`)
	if err != nil {
		return fmt.Errorf("inspect capabilities: %w", err)
	}
	now := s.now()
	for rows.Next() {
		var capabilityRaw, grantRaw string
		var revoked bool
		if err := rows.Scan(&capabilityRaw, &grantRaw, &revoked); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read capability diagnostic: %w", err)
		}
		capabilityExpiry, capabilityErr := parseTime(capabilityRaw)
		grantExpiry, grantErr := parseTime(grantRaw)
		if capabilityErr != nil || grantErr != nil {
			_ = rows.Close()
			return fmt.Errorf("read capability expiry")
		}
		result.Capabilities.Stored++
		if !revoked && capabilityExpiry.After(now) && grantExpiry.After(now) {
			result.Capabilities.Active++
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("inspect capabilities: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close capability diagnostics: %w", err)
	}
	result.Capabilities.Inactive = result.Capabilities.Stored - result.Capabilities.Active
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM grants WHERE revoked = 1`).Scan(&result.Capabilities.RevokedGrants); err != nil {
		return fmt.Errorf("count revoked Grants: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issuer_credentials`).Scan(&result.Capabilities.IssuerPrincipals); err != nil {
		return fmt.Errorf("count issuer principals: %w", err)
	}
	return nil
}

func (s *Store) inspectStewardDiagnostics(ctx context.Context, result *Inspection) error {
	result.Steward.Profiles = result.Counts["steward_profiles"]
	result.Steward.Bindings = result.Counts["space_steward_bindings"]
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM steward_jobs GROUP BY state`)
	if err != nil {
		return fmt.Errorf("count Steward job states: %w", err)
	}
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read Steward job state: %w", err)
		}
		switch state {
		case "pending":
			result.Steward.PendingJobs = count
		case "leased":
			result.Steward.LeasedJobs = count
		case "completed":
			result.Steward.CompletedJobs = count
		case "failed":
			result.Steward.FailedJobs = count
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("count Steward job states: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close Steward job states: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT
		 COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0),
		 COALESCE(SUM(CASE WHEN status = 'invalidated' THEN 1 ELSE 0 END), 0)
		 FROM semantic_records`).Scan(&result.Steward.ActiveRecords, &result.Steward.InvalidatedRecords); err != nil {
		return fmt.Errorf("count semantic Record states: %w", err)
	}
	result.Steward.ProjectionExpected = result.Steward.ActiveRecords
	result.Steward.ProjectionStatus = "ok"
	result.Steward.ProjectionHealthy = true
	indexRows, err := s.db.QueryContext(ctx, `SELECT space_id, table_name FROM semantic_space_indexes ORDER BY space_id`)
	if err != nil {
		return fmt.Errorf("list semantic projection diagnostics: %w", err)
	}
	for indexRows.Next() {
		var spaceID v1alpha1.SpaceID
		var tableName string
		if err := indexRows.Scan(&spaceID, &tableName); err != nil {
			_ = indexRows.Close()
			return fmt.Errorf("read semantic projection diagnostic: %w", err)
		}
		if tableName != semanticSpaceIndexTable(spaceID) {
			result.Steward.ProjectionHealthy = false
			result.Steward.ProjectionStatus = "unavailable"
			continue
		}
		var count int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableName).Scan(&count); err != nil {
			result.Steward.ProjectionHealthy = false
			result.Steward.ProjectionStatus = "unavailable"
			continue
		}
		result.Steward.ProjectionEntries += count
	}
	if err := indexRows.Err(); err != nil {
		_ = indexRows.Close()
		return fmt.Errorf("list semantic projection diagnostics: %w", err)
	}
	if err := indexRows.Close(); err != nil {
		return fmt.Errorf("close semantic projection diagnostics: %w", err)
	}
	result.Counts["semantic_fts"] = result.Steward.ProjectionEntries
	result.Steward.ProjectionDrift = result.Steward.ProjectionEntries - result.Steward.ProjectionExpected
	if result.Steward.ProjectionStatus == "ok" && result.Steward.ProjectionDrift != 0 {
		result.Steward.ProjectionHealthy = false
		result.Steward.ProjectionStatus = "drift"
	}
	var oldest sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT MIN(created_at) FROM steward_jobs WHERE state IN ('pending', 'leased')`).Scan(&oldest); err != nil {
		return fmt.Errorf("read oldest Steward job: %w", err)
	}
	if oldest.Valid {
		value, err := parseTime(oldest.String)
		if err != nil {
			return fmt.Errorf("read oldest Steward job time: %w", err)
		}
		result.Steward.OldestOutstandingAt = &value
	}
	return nil
}

func inspectStorage(dataDir string) (managementv1alpha1.StorageDiagnostics, error) {
	var result managementv1alpha1.StorageDiagnostics
	paths := []struct {
		name string
		set  func(uint64)
	}{
		{DatabaseFilename, func(value uint64) { result.DatabaseBytes = value }},
		{DatabaseFilename + "-wal", func(value uint64) { result.WALBytes = value }},
		{DatabaseFilename + "-shm", func(value uint64) { result.SHMBytes = value }},
		{RollbackDatabaseFilename, func(value uint64) { result.RollbackBytes = value }},
	}
	for _, item := range paths {
		path := filepath.Join(dataDir, item.name)
		if err := requireRegularOrAbsent(path); err != nil {
			return managementv1alpha1.StorageDiagnostics{}, fmt.Errorf("inspect storage file %s: %w", item.name, err)
		}
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return managementv1alpha1.StorageDiagnostics{}, fmt.Errorf("inspect storage file %s: %w", item.name, err)
		}
		item.set(uint64(info.Size()))
	}
	result.DataBytes = result.DatabaseBytes + result.WALBytes + result.SHMBytes + result.RollbackBytes
	filesystemBytes, availableBytes, err := storageCapacity(dataDir)
	if err != nil {
		return managementv1alpha1.StorageDiagnostics{}, fmt.Errorf("inspect storage capacity: %w", err)
	}
	result.FilesystemBytes = filesystemBytes
	result.AvailableBytes = availableBytes
	return result, nil
}

// RebuildFTS reconstructs the disposable lexical projection solely from
// immutable receipts.
func (s *Store) RebuildFTS(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin FTS rebuild: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT space_id, table_name FROM space_indexes ORDER BY space_id`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("list Space indexes: %w", err)
	}
	type index struct {
		spaceID   v1alpha1.SpaceID
		tableName string
	}
	var indexes []index
	for rows.Next() {
		var item index
		if err := rows.Scan(&item.spaceID, &item.tableName); err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			return fmt.Errorf("read Space index: %w", err)
		}
		indexes = append(indexes, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		return fmt.Errorf("list Space indexes: %w", err)
	}
	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("close Space indexes: %w", err)
	}
	for _, item := range indexes {
		activeTerms, err := s.activeLexiconTerms(ctx, tx, item.spaceID)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("read Space lexicon: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+item.tableName); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("clear Space FTS projection: %w", err)
		}
		if err := rebuildReceiptProjection(ctx, tx, item.spaceID, item.tableName, activeTerms); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rebuild Space FTS projection: %w", err)
		}
	}
	semanticRows, err := tx.QueryContext(ctx, `SELECT space_id, table_name FROM semantic_space_indexes ORDER BY space_id`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("list semantic Space indexes: %w", err)
	}
	var semanticIndexes []index
	for semanticRows.Next() {
		var item index
		if err := semanticRows.Scan(&item.spaceID, &item.tableName); err != nil {
			_ = semanticRows.Close()
			_ = tx.Rollback()
			return fmt.Errorf("read semantic Space index: %w", err)
		}
		if item.tableName != semanticSpaceIndexTable(item.spaceID) {
			_ = semanticRows.Close()
			_ = tx.Rollback()
			return fmt.Errorf("semantic Space index identity mismatch")
		}
		semanticIndexes = append(semanticIndexes, item)
	}
	if err := semanticRows.Err(); err != nil {
		_ = semanticRows.Close()
		_ = tx.Rollback()
		return fmt.Errorf("list semantic Space indexes: %w", err)
	}
	if err := semanticRows.Close(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("close semantic Space indexes: %w", err)
	}
	for _, item := range semanticIndexes {
		activeTerms, err := s.activeLexiconTerms(ctx, tx, item.spaceID)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("read semantic Space lexicon: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+item.tableName); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("clear semantic Space FTS projection: %w", err)
		}
		if err := rebuildSemanticProjection(ctx, tx, item.spaceID, item.tableName, activeTerms); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rebuild semantic Space FTS projection: %w", err)
		}
	}
	for _, item := range indexes {
		if err := markLexiconIndexed(ctx, tx, item.spaceID, formatTime(s.now().UTC())); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("publish rebuilt Space lexicon: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO metadata(key, value) VALUES ('last_fts_rebuild_at', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, formatTime(s.now().UTC())); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record FTS rebuild time: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit FTS rebuild: %w", err)
	}
	return nil
}

func createSpaceIndex(ctx context.Context, tx *sql.Tx, spaceID v1alpha1.SpaceID, now string) error {
	tableName := spaceIndexTable(spaceID)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO space_indexes(space_id, table_name) VALUES (?, ?)`, spaceID, tableName); err != nil {
		return fmt.Errorf("record Space %q index: %w", spaceID, err)
	}
	if err := createReceiptFTSTable(ctx, tx, tableName); err != nil {
		return fmt.Errorf("create Space %q index: %w", spaceID, err)
	}
	if err := createSemanticSpaceIndex(ctx, tx, spaceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO space_lexicons(space_id, algorithm_version, updated_at) VALUES (?, ?, ?)`,
		spaceID, lexiconAlgorithmVersion, now); err != nil {
		return fmt.Errorf("initialize Space %q lexicon: %w", spaceID, err)
	}
	return nil
}

func spaceIndexTable(spaceID v1alpha1.SpaceID) string {
	return "receipt_fts_" + digestString(string(spaceID))
}
