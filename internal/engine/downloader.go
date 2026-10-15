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
	retryMaxDelay  = 30 * time.Second
	// retryMaxShift 限制指数增长的位移，避免移位溢出；到达它之后退避停在 retryMaxDelay。
	retryMaxShift = 7

	// maxChunkRetries 是单分块在无有效数据推进时的最大连续重试次数。
	// 6 次重试累计约 40~50 秒退避，最大退避封顶 30 秒；持续失败时及时退出向任务上报错误，
	// 既给临时网络抖动与限流留足自愈窗口，又避免在断网或服务端异常时无休止卡在正在下载状态。
	maxChunkRetries = 6
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

// FatalError 表示传输期间遭遇的不可恢复错误（如 401/403/404/618 等致命状态码）。
// 遇到此类错误时分块下载器不应继续重试，应立即中断传输上报失败。
type FatalError struct {
	Err error
}

func (e *FatalError) Error() string {
	return e.Err.Error()
}

func (e *FatalError) Unwrap() error {
	return e.Err
}

// IsFatalError 检查目标错误是否为不可恢复错误。
func IsFatalError(err error) bool {
	var fatal *FatalError
	return errors.As(err, &fatal)
}

// isFatalStatusCode 检查 HTTP 状态码是否属于不可恢复的致命状态。
// 4xx 客户端错误（除 408 请求超时和 429 限流外）如 400, 401, 403, 404, 410 等，
// 以及 6xx 鉴权/过期状态（如 Azure/CDN 私有错误 618 jwt:expired 等），均代表链接失效、无权访问或资源丢失，
// 再次重试不会改变结果，必须直接中止。
func isFatalStatusCode(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return false
	}
	if code >= 400 && code < 500 {
		return true
	}
	if code >= 600 && code < 700 {
		return true
	}
	return false
}

// HTTPProbeInfo holds the result of probing an HTTP/HTTPS resource.
type HTTPProbeInfo struct {
	TotalBytes     int64  `json:"totalBytes"`
	Resumable      bool   `json:"resumable"`
	ETag           string `json:"etag"`
	LastModified   string `json:"lastModified"`
	Filename       string `json:"filename"`
	ContentType    string `json:"contentType"`
	FinalURL       string `json:"finalUrl,omitempty"`
	DirectTransfer bool   `json:"directTransfer,omitempty"`
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

// GetClientForTask returns an *http.Client configured for the specific task run.
// If the task is marked for direct transfer (e.g. fallback when proxy failed during probe), it uses a direct connection.
func (d *HTTPDownloader) GetClientForTask(t *task.Task) *http.Client {
	if t != nil && t.DirectTransfer {
		if directClient := d.newDirectClient(); directClient != nil {
			return directClient
		}
	}
	return d.GetClientForDownload()
}

// NewDirectFallbackClient 当下载器当前配置了代理且未使用测试注入的固定 customClient 时，
// 返回一个绕过代理的直连 *http.Client，供传输中途或 HLS 代理故障逃生使用。
func (d *HTTPDownloader) NewDirectFallbackClient() *http.Client {
	if d == nil || d.customClient || !d.isProxyConfigured() {
		return nil
	}
	return d.newDirectClient()
}

func (d *HTTPDownloader) newDirectClient() *http.Client {
	tr, err := createHTTPTransport("direct", "")
	if err != nil {
		return nil
	}
	return &http.Client{
		Transport: tr,
		Timeout:   0,
	}
}

func (d *HTTPDownloader) isProxyConfigured() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.proxyMode == "custom" && strings.TrimSpace(d.customProxyAddr) != "" {
		return true
	}
	if d.proxyMode == "system" {
		return isSystemProxyActive()
	}
	return false
}

