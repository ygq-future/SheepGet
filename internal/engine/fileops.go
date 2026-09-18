package engine

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"sheep-get/internal/task"
)

// DefaultFilename 是 URL 与服务器都没有给出文件名时使用的兜底名称。
const DefaultFilename = "download.bin"

// URLFilename 提取 URL 路径中的文件名，例如 https://host/a/b.mp4 得到 b.mp4。
// URL 只用 "/" 作为路径分隔符，因此这里使用 path.Base 而不是随平台变化的 filepath.Base；
// 路径中没有可用文件名时返回空字符串，由调用方决定兜底名称或继续等待探测结果。
func URLFilename(rawURL string) string {
	clean, _, _ := strings.Cut(rawURL, "?")
	base := path.Base(clean)
	if base == "" || base == "/" || base == "." {
		return ""
	}
	return base
}

// moveFile 把 srcPath 移动到 dstPath：同一文件系统内用 rename，rename 失败时退化为跨设备复制。
// 失败时保留 srcPath，由调用方决定是报告错误还是丢弃已有数据；成功时 srcPath 不再存在。
func moveFile(srcPath, dstPath string) error {
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	}
	return safeTransferCrossDevice(srcPath, dstPath)
}

// safeTransferCrossDevice transfers a file across filesystems or disks by copying to a temporary
// file in the destination directory, syncing, closing, and renaming before removing the source.
// If any step fails, srcPath is strictly preserved to prevent data loss.
func safeTransferCrossDevice(srcPath, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source temp file: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	transferPath := dstPath + ".transferring"
	dstFile, err := os.OpenFile(transferPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}

	buf := make([]byte, 256*1024)
	_, copyErr := io.CopyBuffer(dstFile, srcFile, buf)
	syncErr := dstFile.Sync()
	closeErr := dstFile.Close()

	if copyErr != nil {
		_ = os.Remove(transferPath)
		return fmt.Errorf("copy failed: %w", copyErr)
	}
	if syncErr != nil {
		_ = os.Remove(transferPath)
		return fmt.Errorf("sync failed: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(transferPath)
		return fmt.Errorf("close failed: %w", closeErr)
	}

	if err := os.Rename(transferPath, dstPath); err != nil {
		_ = os.Remove(transferPath)
		return fmt.Errorf("rename to target destination failed: %w", err)
	}

	_ = os.Remove(srcPath)
	return nil
}

// removeDestinationFiles 删除一个落点上的成品文件与同名分片。
// 重新下载、重置链接等场景需要在写入新数据前腾空目标位置。
func removeDestinationFiles(dir, filename string) {
	if dir == "" || filename == "" {
		return
	}
	destPath := filepath.Join(dir, filename)
	_ = os.Remove(destPath)
	_ = os.Remove(destPath + ".sheepget")
}

// RemoveTaskFiles 删除任务的全部磁盘产物：落点上的成品与同名分片，以及可能位于独立临时目录中的分片。
// GetPartPath 在配置了独立临时目录时把分片放在别处，只清理落点旁的文件会留下无人引用的孤儿分片。
func (m *Manager) RemoveTaskFiles(t *task.Task) {
	if t == nil || t.Directory == "" || t.Filename == "" {
		return
	}
	removeDestinationFiles(t.Directory, t.Filename)
	if partPath := m.GetPartPath(t); partPath != filepath.Join(t.Directory, t.Filename+".sheepget") {
		_ = os.Remove(partPath)
	}
}
