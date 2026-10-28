//go:build !windows

package chrome

// SystemDarkMode 报告系统当前是否为深色模式。只有 Windows 需要手动查询以设置 DWM 属性；其余平台由窗口管理器处理。
func SystemDarkMode() bool {
	return false
}
