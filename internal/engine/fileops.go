package engine

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"sheep-get/internal/task"
)

// DefaultFilename 是 URL 与服务器都没有给出文件名时使用的兜底名称。
const DefaultFilename = "download.bin"

// CleanFilename 清理任意外部传入的文件名，剥离 Windows (\) 与 POSIX (/) 路径分隔符，
// 去除首尾空白与包裹引号，防范绝对路径与目录穿越，仅保留纯文件名。
func CleanFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, `"'`)
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// 统一转为 / 后取 base，防范各种目录穿越
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "" || name == "." || name == "/" || name == ".." {
		return ""
	}
	return name
}

func sanitizeDispositionFilename(name string) string {
	return CleanFilename(name)
}

// ParseContentDispositionFilename 从 Content-Disposition 标头或参数值中提取文件名。
// 优先提取并解码 RFC 5987 / RFC 6266 规范的 filename*=，若无则提取普通 filename=。
func ParseContentDispositionFilename(cd string) string {
	if cd == "" {
		return ""
	}
	parts := strings.Split(cd, ";")
	var fallbackName string

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		lower := strings.ToLower(trimmed)

		// 1. 优先处理 filename*=（RFC 5987 / RFC 6266）
		if strings.HasPrefix(lower, "filename*=") {
			val := strings.TrimSpace(trimmed[len("filename*="):])
			val = strings.Trim(val, `"'`)
			// 格式通常为: UTF-8''encoded_name 或 charset'lang'encoded_name
			quoteIdx1 := strings.Index(val, "'")
			if quoteIdx1 != -1 {
				quoteIdx2 := strings.Index(val[quoteIdx1+1:], "'")
				if quoteIdx2 != -1 {
					quoteIdx2 += quoteIdx1 + 1
					encoded := val[quoteIdx2+1:]
					if unescaped, err := url.QueryUnescape(encoded); err == nil {
						if clean := sanitizeDispositionFilename(unescaped); clean != "" {
							return clean
						}
					} else if unescaped, err := url.PathUnescape(encoded); err == nil {
						if clean := sanitizeDispositionFilename(unescaped); clean != "" {
							return clean
						}
					}
				}
			}
			if unescaped, err := url.QueryUnescape(val); err == nil {
				if clean := sanitizeDispositionFilename(unescaped); clean != "" {
					return clean
				}
			}
		}

		// 2. 普通 filename=
		if fallbackName == "" && strings.HasPrefix(lower, "filename=") {
			val := strings.TrimSpace(trimmed[len("filename="):])
			val = strings.Trim(val, `"'`)
			if strings.Contains(val, "%") {
				if unescaped, err := url.QueryUnescape(val); err == nil && unescaped != "" {
					val = unescaped
				}
			}
			if clean := sanitizeDispositionFilename(val); clean != "" {
				fallbackName = clean
			}
		}
	}

	return fallbackName
}

// URLFilename 提取 URL 中的文件名。
// 优先解析 Query 中的 response-content-disposition、rscd、filename 等云存储/直链参数；
// 若未提供或未解析出有效名称，则回退提取 URL 路径中的末尾段（path.Base）。
func URLFilename(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// 1. 尝试解析 Query 参数中的文件名信息
	if u, err := url.Parse(rawURL); err == nil {
		q := u.Query()
		if len(q) > 0 {
			lowerQuery := make(map[string]string, len(q))
			for k, v := range q {
				if len(v) > 0 && strings.TrimSpace(v[0]) != "" {
					lowerQuery[strings.ToLower(k)] = v[0]
				}
			}

			// a. 优先从 Content-Disposition 类参数解析（如 S3、Azure Blob、GitHub Release Assets 等）
			for _, key := range []string{"response-content-disposition", "rscd"} {
				if val, ok := lowerQuery[key]; ok {
					if fn := ParseContentDispositionFilename(val); fn != "" {
						return fn
					}
				}
			}

			// b. 检查直接带有 filename 的参数（如 filename、file_name、attname）
			for _, key := range []string{"filename", "file_name", "attname"} {
				if val, ok := lowerQuery[key]; ok {
					val = strings.Trim(strings.TrimSpace(val), `"'`)
					if strings.Contains(val, "%") {
						if unescaped, err := url.QueryUnescape(val); err == nil && unescaped != "" {
							val = unescaped
						}
					}
					if clean := sanitizeDispositionFilename(val); clean != "" {
						return clean
					}
				}
			}
		}
	}

	// 2. 回退到提取 URL 路径末尾部分
	clean, _, _ := strings.Cut(rawURL, "?")
	clean, _, _ = strings.Cut(clean, "#")
	base := path.Base(clean)
	if base == "" || base == "/" || base == "." {
		return ""
	}
	if strings.Contains(base, "%") {
		if unescaped, err := url.PathUnescape(base); err == nil && unescaped != "" && unescaped != "/" && unescaped != "." {
			return unescaped
		}
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

	_ = srcFile.Close()
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

// RemoveTaskFiles 删除任务的全部磁盘产物：落点上的成品与同名分片、HLS 任务的分片目录，
// 以及可能位于独立临时目录中的分片。
// GetPartPath 在配置了独立临时目录时把分片放在别处，只清理落点旁的文件会留下无人引用的孤儿分片。
func (m *Manager) RemoveTaskFiles(t *task.Task) {
	if t == nil || t.Directory == "" || t.Filename == "" {
		return
	}
	removeDestinationFiles(t.Directory, t.Filename)
	if partPath := m.GetPartPath(t); partPath != filepath.Join(t.Directory, t.Filename+".sheepget") {
		_ = os.Remove(partPath)
	}
	if t.IsHLS() {
		m.removeSegmentDir(t)
	}
}
