//go:build !windows

package appliance

import "syscall"

// factsPerfRSSBytes reports the peak resident set size of this test process.
// It is a measured observation on the host that ran the harness, not a
// portable release budget.
func factsPerfRSSBytes() int64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return -1
	}
	// Darwin reports Maxrss in bytes; Linux reports it in kibibytes.
	if usage.Maxrss < 0 {
		return -1
	}
	return usage.Maxrss
}
