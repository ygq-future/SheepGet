// Package logging 提供进程内唯一的日志出口：一条按大小滚动的文件流。
//
// 日志要能在出问题之后回答「当时到底发生了什么」，因此写入必须完整、有序，且不能无限增长：
// 单个文件不超过 maxFileSize，最多保留 maxFiles 个（含当前文件）。轮转失败不影响下载——
// 写日志永远不该成为业务失败的原因。
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// LogFileName 是当前日志文件名，放在数据目录的 logs/ 下。
	LogFileName = "sheepget.log"
	// maxFileSize 是单个日志文件的滚动阈值。2 MiB 足够装下一次完整下载的请求编年史。
	maxFileSize = 2 << 20
	// maxFiles 是保留的文件个数（含当前文件），历史上限因此是 maxFileSize × maxFiles。
	maxFiles = 5
)

// logRotationName 匹配轮转出来的文件名（sheepget.1.log、sheepget.2.log …）。
// 它与 rotatedPath 的命名是一对：改了一处必须改另一处。
var logRotationName = regexp.MustCompile(`^` + regexp.QuoteMeta(strings.TrimSuffix(LogFileName, ".log")) + `\.\d+\.log$`)

// Logger 是应用日志出口。
type Logger struct {
	*slog.Logger

	writer *rotatingWriter
}

// New 在 dir 下打开滚动日志，必要时创建目录。目录不可写时返回错误，由调用方决定降级方式。
func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log dir %s: %w", dir, err)
	}
	writer, err := newRotatingWriter(filepath.Join(dir, LogFileName), maxFileSize, maxFiles)
	if err != nil {
		return nil, err
	}
	return &Logger{
		Logger: slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo})),
		writer: writer,
	}, nil
}

// discard 是全局共用的丢弃出口：Logger 本身无状态，不必每次新建。
var discard = &Logger{
	Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	writer: &rotatingWriter{},
}

// Discard 返回一个丢弃一切日志的出口，用于日志文件写不出来时（目录只读、磁盘满），
// 也用于还没接上日志出口的调用方——下载本身照常进行，只是没有日志可看。
func Discard() *Logger { return discard }

// Path 返回当前日志文件路径，供界面与错误信息指引用户去哪看。
func (l *Logger) Path() string {
	return l.writer.path
}

// LogFile 是日志目录里的一个文件。
type LogFile struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// ListLogFiles 列出 dir 下本应用的日志文件（当前文件与各历史文件）。
//
// 「哪些文件算日志」这条规则只在这里定义一次：写入器按同样的命名轮转，清理也按它认领，
// 于是清理既不会漏掉轮转文件，也不会误删目录里的其它东西。目录不存在按「没有日志」处理。
func ListLogFiles(dir string) ([]LogFile, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	files := make([]LogFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isLogFileName(entry.Name()) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, LogFile{
			Path:    filepath.Join(dir, entry.Name()),
			Name:    entry.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return files, nil
}

// isLogFileName 判断文件名是否属于本应用的日志（sheepget.log 或 sheepget.N.log）。
func isLogFileName(name string) bool {
	if name == LogFileName {
		return true
	}
	return logRotationName.MatchString(name)
}

// Close 刷写并关闭日志文件。
func (l *Logger) Close() error {
	return l.writer.Close()
}

// SafeURL 把链接里与请求上下文同等敏感的部分摘掉——查询串常带一次性令牌、签名与防盗链参数，
// 它们不该出现在日志里（ADR-0005 决策 5）。保留 scheme://host/path 足以定位是哪一次下载。
func SafeURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	return parsed.String()
}

// SafeError 把错误里嵌着的地址脱敏后返回：net/http 的请求错误是 *url.Error，
// 它的 Error() 会原样带上完整地址（含查询串里的令牌）。这类错误既会进日志，也会写进任务的
// 错误信息，因此统一在源头脱敏，而不是指望每个输出点各自记得处理。
func SafeError(err error) error {
	if err == nil {
		return nil
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	scrubbed := *urlErr
	scrubbed.URL = SafeURL(urlErr.URL)
	return &scrubbed
}

// rotatingWriter 是一个按大小滚动的文件写入器：写满就把当前文件改名为 .1，原有编号依次后移，
// 超出保留个数的最旧一个删除。
type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxSize  int64
	maxFiles int

	file *os.File
	size int64
}

func newRotatingWriter(path string, maxSize int64, maxFiles int) (*rotatingWriter, error) {
	w := &rotatingWriter{path: path, maxSize: maxSize, maxFiles: maxFiles}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", w.path, err)
	}
	w.file = file
	w.size = 0
	if info, statErr := file.Stat(); statErr == nil {
		w.size = info.Size()
	}
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 轮转失败不阻断这一条日志：继续追加，下次写再试。
	if w.size+int64(len(p)) > w.maxSize {
		_ = w.rotateLocked()
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// renameFile 是轮转时的改名操作。它是个变量而不是直接调用 os.Rename：Windows 上改名会被
// 杀软或索引器短暂占住，测试需要能稳定地制造这种失败（标准库的 os.Rename 本身也是变量）。
var renameFile = os.Rename

func (w *rotatingWriter) rotateLocked() error {
	_ = w.file.Close()
	_ = os.Remove(w.rotatedPath(w.maxFiles - 1))
	for i := w.maxFiles - 2; i >= 1; i-- {
		_ = renameFile(w.rotatedPath(i), w.rotatedPath(i+1))
	}
	renameErr := renameFile(w.path, w.rotatedPath(1))
	// 无论改名成不成功都要重新打开当前文件：改名失败（被杀软或索引器占住）时若把文件留着关闭，
	// 之后每一条日志都会以「文件已关闭」失败，而且下次轮转会在关闭已关闭的文件时立刻放弃——
	// 日志会就此永久失效。这里退回「继续追加」，下一次写入再试一次轮转。
	openErr := w.open()
	if openErr != nil {
		return openErr
	}
	return renameErr
}

// rotatedPath 返回第 index 个历史文件的路径（1 是最近一次轮转出来的那一份）。
func (w *rotatingWriter) rotatedPath(index int) string {
	base := strings.TrimSuffix(w.path, ".log")
	return fmt.Sprintf("%s.%d.log", base, index)
}
