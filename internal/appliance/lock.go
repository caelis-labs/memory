package appliance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure owner lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
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
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	return errors.Join(unlockErr, closeErr)
}
