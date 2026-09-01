//go:build windows

package appliance

import "golang.org/x/sys/windows"

func storageCapacity(path string) (uint64, uint64, error) {
	value, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(value, &available, &total, &free); err != nil {
		return 0, 0, err
	}
	return total, available, nil
}
