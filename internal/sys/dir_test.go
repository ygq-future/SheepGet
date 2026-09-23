package sys

import (
	"testing"
)

func TestDefaultDownloadDir(t *testing.T) {
	dir := DefaultDownloadDir()
	if dir == "" {
		t.Fatal("expected non-empty default download directory")
	}
}
