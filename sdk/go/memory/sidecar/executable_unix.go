//go:build !windows

package sidecar

import "os"

func isExecutableFile(info os.FileInfo, _ string) bool {
	return info.Mode().IsRegular() && info.Mode()&0o111 != 0
}
