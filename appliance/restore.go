package appliance

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	core "github.com/caelis-labs/memory/internal/appliance"
)

// RestoreOptions describes an authenticated offline installation of a
// Memory-owned snapshot. The caller must stop every process that can own the
// data directory before calling Restore.
type RestoreOptions struct {
	DataDir              string
	Snapshot             io.Reader
	ManagementCredential string
	Clock                func() time.Time
	Random               io.Reader
}

// OfflineRestoreOptions describes an owner-authorized restore without
// exposing the local management credential to an embedding. The appliance
// reads the owner-only credential from its protected path before entering the
// underlying offline operation, which then authenticates it while holding the
// data-directory lock.
type OfflineRestoreOptions struct {
	DataDir  string
	Snapshot io.Reader
	Clock    func() time.Time
	Random   io.Reader
}

// RestoreResult reports the generation transition without exposing the
// appliance database or schema internals.
type RestoreResult struct {
	SourceGeneration  string `json:"source_generation"`
	StorageGeneration string `json:"storage_generation"`
	SchemaVersion     int    `json:"schema_version"`
	RollbackAvailable bool   `json:"rollback_available"`
	RollbackPath      string `json:"rollback_path,omitempty"`
}

// PrepareUpgrade records an exact stopped generation and gates future writes
// until the upgraded process explicitly commits it.
func PrepareUpgrade(ctx context.Context, dataDir, managementCredential string) (RestoreResult, error) {
	result, err := core.PrepareUpgrade(ctx, dataDir, managementCredential)
	return fromRestoreResult(result), err
}

// PrepareUpgradeOwned captures the stopped generation using the owner-only
// management credential stored inside the appliance data directory.
func PrepareUpgradeOwned(ctx context.Context, dataDir string) (RestoreResult, error) {
	credential, err := readManagementCredential(dataDir)
	if err != nil {
		return RestoreResult{}, err
	}
	return PrepareUpgrade(ctx, dataDir, credential)
}

// Restore installs and validates one authenticated snapshot while the
// appliance is offline. The prior database remains available as an
// appliance-owned rollback image until CommitRestore or RollbackRestore.
func Restore(ctx context.Context, options RestoreOptions) (RestoreResult, error) {
	result, err := core.Restore(ctx, core.RestoreOptions{
		DataDir:              options.DataDir,
		Snapshot:             options.Snapshot,
		ManagementCredential: options.ManagementCredential,
		Clock:                options.Clock,
		Random:               options.Random,
	})
	return fromRestoreResult(result), err
}

// RestoreOwned installs one owner-authenticated snapshot while the appliance
// is offline. The prior generation remains available through the appliance's
// rollback contract until CommitRestoreOwned or RollbackRestoreOwned.
func RestoreOwned(ctx context.Context, options OfflineRestoreOptions) (RestoreResult, error) {
	credential, err := readManagementCredential(options.DataDir)
	if err != nil {
		return RestoreResult{}, err
	}
	return Restore(ctx, RestoreOptions{
		DataDir:              options.DataDir,
		Snapshot:             options.Snapshot,
		ManagementCredential: credential,
		Clock:                options.Clock,
		Random:               options.Random,
	})
}

// RollbackRestore reinstalls the pre-restore generation while the appliance is
// offline. It rotates the generation so stale capabilities cannot cross the
// recovery boundary.
func RollbackRestore(ctx context.Context, dataDir, managementCredential string, random io.Reader) (RestoreResult, error) {
	result, err := core.RollbackRestore(ctx, dataDir, managementCredential, random)
	return fromRestoreResult(result), err
}

// RollbackRestoreOwned restores the appliance-owned pre-restore generation
// without exposing its management credential to an embedding.
func RollbackRestoreOwned(ctx context.Context, dataDir string, random io.Reader) (RestoreResult, error) {
	credential, err := readManagementCredential(dataDir)
	if err != nil {
		return RestoreResult{}, err
	}
	return RollbackRestore(ctx, dataDir, credential, random)
}

// CommitRestore accepts a restored generation while the appliance is offline.
func CommitRestore(dataDir, managementCredential string) error {
	return core.CommitRestore(dataDir, managementCredential)
}

// CommitRestoreOwned accepts the current restored generation using the
// appliance's owner-only management credential.
func CommitRestoreOwned(dataDir string) error {
	credential, err := readManagementCredential(dataDir)
	if err != nil {
		return err
	}
	return CommitRestore(dataDir, credential)
}

func readManagementCredential(dataDir string) (string, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return "", os.ErrInvalid
	}
	path := filepath.Join(dataDir, core.ManagementCredentialFile)
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect owner management credential: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("owner management credential is not a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read owner management credential: %w", err)
	}
	credential := strings.TrimSpace(string(raw))
	if credential == "" {
		return "", fmt.Errorf("owner management credential is empty")
	}
	return credential, nil
}

func fromRestoreResult(result core.RestoreResult) RestoreResult {
	return RestoreResult{
		SourceGeneration:  result.SourceGeneration,
		StorageGeneration: result.StorageGeneration,
		SchemaVersion:     result.SchemaVersion,
		RollbackAvailable: result.RollbackAvailable,
		RollbackPath:      result.RollbackPath,
	}
}
