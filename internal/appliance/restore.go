package appliance

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const RollbackDatabaseFilename = "memory.db.rollback"

type RestoreOptions struct {
	DataDir              string
	Snapshot             io.Reader
	ManagementCredential string
	Clock                func() time.Time
	Random               io.Reader
}

type RestoreResult struct {
	SourceGeneration  string `json:"source_generation"`
	StorageGeneration string `json:"storage_generation"`
	SchemaVersion     int    `json:"schema_version"`
	RollbackAvailable bool   `json:"rollback_available"`
	RollbackPath      string `json:"rollback_path,omitempty"`
}

// PrepareUpgrade captures the exact stopped database and marks the live
// generation pending before a new binary can migrate or serve it. The new
// process remains management-readable but cannot acknowledge mutations until
// CommitRestore; the old memoryctl can use RollbackRestore on failure.
func PrepareUpgrade(ctx context.Context, dataDir, managementCredential string) (RestoreResult, error) {
	credential := strings.TrimSpace(managementCredential)
	if dataDir == "" || credential == "" {
		return RestoreResult{}, fmt.Errorf("upgrade data directory and management credential are required")
	}
	lock, err := acquireOwnerLock(dataDir)
	if err != nil {
		return RestoreResult{}, err
	}
	defer lock.close()
	databasePath := filepath.Join(dataDir, DatabaseFilename)
	rollbackPath := filepath.Join(dataDir, RollbackDatabaseFilename)
	for _, path := range []string{databasePath, rollbackPath} {
		if err := requireRegularOrAbsent(path); err != nil {
			return RestoreResult{}, fmt.Errorf("secure upgrade database %s: %w", filepath.Base(path), err)
		}
	}
	if _, err := os.Stat(databasePath); err != nil {
		return RestoreResult{}, fmt.Errorf("inspect upgrade database: %w", err)
	}
	if err := verifySQLiteFile(ctx, databasePath, true); err != nil {
		return RestoreResult{}, fmt.Errorf("verify upgrade database: %w", err)
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("open upgrade database: %w", err)
	}
	var generation, storedDigest, pending string
	var schemaVersion int
	readErr := database.QueryRowContext(ctx,
		`SELECT
		 (SELECT value FROM metadata WHERE key = 'storage_generation'),
		 (SELECT value FROM metadata WHERE key = 'management_credential_digest'),
		 COALESCE((SELECT value FROM metadata WHERE key = 'restore_pending'), '0'),
		 (SELECT MAX(version) FROM schema_migrations)`).Scan(
		&generation, &storedDigest, &pending, &schemaVersion)
	closeErr := database.Close()
	if readErr != nil {
		return RestoreResult{}, fmt.Errorf("read upgrade state: %w", readErr)
	}
	if closeErr != nil {
		return RestoreResult{}, fmt.Errorf("close upgrade database: %w", closeErr)
	}
	if subtle.ConstantTimeCompare([]byte(digestString(credential)), []byte(storedDigest)) != 1 {
		return RestoreResult{}, fmt.Errorf("management credential does not authorize upgrade preparation")
	}
	rollbackExists := false
	if _, err := os.Lstat(rollbackPath); err == nil {
		rollbackExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return RestoreResult{}, fmt.Errorf("inspect upgrade rollback database: %w", err)
	}
	if pending == "1" {
		if !rollbackExists {
			return RestoreResult{}, fmt.Errorf("generation is already pending without a rollback database")
		}
		if err := verifySQLiteFile(ctx, rollbackPath, true); err != nil {
			return RestoreResult{}, fmt.Errorf("verify upgrade rollback database: %w", err)
		}
		var rollbackGeneration string
		rollback, err := sql.Open("sqlite", rollbackPath)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("open upgrade rollback database: %w", err)
		}
		readErr := rollback.QueryRowContext(ctx,
			`SELECT value FROM metadata WHERE key = 'storage_generation'`).Scan(&rollbackGeneration)
		closeErr := rollback.Close()
		if readErr != nil {
			return RestoreResult{}, fmt.Errorf("read upgrade rollback generation: %w", readErr)
		}
		if closeErr != nil {
			return RestoreResult{}, fmt.Errorf("close upgrade rollback database: %w", closeErr)
		}
		if rollbackGeneration != generation {
			return RestoreResult{}, fmt.Errorf("existing rollback database belongs to another generation")
		}
		return RestoreResult{
			SourceGeneration: generation, StorageGeneration: generation,
			SchemaVersion: schemaVersion, RollbackAvailable: true, RollbackPath: rollbackPath,
		}, nil
	}
	if pending != "0" {
		return RestoreResult{}, fmt.Errorf("stored pending state is invalid")
	}
	if rollbackExists {
		// A rollback image with no pending barrier can only be an interrupted
		// preparation. It may predate later acknowledged writes, so recapture
		// the exact stopped database instead of trusting generation identity.
		if err := os.Remove(rollbackPath); err != nil {
			return RestoreResult{}, fmt.Errorf("remove abandoned upgrade rollback database: %w", err)
		}
		if err := syncDirectory(dataDir); err != nil {
			return RestoreResult{}, err
		}
		rollbackExists = false
	}
	createdRollback := false
	if !rollbackExists {
		createdRollback, err = createRollbackSnapshot(ctx, databasePath, rollbackPath)
		if err != nil {
			return RestoreResult{}, err
		}
		if !createdRollback {
			return RestoreResult{}, fmt.Errorf("upgrade database is unavailable")
		}
		if err := syncDirectory(dataDir); err != nil {
			_ = os.Remove(rollbackPath)
			return RestoreResult{}, err
		}
	}
	removeCreatedRollback := func() {
		if createdRollback {
			_ = os.Remove(rollbackPath)
		}
	}
	database, err = sql.Open("sqlite", databasePath)
	if err != nil {
		removeCreatedRollback()
		return RestoreResult{}, fmt.Errorf("open upgrade database: %w", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO metadata(key, value) VALUES ('restore_pending', '1')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		_ = database.Close()
		return RestoreResult{}, fmt.Errorf("mark upgrade pending: %w", err)
	}
	if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		_ = database.Close()
		return RestoreResult{}, fmt.Errorf("checkpoint pending upgrade database: %w", err)
	}
	if err := database.Close(); err != nil {
		return RestoreResult{}, fmt.Errorf("close pending upgrade database: %w", err)
	}
	if err := syncDirectory(dataDir); err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{
		SourceGeneration: generation, StorageGeneration: generation,
		SchemaVersion: schemaVersion, RollbackAvailable: true, RollbackPath: rollbackPath,
	}, nil
}

