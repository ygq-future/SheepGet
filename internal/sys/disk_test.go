package sys

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAvailableDiskSpaceAndPreFlightCheck(t *testing.T) {
	tmp := t.TempDir()

	// 1. 真实系统查询：对于已存在目录及尚未创建的子路径，应能回溯父目录并查出非零可用空间
	avail, err := AvailableDiskSpace(filepath.Join(tmp, "not-yet-created", "sub", "file.bin"))
	if err != nil {
		t.Fatalf("expected AvailableDiskSpace to succeed on non-existent child path, got: %v", err)
	}
	if avail == 0 {
		t.Fatal("expected positive available disk space on temp volume")
	}

	// 2. 注入受控空间返回：验证足够空间放行、空间不足返回 ErrInsufficientDiskSpace
	restore := SetAvailableDiskSpaceForTest(func(dir string) (uint64, error) {
		return 1024, nil
	})
	defer restore()

	if err := CheckDiskSpace(tmp, 0); err != nil {
		t.Fatalf("expected 0 required bytes to pass, got: %v", err)
	}
	if err := CheckDiskSpace(tmp, 1024); err != nil {
		t.Fatalf("expected exact available bytes (1024) to pass, got: %v", err)
	}
	err = CheckDiskSpace(tmp, 1025)
	if !errors.Is(err, ErrInsufficientDiskSpace) {
		t.Fatalf("expected ErrInsufficientDiskSpace when required (1025) > available (1024), got: %v", err)
	}
}
