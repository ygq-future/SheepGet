//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// findBrowserExe 定位 Chrome 或 Edge 的可执行文件；找不到（或浏览器不受支持）时返回空串，
// 由调用方回退到 shell 启动。
func findBrowserExe(browser string) string {
	var vendor string
	var exeName string
	switch strings.ToLower(strings.TrimSpace(browser)) {
	case "chrome":
		vendor, exeName = filepath.Join("Google", "Chrome"), "chrome.exe"
	case "edge":
		vendor, exeName = filepath.Join("Microsoft", "Edge"), "msedge.exe"
	default:
		return ""
	}

	regKey := `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + exeName
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, regKey, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		val, _, err := k.GetStringValue("")
		_ = k.Close()
		if err == nil && val != "" {
			if info, statErr := os.Stat(val); statErr == nil && !info.IsDir() {
				return val
			}
		}
	}

	for _, envVar := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		prefix := os.Getenv(envVar)
		if prefix == "" {
			continue
		}
		cand := filepath.Join(prefix, vendor, "Application", exeName)
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}

	return ""
}
