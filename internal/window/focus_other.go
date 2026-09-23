//go:build !windows

package window

import "unsafe"

// forceForegroundWindow 在非 Windows 平台无需额外处理前台锁。
func forceForegroundWindow(_ unsafe.Pointer) {}
