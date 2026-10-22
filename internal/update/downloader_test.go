package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloader_MultiProxyFallback(t *testing.T) {
	// Target download server
	fileContent := []byte("sheepget update test payload binary data")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "40")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContent)
	}))
	defer ts.Close()

	deadProxy := "http://127.0.0.1:54321"
	dl := NewDownloader(nil)
	dl.SetProxyChain(deadProxy, "direct")

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "pkg.zip")

	var progressReports []DownloadProgress
	err := dl.Download(context.Background(), ts.URL+"/dl/pkg.zip", targetPath, func(p DownloadProgress) {
		progressReports = append(progressReports, p)
	})
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	downloaded, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(downloaded) != string(fileContent) {
		t.Errorf("content mismatch, got %s", string(downloaded))
	}
}
