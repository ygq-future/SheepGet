//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// getDefaultDownloadDir returns the conventional Downloads directory.
//
// macOS 与 Linux 的下载目录按平台惯例位于 ~/Downloads。
func getDefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	downloads := filepath.Join(home, "Downloads")
	if info, err := os.Stat(downloads); err == nil && info.IsDir() {
		return downloads
	}

	return downloads
}
