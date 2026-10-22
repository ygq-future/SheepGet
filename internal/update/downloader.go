package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProgressFunc receives real-time progress updates during download.
type ProgressFunc func(progress DownloadProgress)

// Downloader handles downloading update packages with multi-proxy fallback resilience.
type Downloader struct {
	httpClient *http.Client
	proxyChain []string
	activeNode string
}

// NewDownloader creates a new Downloader instance.
func NewDownloader(client *http.Client) *Downloader {
	if client == nil {
		client = &http.Client{
			Timeout: 0, // No timeout for large file downloads; controlled by Context
		}
	}
	return &Downloader{
		httpClient: client,
	}
}

// SetProxyChain updates the candidate proxy chain for downloads.
func (d *Downloader) SetProxyChain(chain ...string) {
	d.proxyChain = append([]string(nil), chain...)
}

// SetActiveNode sets the preferred proxy node (e.g. from a successful update check).
func (d *Downloader) SetActiveNode(node string) {
	d.activeNode = node
}

func (d *Downloader) clientForNode(node string) (*http.Client, error) {
	var transport *http.Transport
	var err error

	switch strings.ToLower(node) {
	case "direct", "":
		transport, err = createTransport("direct", "")
	case "system":
		transport, err = createTransport("system", "")
	default:
		transport, err = createTransport("custom", node)
	}

	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: transport,
		Timeout:   0,
	}, nil
}

// Download downloads a file from downloadURL to targetPath, invoking progressFn periodically.
// If a candidate proxy chain is available, it prioritizes the activeNode and falls back along the chain.
func (d *Downloader) Download(ctx context.Context, downloadURL, targetPath string, progressFn ProgressFunc) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Determine ordered list of client attempts
	var candidates []string
	if d.activeNode != "" {
		candidates = append(candidates, d.activeNode)
	}
	for _, node := range d.proxyChain {
		if !sliceContains(candidates, node) {
			candidates = append(candidates, node)
		}
	}

	// Fallback to single client if no proxyChain is configured
	if len(candidates) == 0 {
		return d.downloadWithClient(ctx, d.httpClient, downloadURL, targetPath, progressFn)
	}

	var errors []string
	for _, node := range candidates {
		client, err := d.clientForNode(node)
		if err != nil {
			errors = append(errors, fmt.Sprintf("[%s] client error: %v", node, err))
			continue
		}

		err = d.downloadWithClient(ctx, client, downloadURL, targetPath, progressFn)
		if err == nil {
			d.activeNode = node
			return nil
		}

		errors = append(errors, fmt.Sprintf("[%s] %v", node, err))
	}

	return fmt.Errorf("download failed across all proxy candidates: %s", strings.Join(errors, "; "))
}

func (d *Downloader) downloadWithClient(ctx context.Context, client *http.Client, downloadURL, targetPath string, progressFn ProgressFunc) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("User-Agent", "SheepGet-AutoUpdater")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download server returned %d %s", resp.StatusCode, resp.Status)
	}

	totalSize := resp.ContentLength
	out, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}
	defer func() { _ = out.Close() }()

	var downloaded int64
	var lastBytes int64
	lastTime := time.Now()

	buf := make([]byte, 64*1024) // 64KB buffer
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				return fmt.Errorf("failed writing downloaded data: %w", wErr)
			}
			downloaded += int64(n)

			now := time.Now()
			elapsed := now.Sub(lastTime)
			if elapsed >= 250*time.Millisecond || rErr == io.EOF {
				var speedBps int64
				if elapsed.Seconds() > 0 {
					speedBps = int64(float64(downloaded-lastBytes) / elapsed.Seconds())
				}
				lastBytes = downloaded
				lastTime = now

				if progressFn != nil {
					var percent float64
					if totalSize > 0 {
						percent = float64(downloaded) / float64(totalSize) * 100.0
						if percent > 100.0 {
							percent = 100.0
						}
					}
					progressFn(DownloadProgress{
						DownloadedBytes: downloaded,
						TotalBytes:      totalSize,
						Percentage:      percent,
						SpeedBps:        speedBps,
					})
				}
			}
		}

		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			return fmt.Errorf("error reading download stream: %w", rErr)
		}
	}

	return nil
}
