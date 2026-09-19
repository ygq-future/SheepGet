package main

import "github.com/wailsapp/wails/v3/pkg/application"

// raiseWindow 把窗口带到前台。
//
// Wails 的 Focus 在 Windows 上就是 SetForegroundWindow，浏览器占着前台时会被系统拒绝，
// 因此这里再补一步平台专用的前台激活（见 windowfocus_windows.go）。它只抬高这一次，
// 不改变窗口的常驻置顶属性。
func raiseWindow(win application.Window) {
	if win == nil {
		return
	}
	win.Focus()
	if webviewWin, ok := win.(*application.WebviewWindow); ok {
		forceForegroundWindow(webviewWin.NativeWindow())
	}
}

// showAndRaise 显示窗口并立刻把它带到前台。这两件事是一个动作的两半：Wails 的 Show 在 Windows 上
// 只是 ShowWindow(SW_SHOW)，窗口会显示，但在浏览器正占着前台时会落在它后面，所以「让用户看见这个
// 窗口」的调用点必须走这里，而不是只调 Show。
func showAndRaise(win application.Window) {
	if win == nil {
		return
	}
	win.Show()
	raiseWindow(win)
}
