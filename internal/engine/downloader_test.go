package engine_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/sys"
	"sheep-get/internal/task"
)

// countingSnapshotSink 统计发布次数：进度汇报的契约就是「每次交出一份自有副本」，
// 因此这里只需要每次都说要、并在收到时记一笔。
type countingSnapshotSink struct {
	onSnapshot func()
}

func (s *countingSnapshotSink) WantsSnapshot() bool {
	return true
}

func (s *countingSnapshotSink) PublishSnapshot(*task.Task) {
	s.onSnapshot()
}

func TestHTTPDownloader_ProbeAndDownload(t *testing.T) {
	// Prepare test payload
	fileSize := 1024 * 1024 // 1MB
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("failed to generate random payload: %v", err)
	}

	etag := "\"test-etag-v1\""

	// Setup mock test server supporting Range and ETag
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Disposition", "attachment; filename=\"sample.bin\"")

		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}

		// Handle range request
		if !strings.HasPrefix(rangeHeader, "bytes=") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(rangeSpec, "-")
		if len(parts) != 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		start, err1 := strconv.ParseInt(parts[0], 10, 64)
		end, err2 := strconv.ParseInt(parts[1], 10, 64)
		if err1 != nil || err2 != nil || start > end || start >= int64(len(payload)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())

	ctx := context.Background()

	// 1. Test Probe
	info, err := downloader.Probe(ctx, ts.URL, nil)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if info.TotalBytes != int64(fileSize) {
		t.Fatalf("expected size %d, got %d", fileSize, info.TotalBytes)
	}
	if !info.Resumable {
		t.Fatalf("expected Resumable=true")
	}
	if info.Filename != "sample.bin" {
		t.Fatalf("expected filename sample.bin, got %s", info.Filename)
	}

	// 2. Test Download (Multi-chunk)
	tmpDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-dl-1",
		URL:            ts.URL,
		Filename:       "sample.bin",
		Directory:      tmpDir,
		TotalBytes:     info.TotalBytes,
		MaxConcurrency: 4,
		Resumable:      true,
		ETag:           info.ETag,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	var progressCalls int
	sink := &countingSnapshotSink{onSnapshot: func() { progressCalls++ }}
	err = downloader.Download(ctx, dlTask, sink)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	destFile := filepath.Join(tmpDir, "sample.bin")
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Fatalf("downloaded content mismatch with original payload!")
	}
	if progressCalls == 0 {
		t.Fatalf("expected progress callback to be called")
	}
}

func TestHTTPDownloader_VersionMismatch(t *testing.T) {
	// Test version consistency: if ETag changes during download, it should return ErrVersionMismatch
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != "\"new-etag\"" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		w.Header().Set("ETag", "\"new-etag\"")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-mismatch",
		URL:            ts.URL,
		Filename:       "sample.bin",
		Directory:      tmpDir,
		TotalBytes:     1024 * 1024,
		MaxConcurrency: 2,
		Resumable:      true,
		ETag:           "\"old-etag\"",
	}

	err := downloader.Download(context.Background(), dlTask, nil)
	if err == nil || !strings.Contains(err.Error(), "file version changed") {
		t.Fatalf("expected version mismatch error, got %v", err)
	}
}

func TestHTTPDownloader_TempDirectoryAndTransfer(t *testing.T) {
	payload := []byte("hello from temp directory download test")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tempDir := t.TempDir()
	destDir := t.TempDir()

	downloader.SetTempDirectory(tempDir)

	dlTask := &task.Task{
		ID:             "task-temp-transfer",
		URL:            ts.URL,
		Filename:       "output.bin",
		Directory:      destDir,
		TempDir:        tempDir,
		TotalBytes:     int64(len(payload)),
		MaxConcurrency: 1,
		Resumable:      false,
	}

	err := downloader.Download(context.Background(), dlTask, nil)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify final destination file exists and content matches
	finalPath := filepath.Join(destDir, "output.bin")
	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Fatalf("content mismatch: got %q, want %q", content, payload)
	}

	// Verify temp file is cleaned up after successful transfer
	partPath := downloader.GetPartPath(dlTask)
	if _, statErr := os.Stat(partPath); statErr == nil {
		t.Errorf("expected temp part file to be removed, but it still exists at %s", partPath)
	}
}

