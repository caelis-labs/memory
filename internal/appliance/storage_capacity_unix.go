//go:build !windows

package appliance

import "golang.org/x/sys/unix"

func storageCapacity(path string) (uint64, uint64, error) {
	var filesystem unix.Statfs_t
	if err := unix.Statfs(path, &filesystem); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(filesystem.Bsize)
	return uint64(filesystem.Blocks) * blockSize, uint64(filesystem.Bavail) * blockSize, nil
}