// RefreshFinalURL 使用任务的原始入口 t.URL 重新探测并刷新过期的重定向地址 FinalURL。
// 仅当新探测结果与原任务属于同一份文件（大小、断点支持及 ETag/Last-Modified 校验一致）时才采纳新地址，
// 避免把错误页面或变更后的文件拼进已有分片。
func (d *HTTPDownloader) RefreshFinalURL(ctx context.Context, client *http.Client, t *task.Task) (string, bool) {
	if t == nil || t.URL == "" {
		return "", false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	info, err := d.probeWithClient(probeCtx, client, t.URL, t.RequestHeaders)
	if err != nil && !t.DirectTransfer {
		if directClient := d.NewDirectFallbackClient(); directClient != nil {
			if directInfo, directErr := d.probeWithClient(probeCtx, directClient, t.URL, t.RequestHeaders); directErr == nil {
				info = directInfo
				err = nil
			}
		}
	}
	if err != nil || info == nil || info.FinalURL == "" {
		return "", false
	}
	if t.TotalBytes > 0 && info.TotalBytes > 0 && t.TotalBytes != info.TotalBytes {
		return "", false
	}
	if t.Downloaded > 0 && !info.Resumable {
		return "", false
	}
	if t.ETag != "" && info.ETag != "" && t.ETag != info.ETag {
		return "", false
	}
	if t.ETag == "" && t.LastModified != "" && info.LastModified != "" && t.LastModified != info.LastModified {
		return "", false
	}
	return info.FinalURL, true
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
		proxyFunc = systemProxyFunc()
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
	info, err := d.probeWithClient(ctx, d.client, urlStr, creds)
	if err == nil {
		return info, nil
	}

	// 若当前处于代理模式（custom 代理或启用的 system 代理），且探测失败（超时/连接重置等）：
	// 尝试一次使用直连探测；若直连成功，则本次下载标记为直连下载（DirectTransfer = true）。
	if !d.customClient && d.isProxyConfigured() {
		directTr, trErr := createHTTPTransport("direct", "")
		if trErr == nil {
			directClient := &http.Client{Transport: directTr, Timeout: 0}
			directCtx := ctx
			if ctx.Err() != nil {
				var cancel context.CancelFunc
				directCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
			}
			if directInfo, directErr := d.probeWithClient(directCtx, directClient, urlStr, creds); directErr == nil {
				d.log().Info("probe succeeded via direct connection fallback, task will use direct transfer", "url", logging.SafeURL(urlStr))
				directInfo.DirectTransfer = true
				return directInfo, nil
			}
		}
	}

	return nil, err
}

func (d *HTTPDownloader) probeWithClient(ctx context.Context, client *http.Client, urlStr string, creds credentials.RequestCredentials) (*HTTPProbeInfo, error) {
	var resp *http.Response
	var err error
	for range 2 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req, rErr := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if rErr != nil {
			return nil, rErr
		}
		req.Header.Set("Range", "bytes=0-0")
		req.Header.Set("User-Agent", UserAgentChrome)
		req.Header.Set("Accept", "*/*")
		applyRequestHeaders(req, creds)

		resp, err = client.Do(req)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) {
			break
		}

		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		// Discard the unusable response so an error status is never mistaken for a successful probe.
		resp = nil

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// 明确的客户端错误（404/403/401 等，除 405 Method Not Allowed 外）重试无意义，直接跳出尝试 HEAD 或返回
		if statusCode >= 400 && statusCode < 500 && statusCode != http.StatusMethodNotAllowed {
			break
		}

		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if (err != nil || resp == nil) && ctx.Err() == nil {
		headReq, hErr := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
		if hErr == nil {
			headReq.Header.Set("User-Agent", UserAgentChrome)
			headReq.Header.Set("Accept", "*/*")
			applyRequestHeaders(headReq, creds)
			resp, err = client.Do(headReq)
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

	finalURL := urlStr
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		if u := resp.Request.URL.String(); u != "" {
			finalURL = u
		}
	}

	info := &HTTPProbeInfo{
		TotalBytes:   -1,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		ContentType:  resp.Header.Get("Content-Type"),
		FinalURL:     finalURL,
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
		if fn := ParseContentDispositionFilename(cd); fn != "" {
			info.Filename = fn
		}
	}
	if info.Filename == "" {
		if finalURL != "" {
			info.Filename = extractFilenameFromURL(finalURL)
		}
		if info.Filename == "" {
			info.Filename = extractFilenameFromURL(urlStr)
		}
	}

	return info, nil
}

// applyRequestHeaders copies task request context onto an outgoing request.
func applyRequestHeaders(req *http.Request, creds credentials.RequestCredentials) {
	if creds != nil {
		creds.ApplyToHTTPRequest(req)
	}
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
	tempDir := filepath.Dir(partPath)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	if err := checkPathsDiskSpace(tempDir, t.Directory, partPath, t.TotalBytes); err != nil {
		return err
	}

	client := d.GetClientForTask(t)

	// If the resource is not resumable or total size is unknown, fallback to single-stream download
	if !t.Resumable || t.TotalBytes <= 0 {
		return d.downloadSingleStream(ctx, t, partPath, destPath, sink, client, true)
	}

	err := d.downloadChunks(ctx, t, partPath, destPath, sink, client)
	if errors.Is(err, ErrRangeNotSupported) && ctx.Err() == nil {
		d.log().Warn("server ignored Range header and returned 200 OK, downgrading to single-stream download",
			"task", t.ID, "url", logging.SafeURL(t.URL))
		t.Resumable = false
		t.Chunks = nil
		t.Downloaded = 0
		publishProgress(t, 0, sink)
		return d.downloadSingleStream(ctx, t, partPath, destPath, sink, client, false)
	}
	return err
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
func initSingleResumeChunk(t *task.Task, downloaded int64) bool {
	if !t.Resumable || t.TotalBytes <= 0 || downloaded <= 0 || downloaded >= t.TotalBytes {
		return false
	}
	t.Chunks = []task.Chunk{{
		Index:      0,
		Start:      0,
		End:        t.TotalBytes - 1,
		Downloaded: downloaded,
	}}
	t.Downloaded = downloaded
	return true
}

func (d *HTTPDownloader) downloadSingleStream(ctx context.Context, t *task.Task, partPath, destPath string, sink TransferSink, client *http.Client, allowRangeUpgrade bool) error {
	logger := d.log()
	started := time.Now()

	for attempt := range 2 {
		reqCtx, watch := stallwatch.New(ctx, d.stallTimeoutOrDefault())

		doSingleReq := func(hc *http.Client, targetURL string) (*http.Response, error) {
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", UserAgentChrome)
			req.Header.Set("Accept", "*/*")
			applyRequestHeaders(req, t.RequestHeaders)
			req.Close = true
			return hc.Do(req)
		}

		// Single stream non-resumable: always start fresh
		file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			watch.Stop()
			return fmt.Errorf("failed to open part file: %w", err)
		}

		resp, err := doSingleReq(client, t.EffectiveURL())
		if (err != nil || (resp != nil && (resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout))) && !t.DirectTransfer && ctx.Err() == nil {
			if directClient := d.NewDirectFallbackClient(); directClient != nil {
				if resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
				if directResp, directErr := doSingleReq(directClient, t.EffectiveURL()); directErr == nil && directResp.StatusCode < 400 {
					t.DirectTransfer = true
					client = directClient
					resp = directResp
					err = nil
					logger.Info("single stream switched to direct connection fallback", "task", t.ID)
				}
			}
		}
		if err == nil && resp != nil &&
			(resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone) &&
			t.FinalURL != "" && t.FinalURL != t.URL && ctx.Err() == nil {
			if refreshedURL, ok := d.RefreshFinalURL(ctx, client, t); ok && refreshedURL != t.FinalURL {
				_ = resp.Body.Close()
				t.FinalURL = refreshedURL
				logger.Info("single stream refreshed expired FinalURL from origin", "task", t.ID)
				resp, err = doSingleReq(client, t.EffectiveURL())
			}
		}
		if err != nil {
			_ = file.Close()
			mappedErr := watch.Err(ctx, logging.SafeError(err))
			watch.Stop()
			return mappedErr
		}
		watch.Touch()

		if resp.Request != nil && resp.Request.URL != nil {
			if final := resp.Request.URL.String(); final != "" && t.FinalURL == "" {
				t.FinalURL = final
			}
		}
		if resp.StatusCode >= 400 {
			_ = resp.Body.Close()
			_ = file.Close()
			watch.Stop()
			return fmt.Errorf("server returned error status: %s", resp.Status)
		}
		var downloaded int64
		if t.ETag == "" && resp.Header.Get("ETag") != "" {
			t.ETag = resp.Header.Get("ETag")
		}
		if t.LastModified == "" && resp.Header.Get("Last-Modified") != "" {
			t.LastModified = resp.Header.Get("Last-Modified")
		}
		if allowRangeUpgrade && (resp.Header.Get("Accept-Ranges") == "bytes" || resp.StatusCode == http.StatusPartialContent) {
			t.Resumable = true
		}
		// 动态回填响应头大小与续传支持：如果任务之前未探出大小（totalBytes <= 0），从真实响应头中提取
		if t.TotalBytes <= 0 {
			cl := resp.ContentLength
			if cl <= 0 && resp.Header.Get("Content-Length") != "" {
				if parsed, pErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); pErr == nil && parsed > 0 {
					cl = parsed
				}
			}
			if cl > 0 {
				t.TotalBytes = cl
				logger.Info("resolved total size from single stream response header", "task", t.ID, "totalBytes", t.TotalBytes, "resumable", t.Resumable)
				publishProgress(t, downloaded, sink)
				if err := checkPathsDiskSpace(filepath.Dir(partPath), t.Directory, partPath, t.TotalBytes); err != nil {
					_ = resp.Body.Close()
					_ = file.Close()
					watch.Stop()
					return err
				}
			}
		}
		body := watch.Reader(resp.Body)
		buf := make([]byte, 64*1024)
		var readErr error
		for {
			select {
			case <-ctx.Done():
				_ = resp.Body.Close()
				_ = file.Close()
				watch.Stop()
				if allowRangeUpgrade {
					initSingleResumeChunk(t, downloaded)
				}
				return ctx.Err()
			default:
			}

			n, rErr := body.Read(buf)
			if n > 0 {
				if _, wErr := file.Write(buf[:n]); wErr != nil {
					_ = resp.Body.Close()
					_ = file.Close()
					watch.Stop()
					return wErr
				}
				downloaded += int64(n)
				publishProgress(t, downloaded, sink)
			}
			if rErr != nil {
				if !errors.Is(rErr, io.EOF) {
					readErr = watch.Err(ctx, rErr)
				}
				break
			}
		}
		if readErr == nil && t.TotalBytes > 0 && downloaded < t.TotalBytes {
			readErr = io.ErrUnexpectedEOF
		}

		_ = resp.Body.Close()
		_ = file.Close()
		watch.Stop()

		if readErr != nil {
			// 若单流在传输中途因代理断流中断，且存在可用的直连通道，切换为直连恢复传输
			if attempt == 0 && !t.DirectTransfer && ctx.Err() == nil {
				if directClient := d.NewDirectFallbackClient(); directClient != nil {
					probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
					_, pErr := d.probeWithClient(probeCtx, directClient, t.EffectiveURL(), t.RequestHeaders)
					cancel()
					if pErr == nil {
						t.DirectTransfer = true
						client = directClient
						logger.Info("single stream mid-transfer proxy failed, retrying via direct connection", "task", t.ID)
						if allowRangeUpgrade && initSingleResumeChunk(t, downloaded) {
							return d.downloadChunks(ctx, t, partPath, destPath, sink, directClient)
						}
						continue
					}
				}
			}
			// P1-1: 单流下载中途已识别出服务端支持断点续传（Accept-Ranges: bytes）且文件总大小已知：
			// 断流或取消时保留已下载进度，若非主动取消则直接无缝升级为分块断点续传完成剩余字节。
			if allowRangeUpgrade && initSingleResumeChunk(t, downloaded) {
				if ctx.Err() == nil {
					logger.Info("single stream interrupted mid-transfer, upgrading to resumable chunked download",
						"task", t.ID, "downloaded", downloaded, "totalBytes", t.TotalBytes)
					return d.downloadChunks(ctx, t, partPath, destPath, sink, client)
				}
			}
			return fmt.Errorf("single stream aborted at %d bytes: %w", downloaded, readErr)
		}
		if t.TotalBytes <= 0 && downloaded > 0 {
			t.TotalBytes = downloaded
		}
		if t.LastModified == "" && resp.Header.Get("Last-Modified") != "" {
			t.LastModified = resp.Header.Get("Last-Modified")
		}
		logger.Info("single stream transfer done",
			"task", t.ID, "url", logging.SafeURL(t.URL), "bytes", downloaded, "ms", time.Since(started).Milliseconds())
		return d.commitCompletedFile(partPath, destPath, t)
	}
	return errors.New("single stream exhausted attempts")
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
	claimed      bool
	active       bool
	splitCount   int
	lastSplit    time.Time
	speed        int64 // bytes/sec
	lastBytes    int64
	lastUpdate   time.Time
	retries      int
	backoffUntil time.Time
}

