//go:build windows

package sidecar

import (
	"os"
	"path/filepath"
	"strings"
)

func isExecutableFile(info os.FileInfo, path string) bool {
	return info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(path), ".exe")
}
