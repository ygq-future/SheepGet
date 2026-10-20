//go:build windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"

	"sheep-get/internal/config"
)

// setupNativeWindow 在 Windows 上没有额外的创建期工作：磨砂底与明暗选择都由
// webviewOptions 里的 WindowsWindow 配置一次性交给 DWM 和 WebView2 完成。
func setupNativeWindow(window application.Window) {}

// setNativeWindowTheme 让原生窗口跟随界面主题：DWM 的沉浸式深色属性同时决定
// 标题栏/边框和 Acrylic 磨砂的底色，不设置它时磨砂会一直停在系统主题上。
// 用户显式选择的 dark/light 必须在这里覆盖系统值；「跟随系统」则读取当前系统状态，
// 由 ThemeChanged 回调重新调用本函数完成切换。
func setNativeWindowTheme(window application.Window, mode string) {
	if window == nil {
		return
	}
	wvWin, ok := window.(*application.WebviewWindow)
	if !ok {
		return
	}
	ptr := wvWin.NativeWindow()
	if ptr == nil {
		return
	}

	dark := nativeWindowThemeDark(config.ThemeMode(mode), systemDarkMode())
	hwnd := w32.HWND(uintptr(ptr))

	// DWM 与 uxtheme 的窗口属性只能在拥有该窗口的 GUI 线程上修改。
	application.InvokeSync(func() {
		w32.SetTheme(hwnd, dark)
		if w32.AllowDarkModeForWindow != nil {
			w32.AllowDarkModeForWindow(hwnd, dark)
		}
		w32.InvalidateRect(hwnd, nil, true)
	})
}

// systemDarkMode 报告 Windows 当前的深色状态。窗口设置可能在应用环境尚未建立前发生，
// 因此这里直接读注册表而不是通过 Wails 的 Env。
func systemDarkMode() bool {
	return w32.IsCurrentlyDarkMode()
}
