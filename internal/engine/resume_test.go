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
	"sheep-get/internal/engine"
	"sheep-get/internal/task"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	// P1-1: If server ignores Range and returns 200 OK instead of 206 Partial Content during chunked download,
	// automatically downgrade to single-stream download and complete the file cleanly.
	payload := bytes.Repeat([]byte("hello world ignore range! "), 25000) // ~650KB (> 2 * MinChunkSize)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
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
	if err != nil {
		t.Fatalf("expected fake-range download to auto-downgrade to single-stream and succeed, got %v", err)
	}
	if dlTask.Resumable {
		t.Fatalf("expected Resumable to be downgraded to false after server returned 200 OK without Accept-Ranges")
	}
	if len(dlTask.Chunks) != 0 {
		t.Fatalf("expected Chunks to be cleared after downgrading to single-stream, got %d chunks", len(dlTask.Chunks))
	}
	got, err := os.ReadFile(filepath.Join(tmpDir, "ignore.bin"))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded file content mismatch: got %d bytes, want %d bytes", len(got), len(payload))
	}
}

func TestHTTPDownloader_SingleStreamUpgradesToResumableChunksOnMidDrop(t *testing.T) {
	// P1-1: A task starts as single-stream (TotalBytes = -1, Resumable = false, e.g. probe timed out).
	// During single-stream transfer, response headers reveal Content-Length and Accept-Ranges: bytes.
	// When the connection drops halfway, the downloader preserves the already downloaded bytes and seamlessly
	// upgrades to a Range request from the exact interruption offset instead of restarting from 0.
	payload := bytes.Repeat([]byte("0123456789abcdef"), 16384) // 256KB
	cutOffset := len(payload) / 2

	var reqCount atomic.Int32
	var sawRangeResume atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := reqCount.Add(1)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"upgrade-etag-v1"`)

		rangeHeader := r.Header.Get("Range")
		if call == 1 && rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload[:cutOffset])
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, hErr := hj.Hijack(); hErr == nil {
					_ = conn.Close()
					return
				}
			}
			return
		}

		if rangeHeader != "" {
			var start, end int64
			_, _ = fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
			if start == int64(cutOffset) {
				sawRangeResume.Store(true)
			}
			if end >= int64(len(payload)) {
				end = int64(len(payload)) - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start : end+1])
			return
		}

		t.Errorf("unexpected non-Range request on call %d", call)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-single-to-chunk-upgrade",
		URL:            ts.URL + "/archive.zip",
		Filename:       "archive.zip",
		Directory:      tmpDir,
		TotalBytes:     -1,
		MaxConcurrency: 2,
		Resumable:      false,
	}

	if err := downloader.Download(context.Background(), dlTask, nil); err != nil {
		t.Fatalf("expected single-stream to upgrade to resumable chunk download and succeed, got: %v", err)
	}
	if !sawRangeResume.Load() {
		t.Fatalf("expected second request to resume via Range from offset %d", cutOffset)
	}
	if !dlTask.Resumable || dlTask.TotalBytes != int64(len(payload)) {
		t.Fatalf("expected task metadata to be upgraded (Resumable=%v, TotalBytes=%d)", dlTask.Resumable, dlTask.TotalBytes)
	}
	got, err := os.ReadFile(filepath.Join(tmpDir, "archive.zip"))
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("output file content mismatch after single-stream upgrade")
	}
}
