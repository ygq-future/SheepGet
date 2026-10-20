//go:build !windows

package main

// systemDarkMode 只有 Windows 需要：那里要把系统明暗解析成 DWM 窗口属性。
// 其余平台由系统窗口管理器直接处理，原生窗口不做这项映射。
func systemDarkMode() bool {
	return false
}
