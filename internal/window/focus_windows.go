//go:build windows

package window

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// 「弹出来就要在最前面」在 Windows 上不是 Show + Focus 就能做到的：当前前台窗口属于浏览器时，
// 系统会拒绝后台进程的 SetForegroundWindow（前台锁定），表现就是窗口已经显示但仍压在浏览器后面，
// 用户必须再点一次任务栏。
//
// 标准绕过办法是把自己的线程临时接入当前前台线程的输入队列，借此取得前台激活许可，激活窗口后
// 立刻解链；随后 BringWindowToTop 再把它提到 z 序最前，这样即使激活仍被拒（浏览器刚抢到前台的
// 那一瞬），窗口也已经盖在浏览器上面。两者都只抬高这一次，不改变窗口的置顶属性。
var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsIconic                 = user32.NewProc("IsIconic")
	procSetFocus                 = user32.NewProc("SetFocus")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
)

// swRestore 对应 SW_RESTORE。
const swRestore = 9

// forceForegroundWindow 把窗口带到前台并提到 z 序最前。
//
// 必须在 Wails 的主线程上执行：窗口属于主线程，AttachThreadInput 要对齐的正是「运行着输入队列的
// 那个线程」。下载交接是从 HTTP 处理协程、剪贴板协程里来的，在那里直接调用等于用错误的线程申请
// 前台激活，系统照样拒绝；所以要先把这一步派发回主线程。
func forceForegroundWindow(handle unsafe.Pointer) {
	hwnd := windows.HWND(uintptr(handle))
	if hwnd == 0 {
		return
	}
	application.InvokeSync(func() { activateWindow(hwnd) })
}

// 下面的调用失败都不改变结论（窗口已经显示出来，最差只是不在最前），没有对应的恢复动作，
// 因此不逐个判断返回值。
func activateWindow(hwnd windows.HWND) {
	// 最小化的窗口先还原，否则激活只会让它在任务栏闪一下。
	if iconic, _, _ := procIsIconic.Call(uintptr(hwnd)); iconic != 0 {
		_, _, _ = procShowWindow.Call(uintptr(hwnd), swRestore)
	}

	foreground, _, _ := procGetForegroundWindow.Call()
	// GetWindowThreadProcessId 的第二个参数允许传 NULL（这里不需要进程号）。
	targetThread, _, _ := procGetWindowThreadProcessID.Call(foreground, 0)
	currentThread := uintptr(windows.GetCurrentThreadId())

	attached := false
	if foreground != 0 && foreground != uintptr(hwnd) && targetThread != 0 && targetThread != currentThread {
		if ok, _, _ := procAttachThreadInput.Call(currentThread, targetThread, 1); ok != 0 {
			attached = true
		}
	}

	_, _, _ = procSetForegroundWindow.Call(uintptr(hwnd))
	_, _, _ = procBringWindowToTop.Call(uintptr(hwnd))
	_, _, _ = procSetFocus.Call(uintptr(hwnd))

	if attached {
		_, _, _ = procAttachThreadInput.Call(currentThread, targetThread, 0)
	}
}
