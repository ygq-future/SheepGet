//go:build !darwin

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

func setupNativeWindow(window application.Window) {
	// 非 macOS 平台占位
}

func setNativeWindowTheme(window application.Window, mode string) {
	// 非 macOS 平台占位
}