func TestHTTPDownloader_TransferFailurePreservesData(t *testing.T) {
	payload := []byte("recoverable content that must not be deleted on transfer failure")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tempDir := t.TempDir()
	downloader.SetTempDirectory(tempDir)

	// Point destination to a directory that contains a read-only directory or file collision
	blockedDir := t.TempDir()
	// Create a folder with the same name as the target filename so os.Rename fails!
	_ = os.Mkdir(filepath.Join(blockedDir, "blocked.bin"), 0755)
	// Put a file inside it to make it impossible to overwrite with a file
	_ = os.WriteFile(filepath.Join(blockedDir, "blocked.bin", "nested.txt"), []byte("nested"), 0644)

	dlTask := &task.Task{
		ID:             "task-preserve-fail",
		URL:            ts.URL,
		Filename:       "blocked.bin",
		Directory:      blockedDir,
		TempDir:        tempDir,
		TotalBytes:     int64(len(payload)),
		MaxConcurrency: 1,
		Resumable:      false,
	}

	err := downloader.Download(context.Background(), dlTask, nil)
	if err == nil {
		t.Fatalf("expected transfer failure, got nil")
	}

	// CRITICAL VERIFICATION: The downloaded .sheepget part file in tempDir MUST still exist with full content!
	partPath := downloader.GetPartPath(dlTask)
	partData, readErr := os.ReadFile(partPath)
	if readErr != nil {
		t.Fatalf("CRITICAL: temporary data was not preserved on transfer failure! error: %v", readErr)
	}
	if !bytes.Equal(partData, payload) {
		t.Fatalf("preserved data mismatch: got %q, want %q", partData, payload)
	}
}

func TestHTTPDownloader_ServerLastModifiedTime(t *testing.T) {
	serverTimeStr := "Wed, 21 Oct 2015 07:28:00 GMT"
	expectedTime, _ := http.ParseTime(serverTimeStr)

	payload := []byte("file with last modified time header")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Header().Set("Last-Modified", serverTimeStr)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	// Case A: useServerFileTime = true
	{
		downloader := engine.NewHTTPDownloader(ts.Client())
		downloader.SetUseServerFileTime(true)
		destDir := t.TempDir()

		dlTask := &task.Task{
			ID:             "task-server-time",
			URL:            ts.URL,
			Filename:       "timed.bin",
			Directory:      destDir,
			TotalBytes:     int64(len(payload)),
			MaxConcurrency: 1,
			Resumable:      false,
			LastModified:   serverTimeStr,
		}

		err := downloader.Download(context.Background(), dlTask, nil)
		if err != nil {
			t.Fatalf("Download failed: %v", err)
		}

		filePath := filepath.Join(destDir, "timed.bin")
		info, statErr := os.Stat(filePath)
		if statErr != nil {
			t.Fatalf("Stat failed: %v", statErr)
		}

		if info.ModTime().Unix() != expectedTime.Unix() {
			t.Errorf("expected mod time %v, got %v", expectedTime, info.ModTime())
		}
	}

	// Case B: useServerFileTime = false
	{
		downloader := engine.NewHTTPDownloader(ts.Client())
		downloader.SetUseServerFileTime(false)
		destDir := t.TempDir()

		dlTask := &task.Task{
			ID:             "task-local-time",
			URL:            ts.URL,
			Filename:       "local.bin",
			Directory:      destDir,
			TotalBytes:     int64(len(payload)),
			MaxConcurrency: 1,
			Resumable:      false,
			LastModified:   serverTimeStr,
		}

		err := downloader.Download(context.Background(), dlTask, nil)
		if err != nil {
			t.Fatalf("Download failed: %v", err)
		}

		filePath := filepath.Join(destDir, "local.bin")
		info, statErr := os.Stat(filePath)
		if statErr != nil {
			t.Fatalf("Stat failed: %v", statErr)
		}

		// ModTime should be recent (now), not 2015!
		if info.ModTime().Year() == 2015 {
			t.Errorf("expected local mod time (recent), got 2015")
		}
	}
}

