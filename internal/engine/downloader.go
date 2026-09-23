// Package engine implements the HTTP chunked download engine and task queue manager.
package engine

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sheep-get/internal/credentials"
	"sheep-get/internal/logging"
	"sheep-get/internal/stallwatch"
	"sheep-get/internal/task"
)

// discardLog 是「没有日志出口」时的兜底：调用方不必处处判空。
var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

const (
	MinChunkSize           = 256 * 1024      // 256KB
	MinSplittableChunkSize = 128 * 1024      // 128KB: 动态拆分时剩余区间的最小物理底线（对半拆分后每片至少 64KB）
	DefaultMinSplitETA     = 3 * time.Second // 预计在此时间内可自然完成的块不进行拆分
	SplitWarmupDuration    = 1 * time.Second // 新块开始下载后的预热观察期
	SplitStallThreshold    = 2 * time.Second // 超过此时间无数据到达视为卡滞（Stall）
	UserAgentChrome        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

	// DefaultStallTimeout 是分片连接「连续多久没有读到任何字节」的判定上限。
	// 用空闲而不是总时长作判据：慢速服务器仍在发字节，本就不该被打断；真正无药可救的是
	// 对端保持连接却不发数据——它不产生错误，没有这道看门狗就会一直阻塞下去。
	DefaultStallTimeout = 30 * time.Second

	// retryBaseDelay / retryMaxDelay 是传输失败后重试的退避区间：指数增长并封顶。
	// 固定小间隔会把被限速的站点越推越远（429 只会更多），封顶则保证网络恢复后能及时续上。
	retryBaseDelay = 300 * time.Millisecond
	retryMaxDelay  = 10 * time.Second
	// retryMaxShift 限制指数增长的位移，避免移位溢出；到达它之后退避停在 retryMaxDelay。
	retryMaxShift = 6
)

// retryBackoff 返回第 attempt 次（从 0 起）重试前的等待时长。
func retryBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > retryMaxShift {
		attempt = retryMaxShift
	}
	delay := retryBaseDelay << attempt
	if delay > retryMaxDelay {
		return retryMaxDelay
	}
	return delay
}

var (
	ErrRangeNotSupported = errors.New("range not supported")
	ErrVersionMismatch   = errors.New("file version changed during download")
)

// HTTPProbeInfo holds the result of probing an HTTP/HTTPS resource.
type HTTPProbeInfo struct {
	TotalBytes   int64  `json:"totalBytes"`
	Resumable    bool   `json:"resumable"`
	ETag         string `json:"etag"`
	LastModified string `json:"lastModified"`
	Filename     string `json:"filename"`
	ContentType  string `json:"contentType"`
}

// HTTPDownloader handles downloading tasks via HTTP/HTTPS.
type HTTPDownloader struct {
	client             *http.Client
	customClient       bool
	mu                 sync.RWMutex
	tempDirectory      string
	useServerFileTime  bool
	defaultConcurrency int
	proxyMode          string
	customProxyAddr    string
	minSplitETA        time.Duration
	stallTimeout       time.Duration
	logger             atomic.Pointer[slog.Logger]
}

// SetLogger 设定传输日志出口。未设定时丢弃全部日志。
func (d *HTTPDownloader) SetLogger(logger *slog.Logger) {
	if logger == nil {
		d.logger.Store(discardLog)
		return
	}
	d.logger.Store(logger)
}

func (d *HTTPDownloader) log() *slog.Logger {
	if logger := d.logger.Load(); logger != nil {
		return logger
	}
	return discardLog
}

// SetStallTimeout 设定「连续多久没有读到字节就重连」的上限，供测试与调优使用。
func (d *HTTPDownloader) SetStallTimeout(timeout time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stallTimeout = timeout
}

// stallTimeoutOrDefault 返回空闲判定上限。
func (d *HTTPDownloader) stallTimeoutOrDefault() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.stallTimeout <= 0 {
		return DefaultStallTimeout
	}
	return d.stallTimeout
}

// SetMinSplitETA updates the minimum ETA threshold for dynamic chunk splitting.
func (d *HTTPDownloader) SetMinSplitETA(dur time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.minSplitETA = dur
}