type chunkCoordinator struct {
	mu                  sync.Mutex
	t                   *task.Task
	file                *os.File
	fileMu              sync.Mutex
	totalDown           int64
	trackers            map[int]*chunkTracker
	sink                TransferSink
	downloader          *HTTPDownloader
	client              *http.Client
	adaptive            *AdaptiveConcurrency
	hasSucceededOnce    atomic.Bool
	consecutiveConnFail atomic.Int32
	refreshMu           sync.Mutex
	lastURLRefresh      time.Time
	// requests 统计这次传输真正发出去的 HTTP 请求数（含重试与拆分出来的子块）。
	// 它是「慢」与「碎」的度量：同样的字节数下请求数暴涨，说明是在跟一个不配合的服务器死磕。
	requests atomic.Int64
	lastErr  error
}

// publishLocked 在持锁状态下把当前进度交出去。必须在持有 coord.mu 时调用：快照要与分片状态
// 取自同一时刻，否则交出去的就是一份内部互相矛盾的分片列表。
func (coord *chunkCoordinator) publishLocked(downloaded int64) {
	publishProgress(coord.t, downloaded, coord.sink)
}
func (coord *chunkCoordinator) resetTrackersLocked() {
	for _, trk := range coord.trackers {
		if trk != nil {
			trk.retries = 0
			trk.backoffUntil = time.Time{}
		}
	}
}