func TestHTTPDownloader_FatalStatusCodeAbortsWithoutRetry(t *testing.T) {
	fatalCodes := []struct {
		name       string
		statusCode int
		statusText string
	}{
		{"jwt expired status 618", 618, "618 jwt:expired"},
		{"forbidden status 403", http.StatusForbidden, "403 Forbidden"},
		{"unauthorized status 401", http.StatusUnauthorized, "401 Unauthorized"},
		{"not found status 404", http.StatusNotFound, "404 Not Found"},
	}

	for _, tc := range fatalCodes {
		t.Run(tc.name, func(t *testing.T) {
			var requestCount atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount.Add(1)
				w.WriteHeader(tc.statusCode)
			}))
			defer ts.Close()

			downloader := engine.NewHTTPDownloader(ts.Client())
			destDir := t.TempDir()

			dlTask := &task.Task{
				ID:             "task-fatal-status",
				URL:            ts.URL,
				Filename:       "fatal.bin",
				Directory:      destDir,
				TotalBytes:     1024,
				MaxConcurrency: 1,
				Resumable:      true,
				Chunks: []task.Chunk{
					{Index: 0, Start: 0, End: 1023, Downloaded: 0, Completed: false},
				},
			}

			err := downloader.Download(context.Background(), dlTask, nil)
			if err == nil {
				t.Fatalf("expected download to fail, got nil")
			}

			if !engine.IsFatalError(err) {
				t.Errorf("expected error to be FatalError, got %v", err)
			}

			// 致命错误必须立即中止，禁止循环重试；由于单分块单并发，请求数应恰好为 1
			if count := requestCount.Load(); count != 1 {
				t.Errorf("expected exactly 1 request (no retries), got %d requests", count)
			}
		})
	}
}

func TestHTTPDownloader_RetryLimitExceeded(t *testing.T) {
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		// 返回 503 临时服务端错误（属于可重试错误）
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	destDir := t.TempDir()

	dlTask := &task.Task{
		ID:             "task-retry-limit",
		URL:            ts.URL,
		Filename:       "retry_limit.bin",
		Directory:      destDir,
		TotalBytes:     1024,
		MaxConcurrency: 1,
		Resumable:      true,
		Chunks: []task.Chunk{
			{Index: 0, Start: 0, End: 1023, Downloaded: 0, Completed: false},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, dlTask, nil)
	if err == nil {
		t.Fatalf("expected download to fail after max retries, got nil")
	}

	if !strings.Contains(err.Error(), "failed after 6 retries") {
		t.Errorf("expected error to mention retry limit (6 retries), got: %v", err)
	}

	// 首次尝试 (1) + 6 次重试 = 7 次请求，封顶停止，绝不无限循环
	if count := requestCount.Load(); count != 7 {
		t.Errorf("expected exactly 7 requests (1 initial + 6 retries), got %d", count)
	}
}

func TestHTTPDownloader_ProbeTimeoutAndFastFail(t *testing.T) {
	// 1. 已取消/超时 Context 应立即返回错误，不发起无谓请求
	downloader := engine.NewHTTPDownloader(http.DefaultClient)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := downloader.Probe(canceledCtx, "http://127.0.0.1:65432/test", nil)
	if err == nil {
		t.Fatal("expected canceled context to return error")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Errorf("expected immediate return on canceled context, took %v", time.Since(start))
	}

	// 2. 遇到明确的 404 状态码时快速失败，不盲目消耗多次重试
	var reqCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	serverDownloader := engine.NewHTTPDownloader(ts.Client())
	_, err = serverDownloader.Probe(context.Background(), ts.URL, nil)
	if err == nil {
		t.Fatal("expected 404 probe to fail")
	}
	// 404 不需要反复重试 3 次 GET，仅允许首次尝试（若为 405 则可试 HEAD，404 不重试 GET）
	if count := reqCount.Load(); count > 2 {
		t.Errorf("expected fast-fail on 404 with <= 2 requests, got %d", count)
	}
}

func TestHTTPDownloader_RedirectFinalURLAndVersionConsistency(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz012345"), 8192) // 256KB
	etag := "\"mirror-node-1-etag\""

	var mirrorReqs atomic.Int32
	mirrorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorReqs.Add(1)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/octet-stream")

		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}

		// Verify If-Match
		if ifMatch := r.Header.Get("If-Match"); ifMatch != "" && ifMatch != etag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}

		var start, end int64
		_, _ = fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer mirrorServer.Close()

	// Dispatcher server: redirects to mirrorServer
	dispatcherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mirrorServer.URL+"/kdenlive.exe", http.StatusFound)
	}))
	defer dispatcherServer.Close()

	downloader := engine.NewHTTPDownloader(dispatcherServer.Client())
	ctx := context.Background()

	// 1. Probe through dispatcher: verify FinalURL captured
	info, err := downloader.Probe(ctx, dispatcherServer.URL+"/dispatcher-entry", nil)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if info.FinalURL != mirrorServer.URL+"/kdenlive.exe" {
		t.Fatalf("expected FinalURL to be %s, got %s", mirrorServer.URL+"/kdenlive.exe", info.FinalURL)
	}
	if info.ETag != etag {
		t.Fatalf("expected ETag %s, got %s", etag, info.ETag)
	}
	if info.Filename != "kdenlive.exe" {
		t.Fatalf("expected Filename kdenlive.exe, got %s", info.Filename)
	}

	// 2. Download task initialized with FinalURL: verify smooth completion without version mismatch
	tmpDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-redirect-finalurl",
		URL:            dispatcherServer.URL + "/dispatcher-entry",
		FinalURL:       info.FinalURL,
		Filename:       info.Filename,
		Directory:      tmpDir,
		TotalBytes:     int64(len(payload)),
		MaxConcurrency: 4,
		Resumable:      true,
		ETag:           info.ETag,
	}

	err = downloader.Download(ctx, dlTask, nil)
	if err != nil {
		t.Fatalf("download with FinalURL failed: %v", err)
	}

	downloadedData, err := os.ReadFile(filepath.Join(tmpDir, "kdenlive.exe"))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(downloadedData, payload) {
		t.Fatal("downloaded content does not match expected payload")
	}
}

