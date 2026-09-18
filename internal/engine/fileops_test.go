package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

func TestURLFilename(t *testing.T) {
	cases := []struct {
		name   string
		rawURL string
		want   string
	}{
		{"plain path", "https://host/dir/movie.mp4", "movie.mp4"},
		{"query is dropped", "https://host/dir/movie.mp4?token=abc&size=1", "movie.mp4"},
		// URL 只用 "/" 作分隔符；反斜杠必须留在文件名里，不能像 filepath.Base 在 Windows 上那样被切开。
		{"backslash is not a separator in URLs", `https://host/dir/na\me.bin`, `na\me.bin`},
		{"empty URL has no filename", "", ""},
		{"root path has no filename", "/", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.URLFilename(tc.rawURL); got != tc.want {
				t.Errorf("URLFilename(%q) = %q, want %q", tc.rawURL, got, tc.want)
			}
		})
	}
}

func TestManager_RemoveTaskFiles_RemovesCompletedPartAndTempPart(t *testing.T) {
	baseDir := t.TempDir()
	tempDir := t.TempDir()
	destDir := filepath.Join(baseDir, "downloads")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create destination dir: %v", err)
	}

	store, err := task.NewFileTaskStore(filepath.Join(baseDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	downloader := engine.NewHTTPDownloader(nil)
	downloader.SetTempDirectory(tempDir)
	mgr := engine.NewManager(store, downloader, engine.Config{MaxActiveTasks: 1})
	t.Cleanup(func() { mgr.Close() })

	tt := &task.Task{
		ID:        "task_1",
		URL:       "https://host/video.mp4",
		Filename:  "video.mp4",
		Directory: destDir,
	}

	finalPath := filepath.Join(destDir, "video.mp4")
	sidePart := finalPath + ".sheepget"
	// 配置了独立临时目录时，分片实际落在 tempDir 下（见 GetPartPath）。
	tempPart := filepath.Join(tempDir, "task_1_video.mp4.sheepget")
	unrelated := filepath.Join(destDir, "keep.bin")

	for _, path := range []string{finalPath, sidePart, tempPart, unrelated} {
		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatalf("failed to seed %s: %v", path, err)
		}
	}

	mgr.RemoveTaskFiles(tt)

	for _, path := range []string{finalPath, sidePart, tempPart} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err = %v", path, err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated file must be kept: %v", err)
	}
}
