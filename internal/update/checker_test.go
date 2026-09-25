package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChecker_CheckAppUpdate(t *testing.T) {
	mockRelease := GitHubRelease{
		TagName:     "v1.2.0",
		Name:        "SheepGet 1.2.0 Release",
		Body:        "- Fixed bugs\n- Added auto update",
		PublishedAt: time.Now(),
		HTMLURL:     "https://github.com/ygq-future/SheepGet/releases/tag/v1.2.0",
		Assets: []GitHubAsset{
			{Name: "SheepGet_1.2.0_windows-x64-portable.zip", BrowserDownloadURL: "https://dl/win-port.zip", Size: 12345},
			{Name: "SheepGet_1.2.0_x64-setup.exe", BrowserDownloadURL: "https://dl/win-setup.exe", Size: 23456},
			{Name: "SheepGet_1.2.0_extension-chrome-mv3.zip", BrowserDownloadURL: "https://dl/ext.zip", Size: 3456},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockRelease)
	}))
	defer ts.Close()

	checker := NewChecker(WithBaseURL(ts.URL))

	t.Run("App update available for portable", func(t *testing.T) {
		res, err := checker.CheckAppUpdate(context.Background(), "1.0.0", "windows", "amd64", true)
		if err != nil {
			t.Fatalf("CheckAppUpdate error: %v", err)
		}
		if !res.HasUpdate {
			t.Errorf("expected HasUpdate = true")
		}
		if res.LatestVersion != "1.2.0" {
			t.Errorf("expected LatestVersion = 1.2.0, got %s", res.LatestVersion)
		}
		if res.AssetName != "SheepGet_1.2.0_windows-x64-portable.zip" {
			t.Errorf("expected portable zip asset, got %s", res.AssetName)
		}
	})

	t.Run("App update available for setup", func(t *testing.T) {
		res, err := checker.CheckAppUpdate(context.Background(), "1.0.0", "windows", "amd64", false)
		if err != nil {
			t.Fatalf("CheckAppUpdate error: %v", err)
		}
		if !res.HasUpdate {
			t.Errorf("expected HasUpdate = true")
		}
		if res.AssetName != "SheepGet_1.2.0_x64-setup.exe" {
			t.Errorf("expected setup exe asset, got %s", res.AssetName)
		}
	})

	t.Run("App update not available when up to date", func(t *testing.T) {
		res, err := checker.CheckAppUpdate(context.Background(), "1.2.0", "windows", "amd64", true)
		if err != nil {
			t.Fatalf("CheckAppUpdate error: %v", err)
		}
		if res.HasUpdate {
			t.Errorf("expected HasUpdate = false for identical version")
		}
	})

	t.Run("Extension update check", func(t *testing.T) {
		res, err := checker.CheckExtensionUpdate(context.Background(), "1.1.0")
		if err != nil {
			t.Fatalf("CheckExtensionUpdate error: %v", err)
		}
		if !res.HasUpdate {
			t.Errorf("expected extension HasUpdate = true")
		}
		if res.AssetName != "SheepGet_1.2.0_extension-chrome-mv3.zip" {
			t.Errorf("expected extension zip asset, got %s", res.AssetName)
		}
	})
}
