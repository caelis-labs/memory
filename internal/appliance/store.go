package appliance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	_ "modernc.org/sqlite"
)

// Store is the single durable authority behind the public package and optional
// standalone process adapter.
type Store struct {
	db                  *sql.DB
	lock                *ownerLock
	dataDir             string
	now                 func() time.Time
	random              io.Reader
	faults              Faults
	candidateRead       func(v1alpha1.SpaceID)
	requestID           atomic.Uint64
	closing             atomic.Bool
	restorePending      atomic.Bool
	generation          string
	managementSum       [sha256.Size]byte
	stewardWorkerSum    [sha256.Size]byte
	managementMu        sync.RWMutex
	managementRotateMu  sync.Mutex
	stewardJobMu        sync.Mutex
	lexiconPolicy       LexiconPolicy
	experimentalLexicon bool
}

// Open acquires the data-directory owner lock, initializes SQLite, and creates
// owner-only local management authentication.
func Open(ctx context.Context, options Options) (*Store, error) {
	if options.DataDir == "" {
		return nil, fmt.Errorf("data directory is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.BusyTimeoutMS <= 0 {
		options.BusyTimeoutMS = defaultSQLiteBusyTimeoutMS
	}
	if err := os.MkdirAll(options.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := secureOwnerPath(options.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure data directory: %w", err)
	}
	lock, err := acquireOwnerLock(options.DataDir)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(options.DataDir, DatabaseFilename)
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := requireRegularOrAbsent(path); err != nil {
			_ = lock.close()
			return nil, fmt.Errorf("secure SQLite path: %w", err)
		}
	}
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(FULL)")
	query.Add("_pragma", "busy_timeout("+strconv.Itoa(options.BusyTimeoutMS)+")")
	// Every mutable transaction reads authorization or canonical state before
	// writing. BEGIN IMMEDIATE acquires SQLite's single WAL writer slot before
	// those reads, avoiding an un-retryable deferred-transaction snapshot
	// upgrade when Remember and Steward commit concurrently. Read-only
	// transactions remain deferred because the driver honors TxOptions.ReadOnly.
	query.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", sqliteFileDSN(dbPath, query))
	if err != nil {
		_ = lock.close()
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	store := &Store{
		db:                  db,
		lock:                lock,
		dataDir:             options.DataDir,
		now:                 options.Clock,
		random:              options.Random,
		faults:              options.Faults,
		candidateRead:       options.CandidateRead,
		lexiconPolicy:       normalizeLexiconPolicy(options.LexiconPolicy),
		experimentalLexicon: options.LexiconPolicy != nil,
	}
	if err := store.initialize(ctx); err != nil {
		_ = store.closeResources()
		return nil, err
	}
	if err := secureOwnerPath(dbPath, 0o600); err != nil {
		_ = store.closeResources()
		return nil, fmt.Errorf("secure SQLite database: %w", err)
	}
	return store, nil
}

// sqliteFileDSN builds a SQLite URI DSN whose authority is empty. Windows
// drive-letter paths must be file:///C:/...; putting C:\... in url.URL.Path
// emits file://C:%5C... which SQLite rejects as an invalid URI authority.
func sqliteFileDSN(dbPath string, query url.Values) string {
	path := strings.ReplaceAll(filepath.ToSlash(dbPath), `\`, "/")
	if windowsDrivePath(path) && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func windowsDrivePath(slashPath string) bool {
	if len(slashPath) < 3 || slashPath[1] != ':' || slashPath[2] != '/' {
		return false
	}
	drive := slashPath[0]
	return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
}

func (s *Store) initialize(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping SQLite: %w", err)
	}
	if err := initializeSchema(ctx, s.db, s.now().UTC()); err != nil {
		return err
	}
	generation, err := s.metadata(ctx, "storage_generation")
	if errors.Is(err, sql.ErrNoRows) {
		generation, err = s.randomHex(16)
		if err == nil {
			_, err = s.db.ExecContext(ctx, `INSERT INTO metadata(key, value) VALUES ('storage_generation', ?)`, generation)
		}
	}
	if err != nil {
		return fmt.Errorf("initialize storage generation: %w", err)
	}
	s.generation = generation
	restorePending, err := s.metadata(ctx, "restore_pending")
	if errors.Is(err, sql.ErrNoRows) {
		restorePending, err = "0", nil
	}
	if err != nil {
		return fmt.Errorf("read restore state: %w", err)
	}
	s.restorePending.Store(restorePending == "1")
	if err := s.initializeManagementCredential(ctx); err != nil {
		return err
	}
	return s.initializeStewardWorkerCredential()
}

func (s *Store) initializeStewardWorkerCredential() error {
	path := s.StewardWorkerCredentialPath()
	if err := requireRegularOrAbsent(path); err != nil {
		return fmt.Errorf("secure Steward Worker credential path: %w", err)
	}
	credential, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		token, tokenErr := s.randomToken(32)
		if tokenErr != nil {
			return fmt.Errorf("create Steward Worker credential: %w", tokenErr)
		}
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return fmt.Errorf("create Steward Worker credential file: %w", createErr)
		}
		if _, writeErr := file.WriteString(token + "\n"); writeErr != nil {
			_ = file.Close()
			return fmt.Errorf("write Steward Worker credential: %w", writeErr)
		}
		if syncErr := file.Sync(); syncErr != nil {
			_ = file.Close()
			return fmt.Errorf("sync Steward Worker credential: %w", syncErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("close Steward Worker credential: %w", closeErr)
		}
		credential = []byte(token)
	} else if err != nil {
		return fmt.Errorf("read Steward Worker credential: %w", err)
	}
	credential = trimCredential(credential)
	if len(credential) == 0 {
		return fmt.Errorf("Steward Worker credential is empty")
	}
	if err := secureOwnerPath(path, 0o600); err != nil {
		return fmt.Errorf("secure Steward Worker credential: %w", err)
	}
	s.stewardWorkerSum = sha256.Sum256(credential)
	return nil
}

func (s *Store) initializeManagementCredential(ctx context.Context) error {
	path := filepath.Join(s.dataDir, ManagementCredentialFile)
	if err := requireRegularOrAbsent(path); err != nil {
		return fmt.Errorf("secure management credential path: %w", err)
	}
	credential, readErr := os.ReadFile(path)
	storedDigest, metadataErr := s.metadata(ctx, "management_credential_digest")
	pendingDigest, pendingErr := s.metadata(ctx, "management_credential_digest_pending")
	if pendingErr != nil && !errors.Is(pendingErr, sql.ErrNoRows) {
		return fmt.Errorf("read pending management credential digest: %w", pendingErr)
	}
	if pendingErr == nil && readErr == nil {
		actualDigest := digestString(string(trimCredential(credential)))
		switch {
		case metadataErr == nil && actualDigest == storedDigest:
			if _, err := s.db.ExecContext(ctx, `DELETE FROM metadata WHERE key = 'management_credential_digest_pending'`); err != nil {
				return fmt.Errorf("clear abandoned management credential rotation: %w", err)
			}
		case actualDigest == pendingDigest:
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("recover management credential rotation: %w", err)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO metadata(key, value) VALUES ('management_credential_digest', ?)
				 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, pendingDigest); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("recover management credential rotation: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM metadata WHERE key = 'management_credential_digest_pending'`); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("recover management credential rotation: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("recover management credential rotation: %w", err)
			}
			storedDigest, metadataErr = pendingDigest, nil
		default:
			return fmt.Errorf("management credential does not match current or pending durable state")
		}
	}
	switch {
	case errors.Is(readErr, os.ErrNotExist) && errors.Is(metadataErr, sql.ErrNoRows):
		token, err := s.randomToken(32)
		if err != nil {
			return fmt.Errorf("create management credential: %w", err)
		}
		credential = []byte(token)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("create management credential file: %w", err)
		}
		if _, err := file.Write(append(append([]byte(nil), credential...), '\n')); err != nil {
			_ = file.Close()
			return fmt.Errorf("write management credential: %w", err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return fmt.Errorf("sync management credential: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("write management credential: %w", err)
		}
		storedDigest = digestString(string(credential))
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO metadata(key, value) VALUES ('management_credential_digest', ?)`,
			storedDigest); err != nil {
			return fmt.Errorf("store management credential digest: %w", err)
		}
	case readErr == nil && errors.Is(metadataErr, sql.ErrNoRows):
		credential = trimCredential(credential)
		storedDigest = digestString(string(credential))
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO metadata(key, value) VALUES ('management_credential_digest', ?)`,
			storedDigest); err != nil {
			return fmt.Errorf("recover management credential digest: %w", err)
		}
	case errors.Is(readErr, os.ErrNotExist) && metadataErr == nil:
		return fmt.Errorf("management credential file is missing")
	case readErr != nil:
		return fmt.Errorf("read management credential: %w", readErr)
	case metadataErr != nil:
		return fmt.Errorf("read management credential digest: %w", metadataErr)
	default:
		credential = trimCredential(credential)
		if digestString(string(credential)) != storedDigest {
			return fmt.Errorf("management credential does not match durable state")
		}
	}
	credential = trimCredential(credential)
	if len(credential) == 0 {
		return fmt.Errorf("management credential is empty")
	}
	if err := secureOwnerPath(path, 0o600); err != nil {
		return fmt.Errorf("secure management credential: %w", err)
	}
	decoded, err := hex.DecodeString(storedDigest)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("management credential digest is invalid")
	}
	s.managementMu.Lock()
	copy(s.managementSum[:], decoded)
	s.managementMu.Unlock()
	return nil
}

