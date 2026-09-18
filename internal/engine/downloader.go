// Package engine implements the HTTP chunked download engine and task queue manager.
package engine

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
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
	"sheep-get/internal/task"
)

const (
	MinChunkSize    = 256 * 1024 // 256KB
	UserAgentChrome = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
)

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

// ProgressFunc reports updated downloaded bytes.
type ProgressFunc func(downloaded int64, chunkIndex int, chunkDownloaded int64)

// Download executes download for a task, handling single-connection or multi-connection range download.
func (d *HTTPDownloader) Download(ctx context.Context, t *task.Task, onProgress ProgressFunc) error {
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
		return d.downloadSingleStream(ctx, t, partPath, destPath, onProgress, client)
	}

	return d.downloadChunks(ctx, t, partPath, destPath, onProgress, client)
}

func (d *HTTPDownloader) downloadSingleStream(ctx context.Context, t *task.Task, partPath, destPath string, onProgress ProgressFunc, client *http.Client) error {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
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

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned error status: %s", resp.Status)
	}

	buf := make([]byte, 64*1024)
	var downloaded int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := file.Write(buf[:n]); wErr != nil {
				return wErr
			}
			downloaded += int64(n)
			if onProgress != nil {
				onProgress(downloaded, 0, downloaded)
			}
		}
		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				break
			}
			return rErr
		}
	}

	_ = file.Close()
	if t.LastModified == "" && resp.Header.Get("Last-Modified") != "" {
		t.LastModified = resp.Header.Get("Last-Modified")
	}
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
	progressMu sync.Mutex
	t          *task.Task
	file       *os.File
	fileMu     sync.Mutex
	totalDown  int64
	trackers   map[int]*chunkTracker
	onProgress ProgressFunc
	downloader *HTTPDownloader
	client     *http.Client
}

func newChunkCoordinator(t *task.Task, file *os.File, downloader *HTTPDownloader, client *http.Client, onProgress ProgressFunc) *chunkCoordinator {
	coord := &chunkCoordinator{
		t:          t,
		file:       file,
		trackers:   make(map[int]*chunkTracker),
		onProgress: onProgress,
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
		splitIdx := coord.trySplitSlowChunkLocked()
		if splitIdx != -1 {
			coord.mu.Unlock()
			return splitIdx
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

func (coord *chunkCoordinator) trySplitSlowChunkLocked() int {
	hasCompletedChunk := false
	for _, c := range coord.t.Chunks {
		if c.Completed {
			hasCompletedChunk = true
			break
		}
	}

	now := time.Now()
	var bestIdx = -1
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
			return -1
		}
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

		// Must have at least 2 * MinChunkSize remaining to justify splitting
		if remaining < MinChunkSize*2 {
			continue
		}

		if remaining > maxRemaining {
			maxRemaining = remaining
			bestIdx = i
		}
	}

	if bestIdx == -1 {
		return -1
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

	return newIdx
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
		if chunkStart > chunkEnd {
			c.Completed = true
			coord.mu.Unlock()
			return nil
		}
		coord.mu.Unlock()

		chunkErr := coord.downloadChunkStream(ctx, chunkIdx, chunkStart, chunkEnd)
		if chunkErr == nil {
			coord.mu.Lock()
			c := &coord.t.Chunks[chunkIdx]
			if c.Downloaded >= (c.End - c.Start + 1) {
				c.Completed = true
			}
			coord.mu.Unlock()
			return nil
		}

		// Fatal errors abort immediately
		if errors.Is(chunkErr, context.Canceled) || errors.Is(chunkErr, ErrVersionMismatch) || errors.Is(chunkErr, ErrRangeNotSupported) {
			return chunkErr
		}

		// Transient network drop or 429 rate limit: back off and retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (coord *chunkCoordinator) downloadChunkStream(ctx context.Context, chunkIdx int, start, end int64) error {
	if start > end {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coord.t.URL, nil)
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
		return err
	}
	defer func() { _ = resp.Body.Close() }()

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

	buf := make([]byte, 64*1024)
	offset := start
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := resp.Body.Read(buf)
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

			if coord.onProgress != nil {
				coord.progressMu.Lock()
				coord.onProgress(currTotal, chunkIdx, chunk.Downloaded)
				coord.progressMu.Unlock()
			}

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
			return rErr
		}
	}

	return nil
}

func (d *HTTPDownloader) downloadChunks(ctx context.Context, t *task.Task, partPath, destPath string, onProgress ProgressFunc, client *http.Client) error {
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

	coord := newChunkCoordinator(t, file, d, client, onProgress)

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
	return d.commitCompletedFile(partPath, destPath, t)
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
