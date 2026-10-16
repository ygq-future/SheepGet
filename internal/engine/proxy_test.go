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

	// Switch to custom with valid addresses
	if err := dl.SetProxy("custom", "http://127.0.0.1:8888", "socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("SetProxy custom failed: %v", err)
	}
	mode, addr = dl.GetProxy()
	if mode != "custom" || addr != "http://127.0.0.1:8888" {
		t.Errorf("expected custom http://127.0.0.1:8888, got %s, %s", mode, addr)
	}
	_, addrs := dl.GetProxies()
	if len(addrs) != 2 || addrs[0] != "http://127.0.0.1:8888" || addrs[1] != "socks5://127.0.0.1:1080" {
		t.Errorf("expected GetProxies to return 2 custom proxies, got %v", addrs)
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

// TestHTTPDownloader_MidTransferProxyFallbackToDirect 验证 P0-1：
// 当任务初始通过代理正常开始分块传输，但传输中途代理服务器崩溃/断流时，
// 下载器能够自动通过直连探针验证目标服务器可达，无缝切换为直连（标记 DirectTransfer=true）并完成剩余分片下载。
func TestHTTPDownloader_MidTransferProxyFallbackToDirect(t *testing.T) {
	fileSize := 1024 * 1024 // 1MB
	payload := make([]byte, fileSize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	var directHits atomic.Int64
	var proxyHits atomic.Int64
	var proxyDead atomic.Bool

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Proxy") == "" {
			directHits.Add(1)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"mid-proxy-etag"`)
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}
		var start, end int64
		_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer targetServer.Close()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits := proxyHits.Add(1)
		// 第 1 次分块请求允许通过代理成功完成，随后模拟代理崩溃（直接断开底层连接）
		if hits > 1 || proxyDead.Load() {
			proxyDead.Store(true)
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				if conn != nil {
					_ = conn.Close()
					return
				}
			}
			http.Error(w, "proxy down", http.StatusBadGateway)
			return
		}

		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.RequestURI, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range r.Header {
			outReq.Header[k] = v
		}
		outReq.Header.Set("X-Via-Proxy", "1")
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
	if err := dl.SetProxy("custom", proxyServer.URL); err != nil {
		t.Fatalf("failed to set proxy: %v", err)
	}

	tempDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-mid-proxy-fallback",
		URL:            targetServer.URL,
		Directory:      tempDir,
		Filename:       "mid_fallback.bin",
		TotalBytes:     int64(len(payload)),
		Resumable:      true,
		ETag:           `"mid-proxy-etag"`,
		MaxConcurrency: 2,
	}

	if err := dl.Download(context.Background(), dlTask, nil); err != nil {
		t.Fatalf("expected download to succeed via mid-transfer direct fallback, got error: %v", err)
	}
	if !dlTask.DirectTransfer {
		t.Errorf("expected task.DirectTransfer to be set to true after mid-transfer proxy failure")
	}
	if directHits.Load() == 0 {
		t.Errorf("expected target server to receive direct requests after proxy died")
	}

	got, err := os.ReadFile(filepath.Join(tempDir, "mid_fallback.bin"))
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("downloaded file content mismatch")
	}
}

// TestHTTPDownloader_SingleStreamMidTransferProxyFallbackToDirect 验证 P0-1 单流场景：
// 当单流下载在读取响应体中途因代理断流报错时，自动切换为直连完成下载。
func TestHTTPDownloader_SingleStreamMidTransferProxyFallbackToDirect(t *testing.T) {
	payload := []byte("single-stream-full-payload-after-proxy-midstream-abort-1234567890")
	var directHits atomic.Int64

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Proxy") == "" {
			directHits.Add(1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer targetServer.Close()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟代理在返回了前几个字节后突然掐断 TCP 连接（unexpected EOF）
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload[:10])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			if conn != nil {
				_ = conn.Close()
			}
		}
	}))
	defer proxyServer.Close()

	dl := engine.NewHTTPDownloader(nil)
	if err := dl.SetProxy("custom", proxyServer.URL); err != nil {
		t.Fatalf("failed to set proxy: %v", err)
	}

	tempDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-single-mid-proxy",
		URL:            targetServer.URL,
		Directory:      tempDir,
		Filename:       "single_mid.bin",
		TotalBytes:     -1,
		Resumable:      false,
		MaxConcurrency: 1,
	}

	if err := dl.Download(context.Background(), dlTask, nil); err != nil {
		t.Fatalf("expected single stream download to recover via direct fallback, got: %v", err)
	}
	if !dlTask.DirectTransfer {
		t.Errorf("expected task.DirectTransfer to be true")
	}
	if directHits.Load() == 0 {
		t.Errorf("expected direct hits on targetServer")
	}
	got, err := os.ReadFile(filepath.Join(tempDir, "single_mid.bin"))
	if err != nil || string(got) != string(payload) {
		t.Fatalf("unexpected file content: %q, err=%v", string(got), err)
	}
}

// TestHTTPDownloader_MidTransferDirectBlockedFallbackToProxy 验证双向通道逃生：
// 当任务当前处于直连状态（如 mode="direct" 或系统代理未开启，但配置有备用 customProxyAddr），
// 前几个分片高速完成后源站防火墙拉黑直连 IP 并对后续请求返回 EOF 时，
// 下载器能够探测并平滑切换至备用代理通道完成剩余分片下载。
func TestHTTPDownloader_MidTransferDirectBlockedFallbackToProxy(t *testing.T) {
	fileSize := 1024 * 1024 // 1MB (4 chunks of 256KB)
	payload := make([]byte, fileSize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	var directReqs atomic.Int64
	var proxyReqs atomic.Int64
	var directBanned atomic.Bool

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viaProxy := r.Header.Get("X-Via-Fallback-Proxy") == "1"
		if viaProxy {
			proxyReqs.Add(1)
		} else {
			hits := directReqs.Add(1)
			// 模拟：前 2 个直连请求正常放行，从第 3 个直连请求起源站封禁直连 IP（直接关闭连接产生 EOF）
			if hits > 2 || directBanned.Load() {
				directBanned.Store(true)
				if hj, ok := w.(http.Hijacker); ok {
					conn, _, _ := hj.Hijack()
					if conn != nil {
						_ = conn.Close()
						return
					}
				}
				return
			}
		}

		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"direct-ban-etag"`)
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}
		var start, end int64
		_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer targetServer.Close()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.RequestURI, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range r.Header {
			outReq.Header[k] = v
		}
		outReq.Header.Set("X-Via-Fallback-Proxy", "1")
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
	// 模拟用户曾配置 customProxyAddr，但当前切换为 direct 模式（或 system 模式未开启系统代理）
	if err := dl.SetProxy("direct", proxyServer.URL); err != nil {
		t.Fatalf("failed to configure direct mode with fallback proxy addr: %v", err)
	}

	tempDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-direct-banned-fallback-proxy",
		URL:            targetServer.URL,
		Directory:      tempDir,
		Filename:       "direct_banned_recover.apk",
		TotalBytes:     int64(len(payload)),
		Resumable:      true,
		ETag:           `"direct-ban-etag"`,
		MaxConcurrency: 4,
	}

	if err := dl.Download(context.Background(), dlTask, nil); err != nil {
		t.Fatalf("expected download to recover via fallback proxy after direct IP ban, got error: %v", err)
	}
	if !directBanned.Load() {
		t.Errorf("expected direct connection ban to be triggered")
	}
	if proxyReqs.Load() == 0 {
		t.Errorf("expected remaining chunks to be downloaded through fallback proxy")
	}

	got, err := os.ReadFile(filepath.Join(tempDir, "direct_banned_recover.apk"))
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("downloaded file content mismatch")
	}
}

