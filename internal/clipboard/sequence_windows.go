//go:build windows

package clipboard

import "syscall"

var (
	moduser32                      = syscall.NewLazyDLL("user32.dll")
	procGetClipboardSequenceNumber = moduser32.NewProc("GetClipboardSequenceNumber")
)

// getClipboardSequence returns the Windows clipboard sequence number.
// It is incremented by the OS whenever clipboard content changes.
// Calling it takes < 1 microsecond, allocates 0 bytes, and requires no OpenClipboard lock.
func getClipboardSequence() uint32 {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(r)
}

func hasSequenceSupport() bool {
	return true
}