// Restore installs a fully authenticated plaintext snapshot only after the
// caller has stopped memoryd. It verifies, migrates, rotates generation, and
// stages the database before one atomic replacement.
func Restore(ctx context.Context, options RestoreOptions) (RestoreResult, error) {
	if options.DataDir == "" || options.Snapshot == nil {
		return RestoreResult{}, fmt.Errorf("restore data directory and snapshot are required")
	}
	credential := strings.TrimSpace(options.ManagementCredential)
	if credential == "" {
		return RestoreResult{}, fmt.Errorf("restore management credential is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if err := os.MkdirAll(options.DataDir, 0o700); err != nil {
		return RestoreResult{}, fmt.Errorf("create restore data directory: %w", err)
	}
	if err := os.Chmod(options.DataDir, 0o700); err != nil {
		return RestoreResult{}, fmt.Errorf("secure restore data directory: %w", err)
	}
	lock, err := acquireOwnerLock(options.DataDir)
	if err != nil {
		return RestoreResult{}, err
	}
	defer lock.close()
	rollbackPath := filepath.Join(options.DataDir, RollbackDatabaseFilename)
	if err := requireRegularOrAbsent(rollbackPath); err != nil {
		return RestoreResult{}, fmt.Errorf("secure rollback path: %w", err)
	}
	if _, err := os.Lstat(rollbackPath); err == nil {
		return RestoreResult{}, fmt.Errorf("rollback database already exists; commit or roll it back before another restore")
	} else if !errors.Is(err, os.ErrNotExist) {
		return RestoreResult{}, fmt.Errorf("inspect rollback database: %w", err)
	}
	suffix, err := randomHexFrom(options.Random, 16)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create restore identity: %w", err)
	}
	stagedPath := filepath.Join(options.DataDir, ".memory-restore-"+suffix+".db")
	staged, err := os.OpenFile(stagedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create staged restore: %w", err)
	}
	cleanupStage := true
	defer func() {
		_ = staged.Close()
		if cleanupStage {
			_ = os.Remove(stagedPath)
		}
	}()
	if _, err := io.Copy(staged, options.Snapshot); err != nil {
		return RestoreResult{}, fmt.Errorf("read authenticated restore snapshot: %w", err)
	}
	if err := staged.Sync(); err != nil {
		return RestoreResult{}, fmt.Errorf("sync staged restore: %w", err)
	}
	if err := staged.Close(); err != nil {
		return RestoreResult{}, fmt.Errorf("close staged restore: %w", err)
	}
	if err := verifySQLiteFile(ctx, stagedPath, true); err != nil {
		return RestoreResult{}, fmt.Errorf("verify restore snapshot: %w", err)
	}
	sourceGeneration, newGeneration, err := prepareRestoredDatabase(
		ctx, stagedPath, credential, options.Clock().UTC(), options.Random, true,
	)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := verifySQLiteFile(ctx, stagedPath, true); err != nil {
		return RestoreResult{}, fmt.Errorf("verify migrated restore snapshot: %w", err)
	}
	databasePath := filepath.Join(options.DataDir, DatabaseFilename)
	rollbackAvailable, err := createRollbackSnapshot(ctx, databasePath, rollbackPath)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := installManagementCredential(options.DataDir, credential, options.Random); err != nil {
		if rollbackAvailable {
			_ = os.Remove(rollbackPath)
		}
		return RestoreResult{}, err
	}
	for _, path := range []string{databasePath + "-wal", databasePath + "-shm"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return RestoreResult{}, fmt.Errorf("remove stale SQLite sidecar %s: %w", filepath.Base(path), err)
		}
	}
	if err := os.Rename(stagedPath, databasePath); err != nil {
		return RestoreResult{}, fmt.Errorf("install restored database: %w", err)
	}
	cleanupStage = false
	if err := os.Chmod(databasePath, 0o600); err != nil {
		return RestoreResult{}, fmt.Errorf("secure restored database: %w", err)
	}
	if err := syncDirectory(options.DataDir); err != nil {
		return RestoreResult{}, err
	}
	result := RestoreResult{
		SourceGeneration: sourceGeneration, StorageGeneration: newGeneration,
		SchemaVersion: CurrentSchemaVersion, RollbackAvailable: rollbackAvailable,
	}
	if rollbackAvailable {
		result.RollbackPath = rollbackPath
	}
	return result, nil
}

