//go:build !windows

package appliance

import (
	"os"
)

func secureOwnerPath(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}