// trySwitchToDirect 在传输中途代理失效（连续连接失败或网关 502/504）时，
// 用直连探针验证目标服务器是否可达；若直连可达，则将当前任务平滑切换为直连传输，保留全部已下载分片。
func (coord *chunkCoordinator) trySwitchToDirect(ctx context.Context) bool {
	if coord.downloader == nil || ctx.Err() != nil {
		return false
	}
	coord.mu.Lock()
	alreadyDirect := coord.t.DirectTransfer
	targetURL := coord.t.EffectiveURL()
	creds := coord.t.RequestHeaders
	coord.mu.Unlock()

	if alreadyDirect {
		return false
	}
	directClient := coord.downloader.NewDirectFallbackClient()
	if directClient == nil {
		return false
	}

	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, err := coord.downloader.probeWithClient(probeCtx, directClient, targetURL, creds); err != nil {
		return false
	}

	coord.mu.Lock()
	if !coord.t.DirectTransfer {
		coord.t.DirectTransfer = true
		coord.client = directClient
		coord.consecutiveConnFail.Store(0)
		coord.resetTrackersLocked()
		coord.downloader.log().Info("chunked transfer switched to direct connection fallback mid-transfer",
			"task", coord.t.ID, "url", logging.SafeURL(coord.t.URL))
	}
	coord.mu.Unlock()
	return true
}

