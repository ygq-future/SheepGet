package engine_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

func TestHTTPDownloader_Resume(t *testing.T) {
	fileSize := 1024 * 1024 // 1MB
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("failed to generate random payload: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", "\"resume-etag\"")
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
		// Write in small slices with sleep to allow cancellation midway
		sub := payload[start : end+1]
		for len(sub) > 0 {
			chunkSize := 32 * 1024
			if len(sub) < chunkSize {
				chunkSize = len(sub)
			}
			_, _ = w.Write(sub[:chunkSize])
			sub = sub[chunkSize:]
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()

	dlTask := &task.Task{
		ID:             "task-resume-1",
		URL:            ts.URL,
		Filename:       "resume.bin",
		Directory:      tmpDir,
		TotalBytes:     int64(fileSize),
		MaxConcurrency: 2,
		Resumable:      true,
		ETag:           "\"resume-etag\"",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// First pass: Cancel halfway
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = downloader.Download(ctx, dlTask, nil)
		close(done)
	}()

	time.Sleep(5 * time.Millisecond)
	cancel()
	<-done // Wait for first pass to completely exit

	t.Logf("After cancel: chunk 0 downloaded=%d, completed=%v; chunk 1 downloaded=%d, completed=%v",
		dlTask.Chunks[0].Downloaded, dlTask.Chunks[0].Completed,
		dlTask.Chunks[1].Downloaded, dlTask.Chunks[1].Completed)

	// Second pass: Resume with fresh context
	ctx2 := context.Background()
	if err := downloader.Download(ctx2, dlTask, nil); err != nil {
		t.Fatalf("resumed download failed: %v", err)
	}
	destFile := filepath.Join(tmpDir, "resume.bin")
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read resumed file: %v", err)
	}
	if len(content) != len(payload) {
		t.Fatalf("resumed content length %d != payload %d", len(content), len(payload))
	}
	for i := range payload {
		if content[i] != payload[i] {
			t.Fatalf("mismatch at byte %d: got %x, expected %x (chunks: %+v)", i, content[i], payload[i], dlTask.Chunks)
		}
	}
}

func TestHTTPDownloader_IgnoreRange(t *testing.T) {
	// If server ignores Range and returns 200 OK instead of 206 Partial Content, return ErrRangeNotSupported
	payload := []byte("hello world ignore range")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-ignore-range",
		URL:            ts.URL,
		Filename:       "ignore.bin",
		Directory:      tmpDir,
		TotalBytes:     int64(len(payload)),
		MaxConcurrency: 2,
		Resumable:      true,
	}

	err := downloader.Download(context.Background(), dlTask, nil)
	if err == nil || !strings.Contains(err.Error(), "range not supported") {
		t.Fatalf("expected range not supported error, got %v", err)
	}
}