// GetMinSplitETA returns the configured minimum ETA threshold for dynamic chunk splitting.
func (d *HTTPDownloader) GetMinSplitETA() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.minSplitETA <= 0 {
		return DefaultMinSplitETA
	}
	return d.minSplitETA
}

// SetTempDirectory updates the default temporary directory for in-progress part files.
func (d *HTTPDownloader) SetTempDirectory(dir string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tempDirectory = strings.TrimSpace(dir)
}

// GetTempDirectory returns the configured temporary directory.
func (d *HTTPDownloader) GetTempDirectory() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.tempDirectory
}

// SetUseServerFileTime sets whether downloaded files should apply server Last-Modified time.
func (d *HTTPDownloader) SetUseServerFileTime(enabled bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.useServerFileTime = enabled
}

// GetUseServerFileTime returns whether server Last-Modified time application is enabled.
func (d *HTTPDownloader) GetUseServerFileTime() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.useServerFileTime
}

// SetDefaultConcurrency sets the fallback chunk/worker count used only when a task read back
// from disk has a zero MaxConcurrency (legacy/edited data). Normal task creation sets a positive
// value upstream, so this fallback is self-healing for malformed persisted state, not a second
// source of truth.
func (d *HTTPDownloader) SetDefaultConcurrency(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.defaultConcurrency = n
}

// SetProxy configures the proxy mode ("direct", "system", "custom") and custom address.
func (d *HTTPDownloader) SetProxy(mode, customAddr string) error {
	transport, err := createHTTPTransport(mode, customAddr)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.proxyMode = mode
	d.customProxyAddr = customAddr
	if !d.customClient {
		d.client = &http.Client{
			Transport: transport,
			Timeout:   0,
		}
	}
	return nil
}

// GetProxy returns the current proxy mode and custom address.
func (d *HTTPDownloader) GetProxy() (string, string) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.proxyMode, d.customProxyAddr
}

// GetClientForDownload returns an *http.Client configured with the current proxy mode for a task run.
func (d *HTTPDownloader) GetClientForDownload() *http.Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.customClient {
		return d.client
	}
	transport, err := createHTTPTransport(d.proxyMode, d.customProxyAddr)
	if err != nil {
		return d.client
	}
	return &http.Client{
		Transport: transport,
		Timeout:   0,
	}
}

func createHTTPTransport(proxyMode, customAddr string) (*http.Transport, error) {
	var proxyFunc func(*http.Request) (*url.URL, error)
	switch proxyMode {
	case "direct":
		proxyFunc = nil
	case "custom":
		trimmed := strings.TrimSpace(customAddr)
		if trimmed != "" {
			parsed, err := url.Parse(trimmed)
			if err != nil {
				return nil, fmt.Errorf("invalid proxy URL: %w", err)
			}
			proxyFunc = http.ProxyURL(parsed)
		} else {
			proxyFunc = nil
		}
	default: // "system"
		proxyFunc = http.ProxyFromEnvironment
	}

	return &http.Transport{
		Proxy: proxyFunc,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}, nil
}

// GetPartPath returns the temporary file path for a task.
func (d *HTTPDownloader) GetPartPath(t *task.Task) string {
	d.mu.RLock()
	globalTemp := d.tempDirectory
	d.mu.RUnlock()

	tempDir := strings.TrimSpace(t.TempDir)
	if tempDir == "" {
		tempDir = globalTemp
	}

	if tempDir == "" || SamePath(tempDir, t.Directory) {
		return filepath.Join(t.Directory, t.Filename+".sheepget")
	}

	id := t.ID
	if id == "" {
		id = "task"
	}
	return filepath.Join(tempDir, fmt.Sprintf("%s_%s.sheepget", id, t.Filename))
}

// NewHTTPDownloader creates a new HTTPDownloader with robust Transport settings.
func NewHTTPDownloader(client *http.Client) *HTTPDownloader {
	customClient := false
	if client != nil {
		customClient = true
	} else {
		transport, _ := createHTTPTransport("system", "")
		client = &http.Client{
			Transport: transport,
			Timeout:   0,
		}
	}
	return &HTTPDownloader{
		client:       client,
		customClient: customClient,
		proxyMode:    "system",
		minSplitETA:  DefaultMinSplitETA,
	}
}

