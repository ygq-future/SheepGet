package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ProgressFunc receives real-time progress updates during download.
type ProgressFunc func(progress DownloadProgress)

// Downloader handles downloading update packages.
type Downloader struct {
	httpClient *http.Client
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

// Download downloads a file from downloadURL to targetPath, invoking progressFn periodically.
func (d *Downloader) Download(ctx context.Context, downloadURL, targetPath string, progressFn ProgressFunc) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("User-Agent", "SheepGet-AutoUpdater")

	resp, err := d.httpClient.Do(req)
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
	buf := make([]byte, 64*1024)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("failed to write to file: %w", writeErr)
			}
			downloaded += int64(n)

			now := time.Now()
			elapsed := now.Sub(lastTime)
			if progressFn != nil && (elapsed >= 200*time.Millisecond || readErr != nil) {
				var speed int64
				if elapsed.Seconds() > 0 {
					speed = int64(float64(downloaded-lastBytes) / elapsed.Seconds())
				}
				percent := float64(0)
				if totalSize > 0 {
					percent = float64(downloaded) / float64(totalSize) * 100
				}
				progressFn(DownloadProgress{
					DownloadedBytes: downloaded,
					TotalBytes:      totalSize,
					Percentage:      percent,
					SpeedBps:        speed,
				})
				lastTime = now
				lastBytes = downloaded
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("error reading response body: %w", readErr)
		}
	}

	return nil
}
