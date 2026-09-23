package engine_test

import (
	"bytes"
	"context"
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
	"sheep-get/internal/logging"
	"sheep-get/internal/task"
)

// stallOnceServer 首轮请求发出响应头与一部分数据后停住不再发（连接保持打开），
// 之后到达的请求正常发完。它复刻的是「对端保持连接但不发数据」——不看门狗就会一直阻塞。
func stallOnceServer(payload []byte, stalledBytes int) (*httptest.Server, *atomic.Int64) {
	var stalled atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		rangeHeader := r.Header.Get("Range")
		start, end := int64(0), int64(len(payload)-1)
		if rangeHeader != "" {
			rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
			parts := strings.Split(rangeSpec, "-")
			start, _ = strconv.ParseInt(parts[0], 10, 64)
			if len(parts) > 1 && parts[1] != "" {
				if parsed, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					end = parsed
				}
			}
			if end >= int64(len(payload)) {
				end = int64(len(payload)) - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		chunk := payload[start : end+1]
		if stalled.Add(1) == 1 && len(chunk) > stalledBytes {
			_, _ = w.Write(chunk[:stalledBytes])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			// 停住：连接不退让、不再发字节，直到测试结束。
			<-r.Context().Done()
			return
		}
		_, _ = w.Write(chunk)
	})), &stalled
}

// TestHTTPDownloader_StalledConnectionRecovers 固定「静默挂住的连接」这条契约：
// 连接不发数据时必须被看门狗斩断，由传输层从当前偏移重连，而不是让任务永远停住。
func TestHTTPDownloader_StalledConnectionRecovers(t *testing.T) {
	tmpDir := t.TempDir()
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MiB
	ts, stalled := stallOnceServer(payload, 128*1024)
	defer ts.Close()

	logger, err := logging.New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	downloader := engine.NewHTTPDownloader(ts.Client())
	downloader.SetLogger(logger.Logger)
	downloader.SetStallTimeout(200 * time.Millisecond)

	dlTask := &task.Task{
		ID:             "task-stall-1",
		URL:            ts.URL + "/file.bin",
		Filename:       "file.bin",
		Directory:      tmpDir,
		TotalBytes:     int64(len(payload)),
		MaxConcurrency: 2,
		Resumable:      true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	done := make(chan error, 1)
	go func() { done <- downloader.Download(context.Background(), dlTask, nil) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("download must recover from a stalled connection, got: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("download never finished: a silently stalled connection was not cut off")
	}

	content, readErr := os.ReadFile(filepath.Join(tmpDir, "file.bin"))
	if readErr != nil {
		t.Fatalf("failed to read downloaded file: %v", readErr)
	}
	if !bytes.Equal(content, payload) {
		t.Fatalf("downloaded content mismatch: got %d bytes, want %d", len(content), len(payload))
	}
	if stalled.Load() < 2 {
		t.Fatalf("expected a reconnect after the stall, server saw %d requests", stalled.Load())
	}

	logContent, _ := os.ReadFile(filepath.Join(tmpDir, logging.LogFileName))
	if !strings.Contains(string(logContent), "stalled=true") {
		t.Fatalf("expected the stall to be recorded in the log, got:\n%s", logContent)
	}
}
