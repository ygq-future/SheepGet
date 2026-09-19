//go:build !windows

package main

import "unsafe"

// forceForegroundWindow 在非 Windows 平台不做额外处理：前台锁定是 Windows 的行为，
// 其它平台沿用 Wails 自己的 Show + Focus。（macOS / Linux 上是否同样需要补一步尚未实测，
// 见 docs/agents/quality.md 的能力矩阵。）
func forceForegroundWindow(_ unsafe.Pointer) {}
