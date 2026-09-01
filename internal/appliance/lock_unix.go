//go:build !windows

package appliance

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockOwnerFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return errOwnerLockContended
	}
	return err
}

func unlockOwnerFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
