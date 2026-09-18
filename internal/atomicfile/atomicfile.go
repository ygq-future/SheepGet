// Package atomicfile 提供「先写临时文件、再改名覆盖」的原子写入，
// 避免进程中断时在目标路径留下半截文件。
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

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

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	return nil
}
