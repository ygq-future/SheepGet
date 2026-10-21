//go:build !darwin && !windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

func setupNativeWindow(window application.Window) {
	// 其余平台由系统窗口管理器决定外观，这里保持占位。
}

func setNativeWindowTheme(window application.Window, mode string) {
	// 其余平台由系统窗口管理器决定外观，这里保持占位。
}