func TestHTTPDownloader_SingleStreamResolvesTotalSizeAndResumable(t *testing.T) {
	payload := []byte("dynamic content size resolved via single stream")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(nil)
	tmpDir := t.TempDir()
	downloader.SetTempDirectory(tmpDir)

	dlTask := &task.Task{
		ID:         "task-unknown-size-resolve",
		URL:        ts.URL + "/stream.bin",
		Filename:   "stream.bin",
		Directory:  tmpDir,
		TotalBytes: -1,
		Resumable:  false,
	}

	err := downloader.Download(context.Background(), dlTask, nil)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if dlTask.TotalBytes != int64(len(payload)) {
		t.Fatalf("expected TotalBytes to be resolved to %d, got %d", len(payload), dlTask.TotalBytes)
	}
	if !dlTask.Resumable {
		t.Fatal("expected task to be marked resumable from Accept-Ranges")
	}
}

func TestHTTPDownloader_ProbeDirectFallbackWhenProxyFails(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	downloader := engine.NewHTTPDownloader(nil)
	// 配置一个无法连通的自定义代理
	_ = downloader.SetProxy("custom", "http://127.0.0.1:59999")

	info, err := downloader.Probe(context.Background(), targetServer.URL+"/test.bin", nil)
	if err != nil {
		t.Fatalf("expected direct fallback to succeed, got: %v", err)
	}
	if !info.DirectTransfer {
		t.Fatal("expected DirectTransfer to be true after fallback")
	}

	dlTask := &task.Task{
		ID:             "direct-task",
		DirectTransfer: true,
	}
	client := downloader.GetClientForTask(dlTask)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestHTTPDownloader_PreFlightDiskSpaceCheck 验证 P1-3：
// 1. 已知大小任务启动前，若目标卷或临时卷空间不足，在预分配 .sheepget 文件和发网络请求前直接拦截；
// 2. 未知大小任务进入单流传输后，一旦从响应头 Content-Length 获知真实大小，立即执行空间校验并拦截超限任务。
func TestHTTPDownloader_PreFlightDiskSpaceCheck(t *testing.T) {
	var serverRequests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverRequests.Add(1)
		w.Header().Set("Content-Length", "1048576") // 1MB
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("head-bytes"))
	}))
	defer ts.Close()

	// 模拟磁盘仅剩 100KB 可用空间
	restore := sys.SetAvailableDiskSpaceForTest(func(dir string) (uint64, error) {
		return 100 * 1024, nil
	})
	defer restore()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()

	// 场景 1：已知大小（1MB > 100KB），在发起网络请求和 Truncate 临时文件前即拦截
	knownTask := &task.Task{
		ID:             "task-disk-full-known",
		URL:            ts.URL + "/large.bin",
		Filename:       "large.bin",
		Directory:      tmpDir,
		TotalBytes:     1024 * 1024,
		MaxConcurrency: 2,
		Resumable:      true,
	}
	err := downloader.Download(context.Background(), knownTask, nil)
	if !errors.Is(err, sys.ErrInsufficientDiskSpace) {
		t.Fatalf("expected ErrInsufficientDiskSpace for known-size task, got: %v", err)
	}
	if serverRequests.Load() != 0 {
		t.Fatalf("expected 0 HTTP requests when pre-flight disk check fails, got %d", serverRequests.Load())
	}
	if _, statErr := os.Stat(downloader.GetPartPath(knownTask)); !os.IsNotExist(statErr) {
		t.Fatalf("expected no .sheepget file to be allocated when pre-flight check fails")
	}

	// 场景 2：未知大小（TotalBytes = -1），单流拿到响应头 Content-Length=1MB 后立即拦截，不继续写爆磁盘
	unknownTask := &task.Task{
		ID:         "task-disk-full-unknown",
		URL:        ts.URL + "/stream.bin",
		Filename:   "stream.bin",
		Directory:  tmpDir,
		TotalBytes: -1,
		Resumable:  false,
	}
	err = downloader.Download(context.Background(), unknownTask, nil)
	if !errors.Is(err, sys.ErrInsufficientDiskSpace) {
		t.Fatalf("expected ErrInsufficientDiskSpace after resolving single-stream Content-Length, got: %v", err)
	}
}