// Probe inspects URL metadata without downloading the body. creds carries the request
// context of the task (Referer/Cookie/Authorization) so expired links can be re-checked.
func (d *HTTPDownloader) Probe(ctx context.Context, urlStr string, creds credentials.RequestCredentials) (*HTTPProbeInfo, error) {
	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		req, rErr := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if rErr != nil {
			return nil, rErr
		}
		req.Header.Set("Range", "bytes=0-0")
		req.Header.Set("User-Agent", UserAgentChrome)
		req.Header.Set("Accept", "*/*")
		applyRequestHeaders(req, creds)
		req.Close = true

		resp, err = d.client.Do(req)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) {
			break
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		// Discard the unusable response so an error status is never mistaken for a successful probe.
		resp = nil
		time.Sleep(200 * time.Millisecond)
	}

	if err != nil || resp == nil {
		headReq, hErr := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
		if hErr == nil {
			headReq.Header.Set("User-Agent", UserAgentChrome)
			headReq.Header.Set("Accept", "*/*")
			applyRequestHeaders(headReq, creds)
			headReq.Close = true
			resp, err = d.client.Do(headReq)
		}
	}

	if err != nil {
		return nil, err
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("probe failed: server returned %s", resp.Status)
	}

	info := &HTTPProbeInfo{
		TotalBytes:   -1,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		ContentType:  resp.Header.Get("Content-Type"),
	}

	// Determine resumability and total size
	if resp.StatusCode == http.StatusPartialContent {
		info.Resumable = true
		contentRange := resp.Header.Get("Content-Range")
		if idx := strings.LastIndex(contentRange, "/"); idx != -1 {
			totalStr := contentRange[idx+1:]
			if total, err := strconv.ParseInt(totalStr, 10, 64); err == nil {
				info.TotalBytes = total
			}
		}
	} else if resp.Header.Get("Accept-Ranges") == "bytes" {
		info.Resumable = true
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			if total, err := strconv.ParseInt(cl, 10, 64); err == nil {
				info.TotalBytes = total
			}
		}
	} else if cl := resp.Header.Get("Content-Length"); cl != "" {
		if total, err := strconv.ParseInt(cl, 10, 64); err == nil {
			info.TotalBytes = total
		}
	}

	// Parse filename from Content-Disposition if present
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := parseContentDisposition(cd); err == nil {
			if fn, ok := params["filename"]; ok {
				info.Filename = fn
			}
		}
	}
	if info.Filename == "" {
		info.Filename = extractFilenameFromURL(urlStr)
	}

	return info, nil
}

// applyRequestHeaders copies task request context onto an outgoing request.
func applyRequestHeaders(req *http.Request, creds credentials.RequestCredentials) {
	if creds != nil {
		creds.ApplyToHTTPRequest(req)
	}
}
func parseContentDisposition(cd string) (string, map[string]string, error) {
	parts := strings.Split(cd, ";")
	disposition := strings.TrimSpace(parts[0])
	params := make(map[string]string)
	for _, part := range parts[1:] {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			k := strings.ToLower(strings.TrimSpace(kv[0]))
			v := strings.Trim(strings.TrimSpace(kv[1]), "\"")
			params[k] = v
		}
	}
	return disposition, params, nil
}

func extractFilenameFromURL(urlStr string) string {
	if name := URLFilename(urlStr); name != "" {
		return name
	}
	return DefaultFilename
}

// TransferSink 是一次传输期间的唯一对外出口。
//
// 传输期间任务的可变对象只属于传输侧：分片在工作协程里持续被改写，而对外读它的地方（落盘、
// 事件广播）会在另一条 goroutine 上序列化整个结构——共享同一个对象就是一次真正的数据竞争。
// 因此对外发布的一律是自有副本：调用方先问「要不要」，传输侧再在自己的锁内拷一份交出去。
type TransferSink interface {
	// WantsSnapshot 报告调用方此刻是否需要一份快照；节流节奏由调用方决定，这里只回答是与否。
	WantsSnapshot() bool
	// PublishSnapshot 接收一份此后不会再被改写的任务副本，由调用方决定落盘与广播的时机。
	PublishSnapshot(*task.Task)
}