// tryRefreshExpiredFinalURL 当任务使用的是 302 重定向后的临时签名地址（FinalURL != URL），
// 且传输或续传中遭遇 401/403/410 等鉴权过期状态码时，尝试回源请求原始 URL 获取新签名。
func (coord *chunkCoordinator) tryRefreshExpiredFinalURL(ctx context.Context, failedURL string) bool {
	if coord.downloader == nil || ctx.Err() != nil {
		return false
	}
	coord.mu.Lock()
	origURL := coord.t.URL
	currFinal := coord.t.FinalURL
	client := coord.client
	coord.mu.Unlock()

	if origURL == "" || currFinal == "" || origURL == currFinal {
		return false
	}

	coord.refreshMu.Lock()
	defer coord.refreshMu.Unlock()

	// 若另一个并发协程刚刚已经完成了刷新，直接复用新地址重试即可。
	coord.mu.Lock()
	latestFinal := coord.t.FinalURL
	coord.mu.Unlock()
	if latestFinal != "" && latestFinal != failedURL {
		return true
	}
	if time.Since(coord.lastURLRefresh) < 2*time.Second {
		return false
	}
	coord.lastURLRefresh = time.Now()

	newFinal, ok := coord.downloader.RefreshFinalURL(ctx, client, coord.t)
	if !ok || newFinal == "" || newFinal == failedURL {
		return false
	}

	coord.mu.Lock()
	coord.t.FinalURL = newFinal
	coord.resetTrackersLocked()
	coord.mu.Unlock()
	coord.downloader.log().Info("refreshed expired FinalURL from origin URL",
		"task", coord.t.ID, "url", logging.SafeURL(origURL))
	return true
}

