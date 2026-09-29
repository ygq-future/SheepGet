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
		{"query without filename is dropped", "https://host/dir/movie.mp4?token=abc&size=1", "movie.mp4"},
		// GitHub Release Assets (Azure Blob SAS URL with rscd and response-content-disposition)
		{
			"github release asset url with rscd",
			"https://release-assets.githubusercontent.com/github-production-release-asset/1353047810/05c355a4-a0ca-42bb-b8ea-f4393ccba553?sp=r&rscd=attachment%3B+filename%3Dmihomo-multi_1.1.2_linux-x64.tar.gz&response-content-disposition=attachment%3B%20filename%3Dmihomo-multi_1.1.2_linux-x64.tar.gz",
			"mihomo-multi_1.1.2_linux-x64.tar.gz",
		},
		{
			"s3 or oss response-content-disposition with quotes",
			"https://bucket.s3.amazonaws.com/uuid-blob-12345?response-content-disposition=attachment%3B%20filename%3D%22archive.tar.gz%22",
			"archive.tar.gz",
		},
		{
			"response-content-disposition with RFC 5987 filename*",
			"https://storage.example.com/asset/999?response-content-disposition=attachment%3Bfilename%2A%3DUTF-8%27%27%E6%B5%8B%E8%AF%95%E6%96%87%E4%BB%B6.zip",
			"测试文件.zip",
		},
		{
			"query with direct filename parameter",
			"https://api.example.com/download?id=123&filename=package-v2.0.0.apk",
			"package-v2.0.0.apk",
		},
		{
			"query with file_name parameter URL encoded",
			"https://api.example.com/download?file_name=%E6%97%A5%E5%BF%97.log",
			"日志.log",
		},
		{
			"query with attname parameter",
			"https://qiniu.example.com/raw-object-id?attname=document.docx",
			"document.docx",
		},
		{
			"path with url encoding decoded cleanly",
			"https://host/dir/%E6%96%87%E4%BB%B6%E5%A4%B9/%E6%8A%A5%E5%91%8A.pdf",
			"报告.pdf",
		},
		{
			"path traversal in query filename sanitized",
			"https://host/api?filename=../../etc/passwd",
			"passwd",
		},
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
func TestParseContentDispositionFilename(t *testing.T) {
	cases := []struct {
		name string
		cd   string
		want string
	}{
		{"standard with quotes", `attachment; filename="video.mp4"`, "video.mp4"},
		{"standard unquoted", `attachment; filename=video.mp4`, "video.mp4"},
		{"rfc5987 utf8 without quotes", `attachment; filename*=UTF-8''my%20clip.mp4`, "my clip.mp4"},
		{"rfc5987 with language tag", `attachment; filename*=utf-8'en'my%20clip.mp4`, "my clip.mp4"},
		{"rfc5987 with chinese characters", `attachment; filename*=UTF-8''%E4%B8%AD%E6%96%87%E6%96%87%E4%BB%B6.zip`, "中文文件.zip"},
		{"filename* takes precedence over filename", `attachment; filename="fallback.bin"; filename*=UTF-8''preferred.bin`, "preferred.bin"},
		{"url encoded filename in plain parameter", `attachment; filename=%E6%B5%8B%E8%AF%95.bin`, "测试.bin"},
		{"path traversal stripped", `attachment; filename="../../../evil.sh"`, "evil.sh"},
		{"windows path traversal stripped", `attachment; filename="..\\..\\evil.bat"`, "evil.bat"},
		{"empty header", "", ""},
		{"only disposition type", "attachment", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.ParseContentDispositionFilename(tc.cd); got != tc.want {
				t.Errorf("ParseContentDispositionFilename(%q) = %q, want %q", tc.cd, got, tc.want)
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