// Download executes download for a task, handling single-connection or multi-connection range download.
func (d *HTTPDownloader) Download(ctx context.Context, t *task.Task, sink TransferSink) error {
	destPath := filepath.Join(t.Directory, t.Filename)
	partPath := d.GetPartPath(t)

	if err := os.MkdirAll(t.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(partPath), 0755); err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}

	client := d.GetClientForDownload()

	// If the resource is not resumable or total size is unknown, fallback to single-stream download
	if !t.Resumable || t.TotalBytes <= 0 {
		return d.downloadSingleStream(ctx, t, partPath, destPath, sink, client)
	}

	return d.downloadChunks(ctx, t, partPath, destPath, sink, client)
}

// publishProgress 把当前进度作为一份自有副本交给调用方。它只在传输侧独占任务对象时调用：
// 单流路径的读写都在同一协程上，分片路径则必须持有 coord.mu（见 publishLocked）。
func publishProgress(t *task.Task, downloaded int64, sink TransferSink) {
	if sink == nil {
		return
	}
	t.Downloaded = downloaded
	if !sink.WantsSnapshot() {
		return
	}
	sink.PublishSnapshot(t.Clone())
}

func (d *HTTPDownloader) downloadSingleStream(ctx context.Context, t *task.Task, partPath, destPath string, sink TransferSink, client *http.Client) error {

	reqCtx, watch := stallwatch.New(ctx, d.stallTimeoutOrDefault())
	defer watch.Stop()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, t.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgentChrome)
	req.Header.Set("Accept", "*/*")
	applyRequestHeaders(req, t.RequestHeaders)
	req.Close = true

	// Single stream non-resumable: always start fresh
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open part file: %w", err)
	}
	defer func() { _ = file.Close() }()

	logger := d.log()
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return watch.Err(ctx, logging.SafeError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	watch.Touch()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned error status: %s", resp.Status)
	}

	body := watch.Reader(resp.Body)
	buf := make([]byte, 64*1024)
	var downloaded int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := body.Read(buf)
		if n > 0 {
			if _, wErr := file.Write(buf[:n]); wErr != nil {
				return wErr
			}
			downloaded += int64(n)
			publishProgress(t, downloaded, sink)
		}
		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				break
			}
			// 单流没有断点可续，停摆只归类不重试：交给任务层按失败处理。
			return fmt.Errorf("single stream aborted at %d bytes: %w", downloaded, watch.Err(ctx, rErr))
		}
	}

	_ = file.Close()
	if t.LastModified == "" && resp.Header.Get("Last-Modified") != "" {
		t.LastModified = resp.Header.Get("Last-Modified")
	}
	logger.Info("single stream transfer done",
		"task", t.ID, "url", logging.SafeURL(t.URL), "bytes", downloaded, "ms", time.Since(started).Milliseconds())
	return d.commitCompletedFile(partPath, destPath, t)
}

func (d *HTTPDownloader) commitCompletedFile(partPath, destPath string, t *task.Task) error {
	// 同盘 rename、跨盘复制由 moveFile 统一处理；失败时保留分片，避免用户数据丢失。
	if err := moveFile(partPath, destPath); err != nil {
		return fmt.Errorf("failed to transfer completed file to %s: %w (temporary file preserved at %s)", destPath, err, partPath)
	}

	// Apply server Last-Modified time if enabled and present
	d.mu.RLock()
	useServerTime := d.useServerFileTime
	d.mu.RUnlock()

	if useServerTime && t != nil && t.LastModified != "" {
		if modTime, parseErr := http.ParseTime(t.LastModified); parseErr == nil {
			_ = os.Chtimes(destPath, modTime, modTime)
		}
	}

	return nil
}

type chunkTracker struct {
	claimed    bool
	active     bool
	splitCount int
	lastSplit  time.Time
	speed      int64 // bytes/sec
	lastBytes  int64
	lastUpdate time.Time
}

type chunkCoordinator struct {
	mu         sync.Mutex
	t          *task.Task
	file       *os.File
	fileMu     sync.Mutex
	totalDown  int64
	trackers   map[int]*chunkTracker
	sink       TransferSink
	downloader *HTTPDownloader
	client     *http.Client
	// requests 统计这次传输真正发出去的 HTTP 请求数（含重试与拆分出来的子块）。
	// 它是「慢」与「碎」的度量：同样的字节数下请求数暴涨，说明是在跟一个不配合的服务器死磕。
	requests atomic.Int64
}

