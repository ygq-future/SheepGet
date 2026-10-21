//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"

	"sheep-get/internal/config"
)

// setupNativeWindow 为无边框窗口设置原生 Windows 应用程序图标，
// 确保在开发环境（bun dev）及无 .syso 资源注入时任务栏和 Alt+Tab 稳定显示图标。
func setupNativeWindow(window application.Window) {
	if window == nil || len(trayIcon) == 0 {
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
	hwnd := w32.HWND(uintptr(ptr))

	application.InvokeSync(func() {
		// ICON_BIG = 1 (任务栏与 Alt+Tab，通常为 32x32 或以上)
		if bigIcon, err := createHIconFromICO(trayIcon, 32); err == nil && bigIcon != 0 {
			w32.SendMessage(hwnd, w32.WM_SETICON, 1, uintptr(bigIcon))
		}
		// ICON_SMALL = 0 (标题栏与小图标，通常为 16x16)
		if smallIcon, err := createHIconFromICO(trayIcon, 16); err == nil && smallIcon != 0 {
			w32.SendMessage(hwnd, w32.WM_SETICON, 0, uintptr(smallIcon))
		}
	})
}

func createHIconFromICO(data []byte, desiredWidth int) (w32.HICON, error) {
	if len(data) < 6 {
		return 0, fmt.Errorf("invalid ico data")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if len(data) < 6+count*16 {
		return 0, fmt.Errorf("truncated ico header")
	}

	bestIdx := -1
	bestDiff := 999999
	for i := 0; i < count; i++ {
		off := 6 + i*16
		w := int(data[off])
		if w == 0 {
			w = 256
		}
		diff := w - desiredWidth
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			bestIdx = i
		}
	}

	if bestIdx == -1 {
		return 0, fmt.Errorf("no icon found")
	}

	entryOff := 6 + bestIdx*16
	imgLen := binary.LittleEndian.Uint32(data[entryOff+8 : entryOff+12])
	imgOff := binary.LittleEndian.Uint32(data[entryOff+12 : entryOff+16])

	if int(imgOff+imgLen) > len(data) {
		return 0, fmt.Errorf("truncated icon resource")
	}

	resBits := data[imgOff : imgOff+imgLen]
	hicon, err := w32.CreateIconFromResourceEx(
		uintptr(unsafe.Pointer(&resBits[0])),
		imgLen,
		true,
		0x00030000,
		desiredWidth,
		desiredWidth,
		0,
	)
	if err != nil {
		return 0, err
	}
	return w32.HICON(hicon), nil
}

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
