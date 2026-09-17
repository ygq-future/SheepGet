package engine_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

func TestHTTPDownloader_ProxyConfiguration(t *testing.T) {
	dl := engine.NewHTTPDownloader(nil)

	// Default mode is system
	mode, addr := dl.GetProxy()
	if mode != "system" {
		t.Errorf("expected default mode 'system', got %q", mode)
	}
	if addr != "" {
		t.Errorf("expected empty custom addr, got %q", addr)
	}

	// Switch to direct
	if err := dl.SetProxy("direct", ""); err != nil {
		t.Fatalf("SetProxy direct failed: %v", err)
	}
	mode, addr = dl.GetProxy()
	if mode != "direct" {
		t.Errorf("expected mode 'direct', got %q", mode)
	}
	if addr != "" {
		t.Errorf("expected empty addr in direct mode, got %q", addr)
	}

	// Switch to custom with valid address
	if err := dl.SetProxy("custom", "http://127.0.0.1:8888"); err != nil {
		t.Fatalf("SetProxy custom failed: %v", err)
	}
	mode, addr = dl.GetProxy()
	if mode != "custom" || addr != "http://127.0.0.1:8888" {
		t.Errorf("expected custom http://127.0.0.1:8888, got %s, %s", mode, addr)
	}

	// Invalid custom address returns error
	if err := dl.SetProxy("custom", "://bad-url"); err == nil {
		t.Errorf("expected error for invalid custom proxy url")
	}
}

func TestHTTPDownloader_ProxyIsolation(t *testing.T) {
	var proxyHits int64

	// Target server providing file
	payload := []byte("hello world through proxy or direct")
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			_, _ = w.Write(payload)
			return
		}
		var start, end int64
		_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer targetServer.Close()

	// Simple HTTP proxy server
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&proxyHits, 1)

		// Forward request to target
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.RequestURI, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range r.Header {
			outReq.Header[k] = v
		}
		resp, err := http.DefaultTransport.RoundTrip(outReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxyServer.Close()

	dl := engine.NewHTTPDownloader(nil)

	// Configure proxy
	if err := dl.SetProxy("custom", proxyServer.URL); err != nil {
		t.Fatalf("failed to set proxy: %v", err)
	}

	tempDir := t.TempDir()
	task1 := &task.Task{
		ID:             "task-proxy-1",
		URL:            targetServer.URL,
		Directory:      tempDir,
		Filename:       "file1.bin",
		TotalBytes:     int64(len(payload)),
		Resumable:      true,
		MaxConcurrency: 1,
	}

	// Task 1 download should route through proxy
	err := dl.Download(context.Background(), task1, nil)
	if err != nil {
		t.Fatalf("task1 download failed: %v", err)
	}

	initialHits := atomic.LoadInt64(&proxyHits)
	if initialHits == 0 {
		t.Fatalf("expected proxyHits > 0 for task1, got %d", initialHits)
	}

	// Now switch downloader to direct mode
	if err := dl.SetProxy("direct", ""); err != nil {
		t.Fatalf("failed to set direct: %v", err)
	}

	// Task 2 download should NOT hit the proxy server
	task2 := &task.Task{
		ID:             "task-proxy-2",
		URL:            targetServer.URL,
		Directory:      tempDir,
		Filename:       "file2.bin",
		TotalBytes:     int64(len(payload)),
		Resumable:      true,
		MaxConcurrency: 1,
	}

	err = dl.Download(context.Background(), task2, nil)
	if err != nil {
		t.Fatalf("task2 download failed: %v", err)
	}

	afterHits := atomic.LoadInt64(&proxyHits)
	if afterHits != initialHits {
		t.Errorf("task2 in direct mode should not hit proxy: initialHits=%d, afterHits=%d", initialHits, afterHits)
	}

	// Verify downloaded content matches
	b1, _ := os.ReadFile(filepath.Join(tempDir, "file1.bin"))
	b2, _ := os.ReadFile(filepath.Join(tempDir, "file2.bin"))
	if string(b1) != string(payload) || string(b2) != string(payload) {
		t.Errorf("downloaded file contents do not match payload")
	}
}