// publishLocked 在持锁状态下把当前进度交出去。必须在持有 coord.mu 时调用：快照要与分片状态
// 取自同一时刻，否则交出去的就是一份内部互相矛盾的分片列表。
func (coord *chunkCoordinator) publishLocked(downloaded int64) {
	publishProgress(coord.t, downloaded, coord.sink)
}

func newChunkCoordinator(t *task.Task, file *os.File, downloader *HTTPDownloader, client *http.Client, sink TransferSink) *chunkCoordinator {
	coord := &chunkCoordinator{
		t:          t,
		file:       file,
		trackers:   make(map[int]*chunkTracker),
		sink:       sink,
		downloader: downloader,
		client:     client,
	}
	now := time.Now()
	for i := range t.Chunks {
		c := &t.Chunks[i]
		coord.totalDown += c.Downloaded
		coord.trackers[c.Index] = &chunkTracker{
			claimed:    c.Completed,
			active:     false,
			splitCount: 0,
			lastSplit:  now,
			lastBytes:  c.Downloaded,
			lastUpdate: now,
		}
	}
	return coord
}

func (coord *chunkCoordinator) getNextChunk(ctx context.Context) int {
	for {
		select {
		case <-ctx.Done():
			return -1
		default:
		}

		coord.mu.Lock()

		// 1. Check if all chunks are completed
		allDone := true
		for _, c := range coord.t.Chunks {
			if !c.Completed {
				allDone = false
				break
			}
		}
		if allDone {
			coord.mu.Unlock()
			return -1
		}

		// 2. Claim next available unclaimed chunk
		for i := range coord.t.Chunks {
			c := &coord.t.Chunks[i]
			trk := coord.trackers[c.Index]
			if !c.Completed && trk != nil && !trk.claimed {
				trk.claimed = true
				trk.active = true
				trk.lastBytes = c.Downloaded
				trk.lastUpdate = time.Now()
				coord.mu.Unlock()
				return c.Index
			}
		}

		// 3. Try to assist a slow chunk by dynamically splitting its remaining range
		split := coord.trySplitSlowChunkLocked()
		if split.newIdx != -1 {
			coord.mu.Unlock()
			// 日志是文件 I/O，放在锁外：拆分很频繁，锁内写日志会让所有工作协程一起等它。
			coord.downloader.log().Info("split slow chunk",
				"chunk", split.fromIdx, "remaining", split.remaining,
				"speedBps", split.speedBps, "newChunk", split.newIdx)
			return split.newIdx
		}

		// 4. Check if any workers are still active
		activeWorkers := 0
		for _, trk := range coord.trackers {
			if trk.active {
				activeWorkers++
			}
		}
		if activeWorkers == 0 {
			coord.mu.Unlock()
			return -1
		}

		coord.mu.Unlock()

		select {
		case <-ctx.Done():
			return -1
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// splitResult 是一次慢块拆分的记录。拆分在分片锁内发生，而日志要走文件 I/O，
// 因此这里把事实带出锁外，由调用方记一行。
type splitResult struct {
	newIdx    int // -1 表示这次没有拆分
	fromIdx   int
	remaining int64
	speedBps  int64
}

func (coord *chunkCoordinator) trySplitSlowChunkLocked() splitResult {
	hasCompletedChunk := false
	for _, c := range coord.t.Chunks {
		if c.Completed {
			hasCompletedChunk = true
			break
		}
	}

	now := time.Now()
	var bestIdx = -1
	var maxETA time.Duration = 0
	var maxRemaining int64 = 0

	var activeSpeeds []int64
	for _, c := range coord.t.Chunks {
		if c.Completed {
			continue
		}
		trk := coord.trackers[c.Index]
		if trk != nil && trk.active {
			activeSpeeds = append(activeSpeeds, trk.speed)
		}
	}

	// Overall slowdown protection: when no chunk has completed and speeds are uniformly low,
	// avoid excessive unhelpful splitting
	if !hasCompletedChunk && len(activeSpeeds) > 1 {
		allSlowAndUniform := true
		for _, spd := range activeSpeeds {
			if spd > 100*1024 {
				allSlowAndUniform = false
				break
			}
		}
		if allSlowAndUniform {
			return splitResult{newIdx: -1}
		}
	}

	minETA := DefaultMinSplitETA
	if coord.downloader != nil {
		minETA = coord.downloader.GetMinSplitETA()
	}

	for i := range coord.t.Chunks {
		c := &coord.t.Chunks[i]
		if c.Completed {
			continue
		}
		trk := coord.trackers[c.Index]
		if trk == nil || !trk.active {
			continue
		}

		// Limit splits per chunk to avoid unbounded fragments
		if trk.splitCount >= 3 {
			continue
		}

		// Cooldown between splits on the same chunk
		if now.Sub(trk.lastSplit) < 100*time.Millisecond {
			continue
		}

		currPos := c.Start + c.Downloaded
		remaining := c.End - currPos + 1

		// Must have at least MinSplittableChunkSize remaining to justify splitting safely
		if remaining < MinSplittableChunkSize {
			continue
		}

		var eta time.Duration
		if trk.speed > 0 {
			etaSeconds := float64(remaining) / float64(trk.speed)
			eta = time.Duration(etaSeconds * float64(time.Second))
			// If the active worker is expected to finish within minETA, do not disturb it!
			if eta <= minETA {
				continue
			}
		} else {
			// No measured speed yet: give warmup time for newly launched chunk workers
			timeSinceUpdate := now.Sub(trk.lastUpdate)
			if timeSinceUpdate < SplitWarmupDuration {
				continue
			}
			// If stalled beyond threshold, consider it a severely blocked chunk
			eta = 24 * time.Hour
		}

		if eta > maxETA || (eta == maxETA && remaining > maxRemaining) {
			maxETA = eta
			maxRemaining = remaining
			bestIdx = i
		}
	}
	if bestIdx == -1 {
		return splitResult{newIdx: -1}
	}

	// Split the remaining range in half
	origChunk := &coord.t.Chunks[bestIdx]
	currPos := origChunk.Start + origChunk.Downloaded
	remaining := origChunk.End - currPos + 1
	mid := currPos + remaining/2
	oldEnd := origChunk.End

	// Shrink original chunk end to mid
	origChunk.End = mid
	trk := coord.trackers[origChunk.Index]
	trk.splitCount++
	trk.lastSplit = now

	// Create assisted chunk for the second half
	newIdx := len(coord.t.Chunks)
	newChunk := task.Chunk{
		Index:      newIdx,
		Start:      mid + 1,
		End:        oldEnd,
		Downloaded: 0,
		Assisted:   true,
		Completed:  false,
	}
	coord.t.Chunks = append(coord.t.Chunks, newChunk)

	coord.trackers[newIdx] = &chunkTracker{
		claimed:    true,
		active:     true,
		splitCount: trk.splitCount,
		lastSplit:  now,
		lastBytes:  0,
		lastUpdate: now,
	}

	return splitResult{newIdx: newIdx, fromIdx: bestIdx, remaining: remaining, speedBps: trk.speed}
}

func (coord *chunkCoordinator) downloadChunkLoop(ctx context.Context, chunkIdx int) error {
	defer func() {
		coord.mu.Lock()
		trk := coord.trackers[chunkIdx]
		if trk != nil {
			trk.active = false
		}
		coord.mu.Unlock()
	}()

	logger := coord.downloader.log()
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		coord.mu.Lock()
		c := &coord.t.Chunks[chunkIdx]
		chunkStart := c.Start + c.Downloaded
		chunkEnd := c.End
		downloadedBefore := c.Downloaded
		if chunkStart > chunkEnd {
			c.Completed = true
			coord.mu.Unlock()
			return nil
		}
		coord.mu.Unlock()

		coord.requests.Add(1)
		requestStart := time.Now()
		chunkErr := coord.downloadChunkStream(ctx, chunkIdx, chunkStart, chunkEnd)
		if chunkErr == nil {
			coord.mu.Lock()
			c := &coord.t.Chunks[chunkIdx]
			if c.Downloaded >= (c.End - c.Start + 1) {
				c.Completed = true
			}
			coord.mu.Unlock()
			logger.Info("chunk request done",
				"chunk", chunkIdx, "range", fmt.Sprintf("%d-%d", chunkStart, chunkEnd),
				"ms", time.Since(requestStart).Milliseconds())
			return nil
		}

		// Fatal errors abort immediately
		if errors.Is(chunkErr, context.Canceled) || errors.Is(chunkErr, ErrVersionMismatch) || errors.Is(chunkErr, ErrRangeNotSupported) {
			return chunkErr
		}

		// 这次尝试有进展就把退避拉回起点：能读到字节说明连接是活的，不该越等越久。
		coord.mu.Lock()
		gained := coord.t.Chunks[chunkIdx].Downloaded - downloadedBefore
		coord.mu.Unlock()
		if gained > 0 {
			attempt = 0
		}

		delay := retryBackoff(attempt)
		attempt++
		logger.Warn("chunk request failed, retrying",
			"chunk", chunkIdx, "attempt", attempt, "backoffMs", delay.Milliseconds(),
			"stalled", errors.Is(chunkErr, stallwatch.ErrStalled),
			"ms", time.Since(requestStart).Milliseconds(), "error", logging.SafeError(chunkErr).Error())

		// Transient network drop, stall or 429 rate limit: back off and retry.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (coord *chunkCoordinator) downloadChunkStream(ctx context.Context, chunkIdx int, start, end int64) error {
	if start > end {
		return nil
	}

	// 看门狗只在这次请求上生效：对端静默挂住时主动斩断，调用方据 ErrStalled 从当前偏移重连。
	reqCtx, watch := stallwatch.New(ctx, coord.downloader.stallTimeoutOrDefault())
	defer watch.Stop()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, coord.t.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("User-Agent", UserAgentChrome)
	req.Header.Set("Accept", "*/*")
	applyRequestHeaders(req, coord.t.RequestHeaders)
	req.Close = true

	if coord.t.ETag != "" {
		req.Header.Set("If-Match", coord.t.ETag)
	} else if coord.t.LastModified != "" {
		req.Header.Set("If-Unmodified-Since", coord.t.LastModified)
	}

	resp, err := coord.client.Do(req)
	if err != nil {
		// *url.Error 会带上完整地址（含查询串里的令牌）；这里统一脱敏，日志与任务错误信息都不落明文。
		return watch.Err(ctx, logging.SafeError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	watch.Touch()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return ErrVersionMismatch
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if start >= end {
			return nil
		}
		return fmt.Errorf("range out of bounds: %d-%d", start, end)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("server rate limited (HTTP 429 Too Many Requests): retrying")
	}
	if resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusOK {
			return fmt.Errorf("%w: server returned 200 OK instead of partial content", ErrRangeNotSupported)
		}
		return fmt.Errorf("download failed: server returned status %s", resp.Status)
	}

	if coord.t.ETag != "" && resp.Header.Get("ETag") != "" && resp.Header.Get("ETag") != coord.t.ETag {
		return ErrVersionMismatch
	}

	body := watch.Reader(resp.Body)
	buf := make([]byte, 64*1024)
	offset := start
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := body.Read(buf)
		if n > 0 {
			coord.mu.Lock()
			chunk := &coord.t.Chunks[chunkIdx]
			targetEnd := chunk.End

			if offset > targetEnd {
				chunk.Completed = true
				coord.mu.Unlock()
				return nil
			}

			validN := int64(n)
			truncated := false
			if offset+validN-1 > targetEnd {
				validN = targetEnd - offset + 1
				truncated = true
			}

			coord.fileMu.Lock()
			_, wErr := coord.file.WriteAt(buf[:validN], offset)
			if wErr != nil {
				coord.fileMu.Unlock()
				coord.mu.Unlock()
				return wErr
			}
			offset += validN
			chunk.Downloaded += validN
			atomic.AddInt64(&coord.totalDown, validN)
			currTotal := atomic.LoadInt64(&coord.totalDown)
			coord.fileMu.Unlock()

			now := time.Now()
			trk := coord.trackers[chunkIdx]
			if trk != nil {
				dt := now.Sub(trk.lastUpdate)
				if dt >= 100*time.Millisecond {
					bytesDiff := chunk.Downloaded - trk.lastBytes
					trk.speed = bytesDiff * int64(time.Second) / int64(dt)
					trk.lastBytes = chunk.Downloaded
					trk.lastUpdate = now
				}
			}

			if chunk.Downloaded >= (chunk.End-chunk.Start+1) || truncated {
				chunk.Completed = true
			}
			done := chunk.Completed

			coord.publishLocked(currTotal)

			coord.mu.Unlock()

			if done {
				return nil
			}
		}

		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				coord.mu.Lock()
				chunk := &coord.t.Chunks[chunkIdx]
				if offset > chunk.End || chunk.Downloaded >= (chunk.End-chunk.Start+1) {
					chunk.Completed = true
				}
				coord.mu.Unlock()
				break
			}
			// 停摆归类比原样返回错误更重要：调用方据此知道该从当前偏移重连而不是放弃。
			return watch.Err(ctx, rErr)
		}
	}

	return nil
}

