package logging_test

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"sheep-get/internal/logging"
)

// 并发写入不得交叠或丢失：日志本身要能在事后被逐行读通。
func TestLogger_ConcurrentWritesStayIntact(t *testing.T) {
	dir := t.TempDir()
	logger, err := logging.New(dir)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				logger.Info("record", "worker", worker, "idx", i)
			}
		}()
	}
	wg.Wait()
	if err := logger.Close(); err != nil {
		t.Fatalf("failed to close logger: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, logging.LogFileName))
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 400 {
		t.Fatalf("expected 400 records, got %d", len(lines))
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "time=") || !strings.Contains(line, "record") {
			t.Fatalf("malformed log line: %q", line)
		}
	}
	if logger.Path() != filepath.Join(dir, logging.LogFileName) {
		t.Fatalf("unexpected log path: %s", logger.Path())
	}
}

// 丢弃出口在没有任何文件的情况下也必须可写可关：日志写不出来不该拖垮调用方。
func TestLogger_DiscardIsUsable(t *testing.T) {
	logger := logging.Discard()
	logger.Info("nothing to see")
	if logger.Path() != "" {
		t.Fatalf("expected no path for the discard logger, got %q", logger.Path())
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("discard close must not fail: %v", err)
	}
}

// 请求错误里嵌着的地址同样要脱敏：它既会进日志，也会写进任务的错误信息。
func TestSafeError_ScrubsEmbeddedURL(t *testing.T) {
	raw := "https://cdn.example.com/a/b.zip?token=secret"
	urlErr := &url.Error{Op: "Get", URL: raw, Err: errors.New("connection reset")}
	scrubbed := logging.SafeError(urlErr)
	if strings.Contains(scrubbed.Error(), "secret") {
		t.Fatalf("token leaked through the error text: %v", scrubbed)
	}
	if !strings.Contains(scrubbed.Error(), "https://cdn.example.com/a/b.zip") {
		t.Fatalf("expected the address to survive without the query, got: %v", scrubbed)
	}
	if scrubbed == error(urlErr) {
		t.Fatal("expected a distinct error value, not the original carrying the raw URL")
	}
	// 非 *url.Error 原样返回，不改变已有语义。
	plain := errors.New("plain failure")
	if got := logging.SafeError(plain); got != plain {
		t.Fatalf("expected the original error, got %v", got)
	}
	if logging.SafeError(nil) != nil {
		t.Fatal("expected nil to stay nil")
	}
}

// 链接里的令牌类参数不得进日志：日志是长期留存的文件，不该泄漏请求上下文（ADR-0005 决策 5）。
func TestSafeURL_DropsQueryAndFragment(t *testing.T) {
	cases := map[string]string{
		"https://cdn.example.com/a/b.zip?token=secret&expires=1": "https://cdn.example.com/a/b.zip",
		"https://example.com/video.m3u8#frag":                    "https://example.com/video.m3u8",
		"https://user:pass@example.com/x":                        "https://example.com/x",
		"not a url":                                              "not a url",
		"":                                                       "",
	}
	for raw, want := range cases {
		if got := logging.SafeURL(raw); got != want {
			t.Errorf("SafeURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

// 「哪些文件算日志」只有一处定义：列出时必须包含当前文件与各轮转文件，且不认领无关文件。
func TestListLogFiles_OnlyOwnLogs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{logging.LogFileName, "sheepget.1.log", "sheepget.12.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	for _, name := range []string{"notes.txt", "sheepget.log.bak", "sheepget.log1", "other.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sheepget.3.log"), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	files, err := logging.ListLogFiles(dir)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
		if file.Size != 1 || file.Path == "" || file.ModTime.IsZero() {
			t.Fatalf("incomplete log file entry: %+v", file)
		}
	}
	sort.Strings(names)
	want := []string{"sheepget.1.log", "sheepget.12.log", "sheepget.log"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected log files: got %v, want %v", names, want)
	}
}

// 日志目录还不存在（日志开关没打开过）时，列出结果为空而不是报错。
func TestListLogFiles_MissingDirIsEmpty(t *testing.T) {
	files, err := logging.ListLogFiles(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing dir must not fail: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files, got %+v", files)
	}
}