// TestHTTPDownloader_MultiProxyFallbackChain 验证多自定义代理轮询回退链：
// 1. 当配置了 [P1, P2, P3] 且 P1 故障、P2 可用时，Probe 与下载依次轮询并锁定 P2（DirectTransfer=false, ProxyAddr=P2）；
// 2. 当下载中途 P2 也故障但 P3 可用时，继续向后轮询切换至 P3，而不直接跳到直连；
// 3. 当 [P1, P2, P3] 全部故障时，最终回退至直连（DirectTransfer=true）。
func TestHTTPDownloader_MultiProxyFallbackChain(t *testing.T) {
	fileSize := 1024 * 1024 // 1MB
	payload := make([]byte, fileSize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	var directHits atomic.Int64
	var p1Hits, p2Hits, p3Hits atomic.Int64
	var p2Dead, p3Dead atomic.Bool

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Proxy") == "" {
			directHits.Add(1)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"multi-proxy-etag"`)
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" || r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(payload)
			}
			return
		}
		var start, end int64
		_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer targetServer.Close()

	forwardViaProxy := func(w http.ResponseWriter, r *http.Request, tag string) {
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.RequestURI, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range r.Header {
			outReq.Header[k] = v
		}
		outReq.Header.Set("X-Via-Proxy", tag)
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
	}

	// P1: 始终故障 (502)
	proxy1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p1Hits.Add(1)
		http.Error(w, "p1 down", http.StatusBadGateway)
	}))
	defer proxy1.Close()

	// P2: 探测阶段可用，传输第 1 个分片后崩溃
	proxy2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits := p2Hits.Add(1)
		if p2Dead.Load() || hits > 2 {
			p2Dead.Store(true)
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				if conn != nil {
					_ = conn.Close()
					return
				}
			}
			http.Error(w, "p2 down", http.StatusBadGateway)
			return
		}
		forwardViaProxy(w, r, "P2")
	}))
	defer proxy2.Close()

	// P3: 当 p3Dead=false 时可用，用于承接 P2 崩溃后的回退
	proxy3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p3Hits.Add(1)
		if p3Dead.Load() {
			http.Error(w, "p3 down", http.StatusBadGateway)
			return
		}
		forwardViaProxy(w, r, "P3")
	}))
	defer proxy3.Close()

	dl := engine.NewHTTPDownloader(nil)
	if err := dl.SetProxy("custom", proxy1.URL, proxy2.URL, proxy3.URL); err != nil {
		t.Fatalf("SetProxy failed: %v", err)
	}

	// Step 1: Probe 应当尝试 P1 (失败) -> 轮询到 P2 (成功)，且不触发直连
	info, err := dl.Probe(context.Background(), targetServer.URL+"/file.bin", nil)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if info.DirectTransfer {
		t.Fatalf("expected DirectTransfer=false when P2 succeeded")
	}
	if info.ProxyAddr != proxy2.URL {
		t.Fatalf("expected Probe to lock onto P2 (%s), got %q", proxy2.URL, info.ProxyAddr)
	}
	if directHits.Load() != 0 {
		t.Fatalf("expected 0 direct hits when P2 is alive, got %d", directHits.Load())
	}

	// Step 2: 下载任务从 P2 开始，中途 P2 崩溃，应当切换到 P3 完成下载，仍不走直连
	tempDir := t.TempDir()
	dlTask := &task.Task{
		ID:             "task-multi-proxy-p2-to-p3",
		URL:            targetServer.URL + "/file.bin",
		Directory:      tempDir,
		Filename:       "p2_to_p3.bin",
		TotalBytes:     int64(len(payload)),
		Resumable:      true,
		ETag:           `"multi-proxy-etag"`,
		ProxyAddr:      info.ProxyAddr,
		MaxConcurrency: 2,
	}
	if err := dl.Download(context.Background(), dlTask, nil); err != nil {
		t.Fatalf("Download P2->P3 failed: %v", err)
	}
	if dlTask.DirectTransfer {
		t.Errorf("expected task to finish via P3 without falling back to direct")
	}
	if dlTask.ProxyAddr != proxy3.URL {
		t.Errorf("expected task.ProxyAddr to update to P3 (%s), got %q", proxy3.URL, dlTask.ProxyAddr)
	}
	if directHits.Load() != 0 {
		t.Errorf("expected 0 direct hits during P2->P3 fallback, got %d", directHits.Load())
	}

	// Step 3: 当 P1, P2, P3 全部故障时，最终回退至 direct
	p2Dead.Store(true)
	p3Dead.Store(true)
	infoDirect, err := dl.Probe(context.Background(), targetServer.URL+"/file.bin", nil)
	if err != nil {
		t.Fatalf("expected Probe to fall back to direct when all proxies fail, got: %v", err)
	}
	if !infoDirect.DirectTransfer {
		t.Errorf("expected DirectTransfer=true when all custom proxies fail")
	}
	if infoDirect.ProxyAddr != "" {
		t.Errorf("expected empty ProxyAddr on direct fallback, got %q", infoDirect.ProxyAddr)
	}
}