// TestHTTPDownloader_HTMLSoftErrorDisguiseInterception 验证 P2-1：
//  1. 当下载二进制/安装包（如 .exe / .zip）遇到服务端会话过期返回 200 OK + text/html 登录页时，
//     无论是分块下载还是单流下载均立即拦截并上报 ErrHTMLDisguisedAsBin 致命错误，绝不将网页存为损坏的安装包；
//  2. 若任务使用的是 302 跳转后的过期 FinalURL 返回了 200 OK 登录页，优先尝试从原始 URL 回源刷新并恢复真实二进制下载；
//  3. 若用户本来就是要下载 .html 网页文件，正常放行并完成保存。
func TestHTTPDownloader_HTMLSoftErrorDisguiseInterception(t *testing.T) {
	loginPageHTML := []byte("<!DOCTYPE html><html><head><title>Login Required</title></head><body>Please sign in</body></html>")
	realZipPayload := append([]byte("PK\x03\x04"), bytes.Repeat([]byte("binary-zip-content-"), 20000)...)

	var originRefreshHits atomic.Int32
	var serverURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/expired-session/installer.exe":
			// 模拟鉴权过期后服务端不返回 401/403，而是直接 200 OK 返回 HTML 登录页
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(loginPageHTML)
		case "/origin/package.zip":
			originRefreshHits.Add(1)
			http.Redirect(w, r, serverURL+"/cdn/fresh/package.zip", http.StatusFound)
		case "/cdn/expired/package.zip":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(loginPageHTML)
		case "/cdn/fresh/package.zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("ETag", `"zip-v1"`)
			rangeHdr := r.Header.Get("Range")
			if rangeHdr == "" {
				w.Header().Set("Content-Length", strconv.Itoa(len(realZipPayload)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(realZipPayload)
				return
			}
			var start, end int64
			_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
			if end >= int64(len(realZipPayload)) {
				end = int64(len(realZipPayload)) - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(realZipPayload)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(realZipPayload[start : end+1])
		case "/docs/manual.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(loginPageHTML)
		case "/portal/download.html":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<div class="login-box">Session expired, please login</div>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	serverURL = ts.URL

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()

	// 1. 探测阶段：对 .exe 链接返回 200 OK + text/html 登录页时直接拦截
	if _, probeErr := downloader.Probe(context.Background(), ts.URL+"/expired-session/installer.exe", nil); !errors.Is(probeErr, engine.ErrHTMLDisguisedAsBin) {
		t.Fatalf("expected Probe to intercept disguised HTML with ErrHTMLDisguisedAsBin, got: %v", probeErr)
	}

	// 2. 分块传输阶段：遭遇 200 OK + text/html 登录页时立即报错中止，绝不降级单流保存损坏文件
	chunkTask := &task.Task{
		ID:             "task-html-disguise-chunk",
		URL:            ts.URL + "/expired-session/installer.exe",
		Filename:       "installer.exe",
		Directory:      tmpDir,
		TotalBytes:     512 * 1024,
		MaxConcurrency: 2,
		Resumable:      true,
	}
	err := downloader.Download(context.Background(), chunkTask, nil)
	if !errors.Is(err, engine.ErrHTMLDisguisedAsBin) {
		t.Fatalf("expected chunked download to fail with ErrHTMLDisguisedAsBin, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, "installer.exe")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no corrupted installer.exe to be saved on disk")
	}

	// 3. 单流传输阶段：未知大小任务遭遇 200 OK + text/html 登录页时同样拦截并清理临时文件
	singleTask := &task.Task{
		ID:         "task-html-disguise-single",
		URL:        ts.URL + "/expired-session/installer.exe",
		Filename:   "installer_single.exe",
		Directory:  tmpDir,
		TotalBytes: -1,
		Resumable:  false,
	}
	err = downloader.Download(context.Background(), singleTask, nil)
	if !errors.Is(err, engine.ErrHTMLDisguisedAsBin) {
		t.Fatalf("expected single-stream download to fail with ErrHTMLDisguisedAsBin, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, "installer_single.exe")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no corrupted installer_single.exe to be saved on disk")
	}

	// 4. 过期 FinalURL 返回 200 OK HTML 登录页时，自动回源刷新 FinalURL 并完成真实 ZIP 下载
	refreshTask := &task.Task{
		ID:             "task-html-disguise-refresh",
		URL:            ts.URL + "/origin/package.zip",
		FinalURL:       ts.URL + "/cdn/expired/package.zip",
		Filename:       "package.zip",
		Directory:      tmpDir,
		TotalBytes:     int64(len(realZipPayload)),
		ETag:           `"zip-v1"`,
		MaxConcurrency: 2,
		Resumable:      true,
	}
	if err := downloader.Download(context.Background(), refreshTask, nil); err != nil {
		t.Fatalf("expected expired FinalURL returning HTML to recover via origin refresh, got: %v", err)
	}
	if originRefreshHits.Load() == 0 {
		t.Fatalf("expected origin URL to be queried for refreshing FinalURL")
	}
	gotZip, err := os.ReadFile(filepath.Join(tmpDir, "package.zip"))
	if err != nil || !bytes.Equal(gotZip, realZipPayload) {
		t.Fatalf("downloaded zip payload mismatch after origin refresh")
	}

	// 4b. URL 网关为 /portal/download.html?file=archive.tar 但任务目标文件名为 archive.tar，且返回 <div... 片段 HTML 时仍准确拦截
	gatewayTask := &task.Task{
		ID:         "task-html-gateway-tar",
		URL:        ts.URL + "/portal/download.html?file=archive.tar",
		Filename:   "archive.tar",
		Directory:  tmpDir,
		TotalBytes: -1,
		Resumable:  false,
	}
	if err := downloader.Download(context.Background(), gatewayTask, nil); !errors.Is(err, engine.ErrHTMLDisguisedAsBin) {
		t.Fatalf("expected gateway URL with binary target filename to intercept HTML fragment, got: %v", err)
	}

	// 5. 合法下载 .html 文件本身时正常放行
	htmlTask := &task.Task{
		ID:         "task-legit-html",
		URL:        ts.URL + "/docs/manual.html",
		Filename:   "manual.html",
		Directory:  tmpDir,
		TotalBytes: -1,
		Resumable:  false,
	}
	if err := downloader.Download(context.Background(), htmlTask, nil); err != nil {
		t.Fatalf("expected legitimate .html download to succeed, got: %v", err)
	}
	gotHTML, err := os.ReadFile(filepath.Join(tmpDir, "manual.html"))
	if err != nil || !bytes.Equal(gotHTML, loginPageHTML) {
		t.Fatalf("legitimate html content mismatch")
	}
}

// TestHTTPDownloader_SharedClientConnectionPoolReuse 验证 P2-2：
// GetClientForDownload 与 GetClientForTask 复用同一个底层 Transport 连接池，避免每次请求重建 Transport 导致 TLS/TCP 连接池失效。
func TestHTTPDownloader_SharedClientConnectionPoolReuse(t *testing.T) {
	dl := engine.NewHTTPDownloader(nil)
	c1 := dl.GetClientForDownload()
	c2 := dl.GetClientForDownload()
	if c1 == nil || c1 != c2 {
		t.Fatalf("expected GetClientForDownload to reuse shared *http.Client instance")
	}

	directTask1 := &task.Task{ID: "t1", DirectTransfer: true}
	directTask2 := &task.Task{ID: "t2", DirectTransfer: true}
	dc1 := dl.GetClientForTask(directTask1)
	dc2 := dl.GetClientForTask(directTask2)
	if dc1 == nil || dc1 != dc2 {
		t.Fatalf("expected direct transfer tasks to reuse shared direct *http.Client pool")
	}
}
