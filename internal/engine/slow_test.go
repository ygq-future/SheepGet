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

func TestHTTPDownloader_SlowChunkAssistance(t *testing.T) {
	// A12: 模拟局部慢块与协助；验证空闲通道对慢块的动态再拆分与文件一致性
	fileSize := 2 * 1024 * 1024 // 2MB
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("failed to generate payload: %v", err)
	}

	var activeConns int64
	var maxObservedConns int64
	var totalReqCount int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt64(&activeConns, 1)
		defer atomic.AddInt64(&activeConns, -1)

		// Track peak concurrency
		for {
			prevMax := atomic.LoadInt64(&maxObservedConns)
			if cur <= prevMax || atomic.CompareAndSwapInt64(&maxObservedConns, prevMax, cur) {
				break
			}
		}

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

		// Simulate slow chunk specifically if starting at 0:
		// deliver first 100KB fast, then stream slowly in 32KB chunks
		if start == 0 {
			chunkLen := int(end - start + 1)
			written := 0
			firstBatch := 100 * 1024
			if firstBatch > chunkLen {
				firstBatch = chunkLen
			}
			_, _ = w.Write(payload[start : start+int64(firstBatch)])
			written += firstBatch

			for written < chunkLen {
				time.Sleep(20 * time.Millisecond)
				step := 32 * 1024
				if written+step > chunkLen {
					step = chunkLen - written
				}
				_, _ = w.Write(payload[start+int64(written) : start+int64(written+step)])
				written += step
			}
			return
		}

		// Other requests (including assisted chunks) download fast
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
		MaxConcurrency: 2,
		Resumable:      true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err := downloader.Download(context.Background(), dlTask, nil)
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

	// Verification of dynamic split and assistance:
	// 1. Chunks must be dynamically split, so total chunks > initial concurrency 2
	if len(dlTask.Chunks) <= 2 {
		t.Fatalf("expected slow chunk to be dynamically split, but len(Chunks)=%d", len(dlTask.Chunks))
	}

	// 2. At least one chunk must be marked as Assisted
	assistedFound := false
	for _, c := range dlTask.Chunks {
		if c.Assisted {
			assistedFound = true
			break
		}
	}
	if !assistedFound {
		t.Fatalf("expected at least one chunk to have Assisted=true")
	}

	// 3. Peak concurrency must not exceed MaxConcurrency
	if maxObservedConns > int64(dlTask.MaxConcurrency) {
		t.Fatalf("peak concurrency %d exceeded MaxConcurrency %d", maxObservedConns, dlTask.MaxConcurrency)
	}
}

func TestHTTPDownloader_OverallSlowdown_NoExcessiveSplitting(t *testing.T) {
	// A12: 模拟整体限速（所有块都很慢），验证不进行无限无休止拆分
	fileSize := 1024 * 1024 // 1MB
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("failed to generate payload: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		// All chunks are delayed uniformly (overall slow network)
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	tmpDir := t.TempDir()

	dlTask := &task.Task{
		ID:             "task-overall-slow",
		URL:            ts.URL,
		Filename:       "overall_slow.bin",
		Directory:      tmpDir,
		TotalBytes:     int64(fileSize),
		MaxConcurrency: 2,
		Resumable:      true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err := downloader.Download(context.Background(), dlTask, nil)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	destFile := filepath.Join(tmpDir, "overall_slow.bin")
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Fatalf("content mismatch in overall slow download")
	}

	// On uniform overall slow speed, chunks shouldn't split excessively (e.g. at most 4 chunks)
	if len(dlTask.Chunks) > 4 {
		t.Fatalf("excessive splitting under overall slow network: got %d chunks", len(dlTask.Chunks))
	}
}
