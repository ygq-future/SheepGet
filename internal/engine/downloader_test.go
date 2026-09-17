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
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

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
	err = downloader.Download(ctx, dlTask, func(downloaded int64, chunkIndex int, chunkDownloaded int64) {
		progressCalls++
	})
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
