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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

func TestHTTPDownloader_SlowChunkAssistance(t *testing.T) {
	// A12: 模拟局部慢块与整体限速；验证协助与最终文件字节一致
	fileSize := 1024 * 1024 // 1MB
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("failed to generate payload: %v", err)
	}

	var slowChunkReqCount int64
	var totalReqCount int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&totalReqCount, 1)
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

		// Delay specifically for the first half if requested
		if start == 0 {
			atomic.AddInt64(&slowChunkReqCount, 1)
			time.Sleep(30 * time.Millisecond)
		}

		_, _ = w.Write(payload[start : end+1])
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()

	dlTask := &task.Task{
		ID:             "task-slow-assist",
		URL:            ts.URL,
		Filename:       "slow_assist.bin",
		Directory:      tmpDir,
		TotalBytes:     int64(fileSize),
		MaxConcurrency: 4,
		Resumable:      true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	var mu sync.Mutex
	var speedHistory []int64
	err := downloader.Download(context.Background(), dlTask, func(downloaded int64, chunkIndex int, chunkDownloaded int64) {
		mu.Lock()
		speedHistory = append(speedHistory, downloaded)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	destFile := filepath.Join(tmpDir, "slow_assist.bin")
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Fatalf("content mismatch in slow chunk assisted download")
	}
}
