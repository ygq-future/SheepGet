package engine_test

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"sheep-get/internal/task"
)

// TestHTTPDownloader_AdaptiveConcurrency_503Fallback 验证当服务端对并发连接数进行严格限流时（如最多2个），
// 若大文件（如 76MB）或单连接传输时间长，客户端自适应并发机制能够动态降级并发，
// 且多余线程遇到的 503 限流不会打断已有正常传输的线程，最终成功下载完成。
func TestHTTPDownloader_AdaptiveConcurrency_503Fallback(t *testing.T) {
	fileSize := 1024 * 1024 // 1MB
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("failed to generate payload: %v", err)
	}

	var activeConns int64
	var maxAllowedConns int64 = 2 // 服务端严格限制最多 2 个并发连接，第 3 个及以上返回 503
	var total503Count int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt64(&activeConns, 1)
		defer atomic.AddInt64(&activeConns, -1)

		if cur > maxAllowedConns {
			atomic.AddInt64(&total503Count, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("503 Service Temporarily Unavailable: connection limit exceeded"))
			return
		}

		w.Header().Set("Accept-Ranges", "bytes")
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}

		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(rangeSpec, "-")
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		end, _ := strconv.ParseInt(parts[1], 10, 64)
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)

		// 模拟每块传输需要一定时间，使第 3~8 个 worker 在并发争抢时连续触发 503
		chunkLen := int(end - start + 1)
		step := 16 * 1024
		written := 0
		for written < chunkLen {
			toWrite := step
			if written+toWrite > chunkLen {
				toWrite = chunkLen - written
			}
			_, _ = w.Write(payload[start+int64(written) : start+int64(written+toWrite)])
			written += toWrite
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()

	dlTask := &task.Task{
		ID:             "task-adaptive-503",
		URL:            ts.URL,
		Filename:       "adaptive_503.bin",
		Directory:      tmpDir,
		TotalBytes:     int64(fileSize),
		MaxConcurrency: 8, // 用户/任务配置了 8 线程并发，但服务端只承受 2 线程
		Resumable:      true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err := downloader.Download(context.Background(), dlTask, nil)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	destFile := filepath.Join(tmpDir, "adaptive_503.bin")
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Fatalf("downloaded file content mismatch")
	}

	if atomic.LoadInt64(&total503Count) == 0 {
		t.Logf("warning: server never returned 503 during download")
	}
}

// TestHTTPDownloader_AdaptiveConcurrency_ProbeInitialLimit 验证下载器支持探测初始并发
func TestHTTPDownloader_AdaptiveConcurrency_ProbeInitialLimit(t *testing.T) {
	// 验证自适应并发控制器逻辑
	ac := engine.NewAdaptiveConcurrency(8, 2)
	if ac.CurrentLimit() != 2 {
		t.Fatalf("expected initial limit 2, got %d", ac.CurrentLimit())
	}
	// 首次遇到 503 限流：冻结并保持当前线程数（2），停止增加
	ac.OnRateLimit()
	if ac.CurrentLimit() != 2 {
		t.Fatalf("expected limit to hold at 2 on first error, got %d", ac.CurrentLimit())
	}

	// 在冷却时间窗口内再次遇到 503：继续回退至 1
	ac.OnRateLimit()
	if ac.CurrentLimit() != 1 {
		t.Fatalf("expected limit to roll back to 1 on recurring error, got %d", ac.CurrentLimit())
	}

	// 降级后在冷却期内即使请求成功也不扩容
	ac.OnSuccess()
	if ac.CurrentLimit() != 1 {
		t.Fatalf("expected limit to remain 1 during cooldown, got %d", ac.CurrentLimit())
	}
}

// TestHTTPDownloader_AdaptiveConcurrency_StreamRampUp 验证连接成功开始拉流时逐个快速步进扩容，
// 以及遇到故障时冻结窗口并在冷却后恢复自愈。
func TestHTTPDownloader_AdaptiveConcurrency_StreamRampUp(t *testing.T) {
	ac := engine.NewAdaptiveConcurrency(4, 2)
	if ac.CurrentLimit() != 2 {
		t.Fatalf("expected initial limit 2, got %d", ac.CurrentLimit())
	}

	// 连接建立并开始拉流：步进扩容至 3
	if limit := ac.OnStreamEstablished(); limit != 3 {
		t.Fatalf("expected limit 3 after first stream, got %d", limit)
	}

	// 再次建连成功：步进扩容至 4（达到上限）
	if limit := ac.OnStreamEstablished(); limit != 4 {
		t.Fatalf("expected limit 4 after second stream, got %d", limit)
	}

	// 达到上限后不再递增
	if limit := ac.OnStreamEstablished(); limit != 4 {
		t.Fatalf("expected limit to remain 4 at cap, got %d", limit)
	}

	// 单线故障：冻结窗口
	ac.FreezeOnFailure()
	// 冷却期内即使建连也不扩容
	if limit := ac.OnStreamEstablished(); limit != 4 {
		t.Fatalf("expected limit 4 during freeze, got %d", limit)
	}
}

// TestHTTPDownloader_AdaptiveConcurrency_SingleThreadFaultIsolationAndBackfill
// 模拟多分块并发下载场景：其中某个分块在下载初期发生单线网络中断，
// 验证其他健康 worker 不受打断保持全速下载，出错分块被隔离并在稍后由健康 worker 替补下载完成。
func TestHTTPDownloader_AdaptiveConcurrency_SingleThreadFaultIsolationAndBackfill(t *testing.T) {
	fileSize := 1024 * 1024 // 1MB
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("failed to generate payload: %v", err)
	}

	var hasFailedChunk atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(fileSize))
			w.WriteHeader(http.StatusOK)
			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.Error(w, "Range header required", http.StatusBadRequest)
			return
		}

		var start, end int64
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "invalid range", http.StatusBadRequest)
			return
		}

		// 模拟：针对某一非 0 分片（例如 start >= 500000），首次请求时故意中途断开连接
		if start >= 500000 && !hasFailedChunk.Swap(true) {
			// 写入少量字节后直接中断连接（模拟连接重置/单线网络抖动）
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				if conn != nil {
					_ = conn.Close()
					return
				}
			}
			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()

	dlTask := &task.Task{
		ID:             "iso-backfill-test",
		URL:            ts.URL,
		Filename:       "iso_backfill.bin",
		Directory:      tmpDir,
		TotalBytes:     int64(fileSize),
		Resumable:      true,
		MaxConcurrency: 4,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err := downloader.Download(context.Background(), dlTask, nil)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	destFile := filepath.Join(tmpDir, "iso_backfill.bin")
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Fatalf("downloaded file content mismatch")
	}
	if !hasFailedChunk.Load() {
		t.Fatalf("expected injected chunk fault to be triggered")
	}
}
