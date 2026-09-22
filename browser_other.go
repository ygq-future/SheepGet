//go:build !windows

package main

// findBrowserExe 在非 Windows 平台不参与路径探测，直接由系统启动命令唤起浏览器。
func findBrowserExe(browser string) string {
	return ""
}
