package update

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sheep-get/internal/storage"
)

func TestService_EndToEndFlow(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Prepare mock extension zip
	extZipPath := filepath.Join(tempDir, "mock_ext.zip")
	createMockZip(t, extZipPath, map[string]string{
		"manifest.json": `{"version":"1.5.0"}`,
		"background.js": `console.log("v1.5.0");`,
	})

	// 2. Mock GitHub Releases endpoint
	mockRelease := GitHubRelease{
		TagName:     "v1.5.0",
		Name:        "Release 1.5.0",
		Body:        "New features",
		PublishedAt: time.Now(),
		Assets: []GitHubAsset{
			{Name: "SheepGet_1.5.0_windows-x64-portable.zip", BrowserDownloadURL: "MOCK_PORTABLE_URL", Size: 100},
			{Name: "SheepGet_1.5.0_extension-chrome-mv3.zip", BrowserDownloadURL: "MOCK_EXT_URL", Size: 50},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockRelease)
		case "/download/ext.zip":
			http.ServeFile(w, r, extZipPath)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Adjust release URLs to point to mock server
	mockRelease.Assets[1].BrowserDownloadURL = ts.URL + "/download/ext.zip"

	st := &storage.Storage{
		Mode:    storage.ModePortable,
		DataDir: filepath.Join(tempDir, "data"),
	}

	checker := NewChecker(WithBaseURL(ts.URL + "/releases/latest"))
	svc := NewService(st, checker)

	t.Run("CheckAppUpdate", func(t *testing.T) {
		res, err := svc.CheckAppUpdate(context.Background())
		if err != nil {
			t.Fatalf("CheckAppUpdate failed: %v", err)
		}
		if !res.HasUpdate {
			t.Fatalf("expected HasUpdate to be true")
		}
		if res.LatestVersion != "1.5.0" {
			t.Fatalf("expected latest version 1.5.0, got %s", res.LatestVersion)
		}
	})

	t.Run("CheckExtensionUpdate", func(t *testing.T) {
		res, err := svc.CheckExtensionUpdate(context.Background())
		if err != nil {
			t.Fatalf("CheckExtensionUpdate failed: %v", err)
		}
		if !res.HasUpdate {
			t.Fatalf("expected HasUpdate to be true")
		}
		if res.LatestVersion != "1.5.0" {
			t.Fatalf("expected latest version 1.5.0, got %s", res.LatestVersion)
		}
	})

	t.Run("ConfigureProxy", func(t *testing.T) {
		if err := svc.SetProxy("direct", ""); err != nil {
			t.Fatalf("SetProxy direct failed: %v", err)
		}
		if err := svc.SetProxy("custom", "http://127.0.0.1:8888"); err != nil {
			t.Fatalf("SetProxy custom failed: %v", err)
		}
	})
}

func createMockZip(t *testing.T, targetPath string, files map[string]string) {
	t.Helper()
	f, err := os.Create(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	defer func() { _ = zw.Close() }()

	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
}
