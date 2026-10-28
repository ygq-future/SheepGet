//go:build !darwin && !windows

package chrome

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// SetupNativeWindow 在非 Windows、非 macOS 平台由窗口管理器处理，此处占位。
func SetupNativeWindow(window application.Window, _ []byte) {
	// 其余平台由系统窗口管理器决定外观，这里保持占位。
}

// SetNativeWindowTheme 在非 Windows、非 macOS 平台由窗口管理器处理，此处占位。
func SetNativeWindowTheme(window application.Window, mode string) {
	// 其余平台由系统窗口管理器决定外观，这里保持占位。
}
