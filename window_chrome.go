package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"sheep-get/internal/config"
)

// nativeWindowThemeDark 把界面主题映射成原生窗口的明暗结果：跟随系统时用调用方读到的
// 系统状态，显式主题则固定，不被系统切换改写。
func nativeWindowThemeDark(mode config.ThemeMode, systemDark bool) bool {
	switch mode {
	case config.ThemeDark:
		return true
	case config.ThemeLight:
		return false
	default:
		return systemDark
	}
}

// nativeWindowTheme 是创建期交给 Wails 的明暗选择：这里从不返回 SystemDefault，
// 否则 Wails 会自己注册系统主题监听并在系统切换时改写用户显式选择的主题。
// 「跟随系统」由调用方解析成本次系统状态后同样落成 Dark/Light，后续更新只走
// setNativeWindowTheme 这一条路径。
func nativeWindowTheme(mode config.ThemeMode, systemDark bool) application.Theme {
	if nativeWindowThemeDark(mode, systemDark) {
		return application.Dark
	}
	return application.Light
}

func immersiveWindowChrome(goos string) (frameless bool, translucent bool, windows application.WindowsWindow, mac application.MacWindow) {
	switch goos {
	case "windows":
		return true, false, application.WindowsWindow{BackdropType: application.None, WebView2CompositionHosting: true}, application.MacWindow{}
	case "darwin":
		return false, true, application.WindowsWindow{}, application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				Hide:                 false,
				HideTitle:            true,
				FullSizeContent:      true,
				UseToolbar:           false,
				HideToolbarSeparator: true,
			},
			InvisibleTitleBarHeight: 0,
		}
	default:
		return false, false, application.WindowsWindow{}, application.MacWindow{}
	}
}