// RollbackRestore reinstalls the pre-restore consistent database. Operators
// must perform this before allowing the restored generation to accept writes.
func RollbackRestore(ctx context.Context, dataDir, managementCredential string, random io.Reader) (RestoreResult, error) {
	credential := strings.TrimSpace(managementCredential)
	if dataDir == "" || credential == "" {
		return RestoreResult{}, fmt.Errorf("rollback data directory and management credential are required")
	}
	if random == nil {
		random = rand.Reader
	}
	lock, err := acquireOwnerLock(dataDir)
	if err != nil {
		return RestoreResult{}, err
	}
	defer lock.close()
	databasePath := filepath.Join(dataDir, DatabaseFilename)
	rollbackPath := filepath.Join(dataDir, RollbackDatabaseFilename)
	if err := requireRegularOrAbsent(rollbackPath); err != nil {
		return RestoreResult{}, fmt.Errorf("secure rollback database: %w", err)
	}
	if _, err := os.Stat(rollbackPath); err != nil {
		return RestoreResult{}, fmt.Errorf("rollback database is unavailable: %w", err)
	}
	if err := verifySQLiteFile(ctx, rollbackPath, true); err != nil {
		return RestoreResult{}, fmt.Errorf("verify rollback database: %w", err)
	}
	sourceGeneration, newGeneration, err := prepareRestoredDatabase(ctx, rollbackPath, credential, time.Now().UTC(), random, false)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := installManagementCredential(dataDir, credential, random); err != nil {
		return RestoreResult{}, err
	}
	for _, path := range []string{databasePath + "-wal", databasePath + "-shm"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return RestoreResult{}, fmt.Errorf("remove stale SQLite sidecar %s: %w", filepath.Base(path), err)
		}
	}
	if err := os.Rename(rollbackPath, databasePath); err != nil {
		return RestoreResult{}, fmt.Errorf("install rollback database: %w", err)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		return RestoreResult{}, fmt.Errorf("secure rollback database: %w", err)
	}
	if err := syncDirectory(dataDir); err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{
		SourceGeneration: sourceGeneration, StorageGeneration: newGeneration,
		SchemaVersion: CurrentSchemaVersion,
	}, nil
}