func newChunkCoordinator(t *task.Task, file *os.File, downloader *HTTPDownloader, client *http.Client, sink TransferSink) *chunkCoordinator {
	coord := &chunkCoordinator{
		t:          t,
		file:       file,
		trackers:   make(map[int]*chunkTracker),
		sink:       sink,
		downloader: downloader,
		client:     client,
		adaptive:   NewAdaptiveConcurrency(t.MaxConcurrency, min(t.MaxConcurrency, 2)),
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

func (coord *chunkCoordinator) countActiveWorkersLocked() int {
	active := 0
	for _, trk := range coord.trackers {
		if trk.active {
			active++
		}
	}
	return active
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
		// 2. 检查当前活跃 worker 数量是否已达自适应并发窗口上限
		activeWorkers := coord.countActiveWorkersLocked()
		allowedLimit := coord.adaptive.CurrentLimit()
		if activeWorkers >= allowedLimit {
			coord.mu.Unlock()
			select {
			case <-ctx.Done():
				return -1
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}
		// 3. Claim next available unclaimed chunk (excluding chunks in backoff cooldown)
		now := time.Now()
		var earliestBackoff time.Time
		hasPendingChunks := false
		for i := range coord.t.Chunks {
			c := &coord.t.Chunks[i]
			trk := coord.trackers[c.Index]
			if !c.Completed && trk != nil && !trk.claimed {
				hasPendingChunks = true
				if !trk.backoffUntil.IsZero() && now.Before(trk.backoffUntil) {
					if earliestBackoff.IsZero() || trk.backoffUntil.Before(earliestBackoff) {
						earliestBackoff = trk.backoffUntil
					}
					continue
				}
				trk.claimed = true
				trk.active = true
				trk.lastBytes = c.Downloaded
				trk.lastUpdate = now
				trk.backoffUntil = time.Time{}
				coord.mu.Unlock()
				return c.Index
			}
		}

		// 4. Try to assist a slow chunk by dynamically splitting its remaining range
		split := coord.trySplitSlowChunkLocked()
		if split.newIdx != -1 {
			coord.mu.Unlock()
			// 日志是文件 I/O，放在锁外：拆分很频繁，锁内写日志会让所有工作协程一起等它。
			coord.downloader.log().Info("split slow chunk",
				"chunk", split.fromIdx, "remaining", split.remaining,
				"speedBps", split.speedBps, "newChunk", split.newIdx)
			return split.newIdx
		}

		// 5. Check if any workers are still active
		activeWorkers = coord.countActiveWorkersLocked()
		if activeWorkers == 0 {
			// 若没有活跃 worker，但有分块处于退避隔离中，等待最早的退避到期进行自愈替补
			if hasPendingChunks && !earliestBackoff.IsZero() {
				waitDur := time.Until(earliestBackoff)
				if waitDur > 2*time.Second {
					waitDur = 2 * time.Second
				}
				if waitDur < 20*time.Millisecond {
					waitDur = 20 * time.Millisecond
				}
				coord.mu.Unlock()
				select {
				case <-ctx.Done():
					return -1
				case <-time.After(waitDur):
				}
				continue
			}
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
			coord.consecutiveConnFail.Store(0)
			coord.adaptive.OnSuccess()
			coord.mu.Lock()
			if trk := coord.trackers[chunkIdx]; trk != nil {
				trk.retries = 0
				trk.backoffUntil = time.Time{}
			}
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
		if errors.Is(chunkErr, context.Canceled) || errors.Is(chunkErr, ErrVersionMismatch) || errors.Is(chunkErr, ErrRangeNotSupported) || IsFatalError(chunkErr) {
			return chunkErr
		}

		// 服务端限流（503/429 或并发超限引起的 403）：触发自适应并发收缩
		if IsRateLimitError(chunkErr) {
			newLimit := coord.adaptive.OnRateLimit()
			logger.Warn("chunk rate limited, scaling down concurrency",
				"chunk", chunkIdx, "newLimit", newLimit,
				"error", chunkErr.Error())

			coord.mu.Lock()
			trk := coord.trackers[chunkIdx]
			var currentRetries int
			if trk != nil {
				currentRetries = trk.retries
				trk.retries++
			}
			activeWorkers := coord.countActiveWorkersLocked()
			coord.mu.Unlock()

			if currentRetries >= maxChunkRetries {
				coord.mu.Lock()
				coord.lastErr = fmt.Errorf("chunk %d download failed after %d retries: %w", chunkIdx, currentRetries, chunkErr)
				coord.mu.Unlock()
				if activeWorkers <= 1 {
					return coord.lastErr
				}
				coord.mu.Lock()
				if trk != nil {
					trk.claimed = false
					trk.active = false
					trk.backoffUntil = time.Now().Add(retryBackoff(currentRetries))
				}
				coord.mu.Unlock()
				return nil
			}

			// 若当前并发允许数小于已激活 worker，且还有其他 worker 在运行，说明当前槽位已超标，主动释放分片让位退出
			if activeWorkers > newLimit {
				coord.mu.Lock()
				targetChunk := &coord.t.Chunks[chunkIdx]
				if !targetChunk.Completed && trk != nil {
					trk.claimed = false
					trk.active = false
					trk.backoffUntil = time.Now().Add(retryBackoff(currentRetries))
				}
				coord.mu.Unlock()
				return nil
			}

			delay := retryBackoff(currentRetries)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}

		// 瞬态网络故障、看门狗停摆（Stall）或连接被重置（RST/EOF）：
		// 1. 触发自适应并发故障回退（首次冻结窗口，冷却期内持续发生连接风暴则逐步下调并发上限）
		newLimit := coord.adaptive.OnRateLimit()

		coord.mu.Lock()
		trk := coord.trackers[chunkIdx]
		gained := coord.t.Chunks[chunkIdx].Downloaded - downloadedBefore
		if gained > 0 && trk != nil {
			trk.retries = 0
		}
		var retries int
		if trk != nil {
			retries = trk.retries
			trk.retries++
		}
		activeWorkers := coord.countActiveWorkersLocked()
		coord.mu.Unlock()

		// 2. 若本轮请求 0 字节推进且连续出现连接故障，检查是否为代理中途失效并尝试切换直连逃生
		if gained == 0 {
			if coord.consecutiveConnFail.Add(1) >= 2 {
				if coord.trySwitchToDirect(ctx) {
					continue
				}
			}
		} else {
			coord.consecutiveConnFail.Store(0)
		}

		logger.Warn("chunk request failed, isolating or retrying",
			"chunk", chunkIdx,
			"retries", retries,
			"activeWorkers", activeWorkers,
			"newLimit", newLimit,
			"stalled", errors.Is(chunkErr, stallwatch.ErrStalled),
			"ms", time.Since(requestStart).Milliseconds(),
			"error", logging.SafeError(chunkErr).Error())

		if retries >= maxChunkRetries {
			if coord.trySwitchToDirect(ctx) {
				continue
			}
			coord.mu.Lock()
			coord.lastErr = fmt.Errorf("chunk %d download failed after %d retries: %w", chunkIdx, retries, chunkErr)
			coord.mu.Unlock()
			if activeWorkers <= 1 {
				return coord.lastErr
			}
		}

		// 若还有其他活跃 worker 在下载（或当前活跃数已超出自适应缩容后的上限），
		// 释放当前分片退出协程：既实现单线故障隔离，又配合并发缩容让多余线程及时让位
		if activeWorkers > 1 || activeWorkers > newLimit {
			coord.mu.Lock()
			if trk != nil {
				trk.claimed = false
				trk.active = false
				trk.backoffUntil = time.Now().Add(retryBackoff(retries))
			}
			coord.mu.Unlock()
			return nil
		}

		// 唯一存活的 worker，在原地退避重试
		delay := retryBackoff(retries)
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

	coord.mu.Lock()
	targetURL := coord.t.EffectiveURL()
	etag := coord.t.ETag
	lastMod := coord.t.LastModified
	creds := coord.t.RequestHeaders
	client := coord.client
	coord.mu.Unlock()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("User-Agent", UserAgentChrome)
	req.Header.Set("Accept", "*/*")
	applyRequestHeaders(req, creds)
	req.Close = true

	if etag != "" {
		req.Header.Set("If-Match", etag)
	} else if lastMod != "" {
		req.Header.Set("If-Unmodified-Since", lastMod)
	}

	resp, err := client.Do(req)
	if err != nil {
		// *url.Error 会带上完整地址（含查询串里的令牌）；这里统一脱敏，日志与任务错误信息都不落明文。
		return watch.Err(ctx, logging.SafeError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	watch.Touch()

	if resp.Request != nil && resp.Request.URL != nil {
		if final := resp.Request.URL.String(); final != "" {
			coord.mu.Lock()
			if coord.t.FinalURL == "" {
				coord.t.FinalURL = final
			}
			coord.mu.Unlock()
		}
	}
	if resp.StatusCode == http.StatusPreconditionFailed {
		return ErrVersionMismatch
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if start >= end {
			return nil
		}
		return fmt.Errorf("range out of bounds: %d-%d", start, end)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		return &RateLimitError{Status: resp.Status}
	}
	if resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusOK {
			return fmt.Errorf("%w: server returned 200 OK instead of partial content", ErrRangeNotSupported)
		}
		// P0-3: 若使用的是 302 重定向后的临时地址（FinalURL != URL），当遭遇 401/403/410 等签名过期状态码时，
		// 先尝试回源原始 URL 刷新签名地址；若刷新成功，返回普通可重试错误以立即用新 FinalURL 续传。
		if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone) &&
			coord.tryRefreshExpiredFinalURL(ctx, targetURL) {
			return fmt.Errorf("redirected URL expired (%s), refreshed from origin", resp.Status)
		}
		// P0-2: 若已有其他分片成功拉流（证明链接与鉴权本身有效），而后续并发连接收到 403 Forbidden，
		// 这是典型的 CDN/防火墙按并发连接数拦截（如 Nginx limit_conn 返回 403），按并发限流降级处理而非判死整个任务。
		if resp.StatusCode == http.StatusForbidden && coord.hasSucceededOnce.Load() {
			coord.mu.Lock()
			active := coord.countActiveWorkersLocked()
			coord.mu.Unlock()
			if active > 1 {
				return &RateLimitError{Status: resp.Status}
			}
		}
		if isFatalStatusCode(resp.StatusCode) {
			return &FatalError{Err: fmt.Errorf("download failed: server returned status %s", resp.Status)}
		}
		return fmt.Errorf("download failed: server returned status %s", resp.Status)
	}

	if etag != "" && resp.Header.Get("ETag") != "" && resp.Header.Get("ETag") != etag {
		return ErrVersionMismatch
	}

	coord.hasSucceededOnce.Store(true)
	body := watch.Reader(resp.Body)
	buf := make([]byte, 64*1024)
	offset := start
	var streamEstablished bool
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := body.Read(buf)
		if n > 0 {
			if !streamEstablished {
				streamEstablished = true
				coord.adaptive.OnStreamEstablished()
			}

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

	coord.mu.Lock()
	lastErr := coord.lastErr
	coord.mu.Unlock()

	for _, c := range t.Chunks {
		if !c.Completed {
			if lastErr != nil {
				return lastErr
			}
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
