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

// isSystemDarkMode reports whether the desktop is currently using the dark theme.
//
// macOS 与 Linux 上系统主题不作为主题来源，仍由配置中心决定；
// 因此 system 模式在这些平台按浅色解析。
func isSystemDarkMode() bool {
	return false
}
