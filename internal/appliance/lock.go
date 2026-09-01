package appliance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type ownerLock struct {
	file *os.File
}

func acquireOwnerLock(dataDir string) (*ownerLock, error) {
	path := filepath.Join(dataDir, OwnerLockFilename)
	if err := requireRegularOrAbsent(path); err != nil {
		return nil, fmt.Errorf("secure owner lock path: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open owner lock: %w", err)
	}
	if err := secureOwnerPath(path, 0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure owner lock: %w", err)
	}
	if err := lockOwnerFile(file); err != nil {
		_ = file.Close()
		if errors.Is(err, errOwnerLockContended) {
			return nil, ErrOwnerLocked
		}
		return nil, fmt.Errorf("acquire owner lock: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		_ = releaseOwnerLock(file)
		return nil, fmt.Errorf("truncate owner lock: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = releaseOwnerLock(file)
		return nil, fmt.Errorf("write owner lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = releaseOwnerLock(file)
		return nil, fmt.Errorf("sync owner lock: %w", err)
	}
	return &ownerLock{file: file}, nil
}

func (l *ownerLock) close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := releaseOwnerLock(l.file)
	l.file = nil
	return err
}

func releaseOwnerLock(file *os.File) error {
	unlockErr := unlockOwnerFile(file)
	closeErr := file.Close()
	return errors.Join(unlockErr, closeErr)
}
