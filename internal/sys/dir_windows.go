//go:build windows

// Package sys 提供系统层级目录与平台环境探测能力。
package sys

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// DefaultDownloadDir detects the actual system Downloads directory.
//
// Windows 允许用户把「下载」重定向到任意卷，权威位置由 Shell 的 User Shell
// Folders 记录，因此注册表是该路径的唯一来源；读不到时回退到 ~/Downloads。
func DefaultDownloadDir() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.QUERY_VALUE)
	if err == nil {
		defer func() { _ = key.Close() }()
		// {374DE290-123F-4565-9164-39C4925E467B} 是 Downloads 的标准 KNOWNFOLDERID。
		for _, valName := range []string{"{374DE290-123F-4565-9164-39C4925E467B}", "{7D83EE9B-2244-4E70-B1F5-5393042AF1E4}"} {
			val, _, err := key.GetStringValue(valName)
			if err == nil && val != "" {
				expanded := os.ExpandEnv(val)
				if info, err := os.Stat(expanded); err == nil && info.IsDir() {
					return expanded
				}
			}
		}
	}

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
