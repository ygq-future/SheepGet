// Package atomicfile 提供「先写临时文件、再改名覆盖」的原子写入，
// 避免进程中断时在目标路径留下半截文件。
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	renameMaxAttempts = 5
	renameBaseDelay   = 25 * time.Millisecond
)

// Rename 把 src 原子重命名为 dst。
// 在 Windows 下，杀毒软件（如 Windows Defender）实时扫描或资源管理器缩略图索引极易在文件刚写完关闭的瞬间
// 短暂持有句柄（触发 ERROR_SHARING_VIOLATION / ERROR_ACCESS_DENIED）。若属于源文件不存在、跨设备（EXDEV）
// 或目标是目录等确定性错误则立即返回，否则通过指数退避重试平滑渡过瞬态句柄占用。
func Rename(src, dst string) error {
	var lastErr error
	for attempt := 0; attempt < renameMaxAttempts; attempt++ {
		err := os.Rename(src, dst)
		if err == nil {
			return nil
		}
		lastErr = err
		if isPermanentRenameError(err, dst) {
			return lastErr
		}
		if attempt+1 < renameMaxAttempts {
			time.Sleep(renameBaseDelay << attempt)
		}
	}
	return lastErr
}

func isPermanentRenameError(err error, dst string) bool {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.EISDIR) || errors.Is(err, syscall.EINVAL) {
		return true
	}
	// Windows ERROR_NOT_SAME_DEVICE = 17 (0x11)
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		if errno, ok := linkErr.Err.(syscall.Errno); ok && errno == 17 {
			return true
		}
	}
	if info, statErr := os.Stat(dst); statErr == nil && info.IsDir() {
		return true
	}
	return false
}

// Write 把 data 原子写入 path：先在同一目录写临时文件，再用 rename 覆盖目标。
//
// sync 为 true 时在改名之前把数据刷到磁盘，用于「写完这一次后可能立刻断电」的低频写入
// （例如用户点保存的配置）；高频写入（例如任务进度持久化）不应开启，否则每次写入都要等待磁盘同步。
func Write(path string, data []byte, perm os.FileMode, sync bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpName := tmp.Name()

	discard := func(action string, cause error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("%s: %w", action, cause)
	}

	if err := tmp.Chmod(perm); err != nil {
		return discard("failed to set temporary file permissions", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return discard("failed to write temporary file", err)
	}
	if sync {
		if err := tmp.Sync(); err != nil {
			return discard("failed to sync temporary file", err)
		}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	if err := Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	return nil
}