func (d *HTTPDownloader) downloadChunks(ctx context.Context, t *task.Task, partPath, destPath string, sink TransferSink, client *http.Client) error {
	if t.MaxConcurrency <= 0 {
		d.mu.RLock()
		t.MaxConcurrency = d.defaultConcurrency
		d.mu.RUnlock()
	}
	if len(t.Chunks) == 0 {
		t.Chunks = splitChunks(t.TotalBytes, t.MaxConcurrency)
	}
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open part file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if err := file.Truncate(t.TotalBytes); err != nil {
		return fmt.Errorf("failed to truncate part file: %w", err)
	}

	coord := newChunkCoordinator(t, file, d, client, sink)
	logger := d.log()
	started := time.Now()
	logger.Info("chunked transfer start",
		"task", t.ID, "url", logging.SafeURL(t.URL),
		"totalBytes", t.TotalBytes, "concurrency", t.MaxConcurrency, "chunks", len(t.Chunks))

	numWorkers := t.MaxConcurrency
	if int64(numWorkers) > t.TotalBytes/MinChunkSize && t.TotalBytes < MinChunkSize*2 {
		numWorkers = 1
	}

	innerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errChan := make(chan error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-innerCtx.Done():
					return
				default:
				}

				chunkIdx := coord.getNextChunk(innerCtx)
				if chunkIdx == -1 {
					return
				}

				wErr := coord.downloadChunkLoop(innerCtx, chunkIdx)
				if wErr != nil {
					select {
					case errChan <- wErr:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errChan)

	if err, ok := <-errChan; ok && err != nil {
		return err
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, c := range t.Chunks {
		if !c.Completed {
			return errors.New("not all chunks completed")
		}
	}

	_ = file.Close()
	logger.Info("chunked transfer done",
		"task", t.ID, "bytes", atomic.LoadInt64(&coord.totalDown),
		"requests", coord.requests.Load(), "chunks", len(t.Chunks),
		"assisted", countAssistedChunks(t.Chunks), "ms", time.Since(started).Milliseconds())
	return d.commitCompletedFile(partPath, destPath, t)
}

// countAssistedChunks 数出这次传输为协助慢块而拆分出来的子块数量。
func countAssistedChunks(chunks []task.Chunk) int {
	count := 0
	for _, c := range chunks {
		if c.Assisted {
			count++
		}
	}
	return count
}

func splitChunks(totalBytes int64, concurrency int) []task.Chunk {
	// Callers normalize concurrency to a positive value before calling.
	// If file is small, keep it as 1 chunk
	if totalBytes < MinChunkSize*2 || concurrency == 1 {
		return []task.Chunk{
			{Index: 0, Start: 0, End: totalBytes - 1, Downloaded: 0, Completed: false},
		}
	}

	chunkSize := totalBytes / int64(concurrency)
	if chunkSize < MinChunkSize {
		chunkSize = MinChunkSize
	}

	var chunks []task.Chunk
	var start int64
	idx := 0
	for start < totalBytes {
		end := start + chunkSize - 1
		if end >= totalBytes || int64(len(chunks)+1) == int64(concurrency) {
			end = totalBytes - 1
		}
		chunks = append(chunks, task.Chunk{
			Index:      idx,
			Start:      start,
			End:        end,
			Downloaded: 0,
			Completed:  false,
		})
		idx++
		start = end + 1
	}

	return chunks
}
