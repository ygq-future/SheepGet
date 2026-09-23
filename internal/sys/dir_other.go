//go:build !windows

// Package sys 提供系统层级目录与平台环境探测能力。
package sys

import (
	"os"
	"path/filepath"
)

// DefaultDownloadDir returns the conventional Downloads directory.
//
// macOS 与 Linux 的下载目录按平台惯例位于 ~/Downloads。
func DefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	return filepath.Join(home, "Downloads")
}