func trimCredential(value []byte) []byte {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r') {
		value = value[:len(value)-1]
	}
	return value
}

func (s *Store) metadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key = ?`, key).Scan(&value)
	return value, err
}

// AuthenticateManagement validates the local management bearer without
// persisting or logging its raw value.
func (s *Store) AuthenticateManagement(credential string) bool {
	if credential == "" {
		return false
	}
	got := sha256.Sum256([]byte(credential))
	s.managementMu.RLock()
	defer s.managementMu.RUnlock()
	return subtle.ConstantTimeCompare(got[:], s.managementSum[:]) == 1
}

// ManagementCredentialPath returns the owner-only bootstrap credential path.
func (s *Store) ManagementCredentialPath() string {
	return filepath.Join(s.dataDir, ManagementCredentialFile)
}

// AuthenticateStewardWorker validates the least-authority bearer accepted only
// by transported Worker claim/apply/fail routes.
func (s *Store) AuthenticateStewardWorker(credential string) bool {
	if credential == "" {
		return false
	}
	got := sha256.Sum256([]byte(credential))
	return subtle.ConstantTimeCompare(got[:], s.stewardWorkerSum[:]) == 1
}

// StewardWorkerCredentialPath returns the owner-only Worker credential path.
func (s *Store) StewardWorkerCredentialPath() string {
	return filepath.Join(s.dataDir, StewardWorkerCredentialFile)
}

// Ready verifies that the durable authority can answer a query.
func (s *Store) Ready(ctx context.Context) error {
	if s.closing.Load() {
		return fmt.Errorf("appliance is shutting down")
	}
	if s.restorePending.Load() {
		return fmt.Errorf("restored generation awaits operator commit")
	}
	return s.db.PingContext(ctx)
}

func (s *Store) requireMutableGeneration() error {
	if s.closing.Load() {
		return s.serviceError(v1alpha1.ErrorCodeUnavailable, "appliance is shutting down", false)
	}
	if s.restorePending.Load() {
		return s.serviceError(v1alpha1.ErrorCodeUnavailable, "restored generation awaits operator commit", false)
	}
	return nil
}

// Close rejects new calls, drains database users, and releases ownership.
func (s *Store) Close() error {
	if !s.closing.CompareAndSwap(false, true) {
		return nil
	}
	return s.closeResources()
}

func (s *Store) closeResources() error {
	return errors.Join(s.db.Close(), s.lock.close())
}

func (s *Store) serviceError(code v1alpha1.ErrorCode, message string, retryable bool) error {
	requestID := s.requestID.Add(1)
	return &v1alpha1.ServiceError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		RequestID: fmt.Sprintf("request-%08d", requestID),
	}
}

func (s *Store) databaseError(message string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", true)
	}
	return s.serviceError(v1alpha1.ErrorCodeUnavailable, message, true)
}

func (s *Store) randomHex(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (s *Store) randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func digestBytes(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func digestString(value string) string {
	return hex.EncodeToString(digestBytes(value))
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func requireRegularOrAbsent(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}
