//go:build windows

package appliance

// factsPerfRSSBytes is unavailable on Windows without a platform-specific
// process query; the harness reports -1 instead of inventing a value.
func factsPerfRSSBytes() int64 { return -1 }