// CommitRestore removes the pre-restore rollback snapshot after the operator
// has accepted the restored generation.
func CommitRestore(dataDir, managementCredential string) error {
	credential := strings.TrimSpace(managementCredential)
	if dataDir == "" || credential == "" {
		return fmt.Errorf("restore data directory and management credential are required")
	}
	store, err := Open(context.Background(), Options{DataDir: dataDir})
	if err != nil {
		return err
	}
	defer store.Close()
	if !store.AuthenticateManagement(credential) {
		return fmt.Errorf("management credential does not authorize restore commit")
	}
	return store.CommitRestore(context.Background())
}

// CommitRestore accepts the installed generation, removes its rollback image,
// and only then allows data-plane calls and readiness to succeed.
func (s *Store) CommitRestore(ctx context.Context) error {
	path := filepath.Join(s.dataDir, RollbackDatabaseFilename)
	if err := requireRegularOrAbsent(path); err != nil {
		return fmt.Errorf("secure rollback database: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("commit restore: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO metadata(key, value) VALUES ('restore_pending', '0')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		return fmt.Errorf("commit restored generation: %w", err)
	}
	s.restorePending.Store(false)
	return syncDirectory(s.dataDir)
}

func prepareRestoredDatabase(
	ctx context.Context,
	path, credential string,
	now time.Time,
	random io.Reader,
	restorePending bool,
) (string, string, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return "", "", fmt.Errorf("open restored database: %w", err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return "", "", fmt.Errorf("enable restored foreign keys: %w", err)
	}
	var sourceGeneration string
	if err := database.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key = 'storage_generation'`).Scan(&sourceGeneration); err != nil {
		return "", "", fmt.Errorf("read restored storage generation: %w", err)
	}
	if err := migrate(ctx, database, now); err != nil {
		return "", "", fmt.Errorf("migrate restored database: %w", err)
	}
	newGeneration, err := randomHexFrom(random, 16)
	if err != nil {
		return "", "", fmt.Errorf("rotate restored storage generation: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin restored metadata update: %w", err)
	}
	rollback := func(err error) (string, string, error) {
		_ = tx.Rollback()
		return "", "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE metadata SET value = ? WHERE key = 'storage_generation'`, newGeneration); err != nil {
		return rollback(fmt.Errorf("store restored generation: %w", err))
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO metadata(key, value) VALUES ('management_credential_digest', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, digestString(credential)); err != nil {
		return rollback(fmt.Errorf("bind restored management credential: %w", err))
	}
	pendingValue := "0"
	if restorePending {
		pendingValue = "1"
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO metadata(key, value) VALUES ('restore_pending', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, pendingValue); err != nil {
		return rollback(fmt.Errorf("store restored pending state: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit restored metadata: %w", err)
	}
	if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return "", "", fmt.Errorf("checkpoint restored database: %w", err)
	}
	return sourceGeneration, newGeneration, nil
}

func createRollbackSnapshot(ctx context.Context, databasePath, rollbackPath string) (bool, error) {
	if err := requireRegularOrAbsent(databasePath); err != nil {
		return false, fmt.Errorf("secure current database: %w", err)
	}
	if _, err := os.Stat(databasePath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect current database: %w", err)
	}
	current, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return false, fmt.Errorf("open current database for rollback: %w", err)
	}
	if _, err := current.ExecContext(ctx, `VACUUM INTO ?`, rollbackPath); err != nil {
		_ = current.Close()
		_ = os.Remove(rollbackPath)
		return false, fmt.Errorf("create pre-restore rollback snapshot: %w", err)
	}
	if err := current.Close(); err != nil {
		_ = os.Remove(rollbackPath)
		return false, fmt.Errorf("close pre-restore database: %w", err)
	}
	if err := os.Chmod(rollbackPath, 0o600); err != nil {
		_ = os.Remove(rollbackPath)
		return false, fmt.Errorf("secure rollback snapshot: %w", err)
	}
	if err := verifySQLiteFile(ctx, rollbackPath, true); err != nil {
		_ = os.Remove(rollbackPath)
		return false, fmt.Errorf("verify rollback snapshot: %w", err)
	}
	rollback, err := os.OpenFile(rollbackPath, os.O_RDWR, 0)
	if err != nil {
		_ = os.Remove(rollbackPath)
		return false, fmt.Errorf("open rollback snapshot for sync: %w", err)
	}
	if err := rollback.Sync(); err != nil {
		_ = rollback.Close()
		_ = os.Remove(rollbackPath)
		return false, fmt.Errorf("sync rollback snapshot: %w", err)
	}
	if err := rollback.Close(); err != nil {
		_ = os.Remove(rollbackPath)
		return false, fmt.Errorf("close rollback snapshot: %w", err)
	}
	return true, nil
}

func installManagementCredential(dataDir, credential string, random io.Reader) error {
	path := filepath.Join(dataDir, ManagementCredentialFile)
	if err := requireRegularOrAbsent(path); err != nil {
		return fmt.Errorf("secure restored management credential path: %w", err)
	}
	current, err := os.ReadFile(path)
	if err == nil {
		if strings.TrimSpace(string(current)) != credential {
			return fmt.Errorf("existing management credential does not match restore authority")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("secure restored management credential: %w", err)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read restored management credential: %w", err)
	}
	suffix, err := randomHexFrom(random, 8)
	if err != nil {
		return fmt.Errorf("create management credential staging identity: %w", err)
	}
	temporary := filepath.Join(dataDir, ".management-restore-"+suffix+".token")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create restored management credential: %w", err)
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := io.WriteString(file, credential+"\n"); err != nil {
		return fmt.Errorf("write restored management credential: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync restored management credential: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close restored management credential: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("install restored management credential: %w", err)
	}
	cleanup = false
	return nil
}

func randomHexFrom(random io.Reader, size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	const alphabet = "0123456789abcdef"
	encoded := make([]byte, size*2)
	for index, item := range value {
		encoded[index*2] = alphabet[item>>4]
		encoded[index*2+1] = alphabet[item&0x0f]
	}
	return string(encoded), nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open data directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync data directory: %w", err)
	}
	return nil
}
