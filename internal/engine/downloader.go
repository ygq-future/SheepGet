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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sheep-get/internal/task"
)

const (
	DefaultMaxConcurrency = 4
	MinChunkSize          = 256 * 1024 // 256KB
	UserAgentChrome       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
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
	client *http.Client
}

// NewHTTPDownloader creates a new HTTPDownloader with robust Transport settings.
func NewHTTPDownloader(client *http.Client) *HTTPDownloader {
	if client == nil {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
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
		}
		client = &http.Client{
			Transport: transport,
			Timeout:   0,
		}
	}
	return &HTTPDownloader{client: client}
}

// Probe inspects URL metadata without downloading the body. headers carries the request
// context of the task (Referer/Cookie/Authorization) so expired links can be re-checked.
func (d *HTTPDownloader) Probe(ctx context.Context, urlStr string, headers map[string]string) (*HTTPProbeInfo, error) {
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
		applyRequestHeaders(req, headers)
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
			applyRequestHeaders(headReq, headers)
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

// applyRequestHeaders copies task request context onto an outgoing request. Range, User-Agent
// and Accept are set by the caller; any header the task carries wins on collision so an updated
// link can override them.
func applyRequestHeaders(req *http.Request, headers map[string]string) {
	for name, value := range headers {
		req.Header.Set(name, value)
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
	parts := strings.Split(urlStr, "?")
	clean := parts[0]
	base := filepath.Base(clean)
	if base == "" || base == "/" || base == "." {
		return "download.bin"
	}
	return base
}

// ProgressFunc reports updated downloaded bytes.
type ProgressFunc func(downloaded int64, chunkIndex int, chunkDownloaded int64)

// Download executes download for a task, handling single-connection or multi-connection range download.
func (d *HTTPDownloader) Download(ctx context.Context, t *task.Task, onProgress ProgressFunc) error {
	destPath := filepath.Join(t.Directory, t.Filename)
	partPath := destPath + ".sheepget"

	if err := os.MkdirAll(t.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// If the resource is not resumable or total size is unknown, fallback to single-stream download
	if !t.Resumable || t.TotalBytes <= 0 {
		return d.downloadSingleStream(ctx, t, partPath, destPath, onProgress)
	}

	return d.downloadChunks(ctx, t, partPath, destPath, onProgress)
}

func (d *HTTPDownloader) downloadSingleStream(ctx context.Context, t *task.Task, partPath, destPath string, onProgress ProgressFunc) error {
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

	resp, err := d.client.Do(req)
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
	return os.Rename(partPath, destPath)
}

func (d *HTTPDownloader) downloadChunks(ctx context.Context, t *task.Task, partPath, destPath string, onProgress ProgressFunc) error {
	// Initialize or load chunks
	if len(t.Chunks) == 0 {
		t.Chunks = splitChunks(t.TotalBytes, t.MaxConcurrency)
	}

	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open part file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Ensure file size matches TotalBytes
	if err := file.Truncate(t.TotalBytes); err != nil {
		return fmt.Errorf("failed to truncate part file: %w", err)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(t.Chunks))

	totalDownloaded := int64(0)
	for _, c := range t.Chunks {
		totalDownloaded += c.Downloaded
	}

	var mu sync.Mutex // protects writing to file at offset and progress reporting

	// Start all chunk workers simultaneously so all channels download in parallel
	for chunkIdx := range t.Chunks {
		chunk := &t.Chunks[chunkIdx]
		if chunk.Completed {
			continue
		}

		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := &t.Chunks[idx]

			// Persistent retry loop for each dedicated channel
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				chunkStart := c.Start + c.Downloaded
				if chunkStart > c.End {
					c.Completed = true
					return
				}

				chunkErr := d.downloadChunk(ctx, t, file, &mu, idx, chunkStart, c.End, &totalDownloaded, onProgress)
				if chunkErr == nil {
					if c.Downloaded >= (c.End - c.Start + 1) {
						c.Completed = true
					}
					return
				}

				// Abort immediately on user cancel, file version mismatch, or Range not supported
				if errors.Is(chunkErr, context.Canceled) || errors.Is(chunkErr, ErrVersionMismatch) || errors.Is(chunkErr, ErrRangeNotSupported) {
					select {
					case errChan <- chunkErr:
					default:
					}
					return
				}

				// If 429 rate limited or transient network drop, wait briefly and retry this channel until completed
				select {
				case <-ctx.Done():
					return
				case <-time.After(300 * time.Millisecond):
				}
			}
		}(chunkIdx)
	}

	wg.Wait()
	close(errChan)

	if err, ok := <-errChan; ok && err != nil {
		return err
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Verify all chunks completed
	for _, c := range t.Chunks {
		if !c.Completed {
			return errors.New("not all chunks completed")
		}
	}

	_ = file.Close()
	return os.Rename(partPath, destPath)
}

func (d *HTTPDownloader) downloadChunk(
	ctx context.Context,
	t *task.Task,
	file *os.File,
	fileMu *sync.Mutex,
	chunkIdx int,
	start, end int64,
	totalDownloaded *int64,
	onProgress ProgressFunc,
) error {
	if start > end {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("User-Agent", UserAgentChrome)
	req.Header.Set("Accept", "*/*")
	applyRequestHeaders(req, t.RequestHeaders)
	req.Close = true // Clean single connection per stream

	// Version consistency validation headers
	if t.ETag != "" {
		req.Header.Set("If-Match", t.ETag)
	} else if t.LastModified != "" {
		req.Header.Set("If-Unmodified-Since", t.LastModified)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return ErrVersionMismatch
	}

	// 416 Requested Range Not Satisfiable: chunk is already at the end
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

	// Check if ETag or LastModified changed on 206 response
	if t.ETag != "" && resp.Header.Get("ETag") != "" && resp.Header.Get("ETag") != t.ETag {
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
			fileMu.Lock()
			_, wErr := file.WriteAt(buf[:n], offset)
			if wErr != nil {
				fileMu.Unlock()
				return wErr
			}
			offset += int64(n)
			t.Chunks[chunkIdx].Downloaded += int64(n)
			atomic.AddInt64(totalDownloaded, int64(n))
			currTotal := atomic.LoadInt64(totalDownloaded)
			fileMu.Unlock()

			if onProgress != nil {
				onProgress(currTotal, chunkIdx, t.Chunks[chunkIdx].Downloaded)
			}
		}

		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				break
			}
			return rErr
		}
	}

	return nil
}

func splitChunks(totalBytes int64, concurrency int) []task.Chunk {
	if concurrency <= 0 {
		concurrency = DefaultMaxConcurrency
	}

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
