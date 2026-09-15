package main

import (
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/windows/registry"
)

// getDefaultDownloadDir detects the actual system Downloads directory
func getDefaultDownloadDir() string {
	if runtime.GOOS == "windows" {
		// Read Windows Registry for User Shell Folders
		key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.QUERY_VALUE)
		if err == nil {
			defer func() { _ = key.Close() }()
			// {374DE290-123F-4565-9164-39C4925E467B} is the standard GUID for Downloads
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

// isSystemDarkMode detects whether Windows is currently using dark theme for apps.
func isSystemDarkMode() bool {
	if runtime.GOOS == "windows" {
		key, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
		if err == nil {
			defer func() { _ = key.Close() }()
			val, _, err := key.GetIntegerValue("AppsUseLightTheme")
			if err == nil {
				return val == 0
			}
		}
	}
	return false
}
