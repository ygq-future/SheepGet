//go:build !windows

package clipboard

func getClipboardSequence() uint32 {
	return 0
}

func hasSequenceSupport() bool {
	return false
}
