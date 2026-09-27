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

	t.Run("Extension update check available", func(t *testing.T) {
		res, err := checker.CheckExtensionUpdate(context.Background(), "1.1.0")
		if err != nil {
			t.Fatalf("CheckExtensionUpdate error: %v", err)
		}
		if !res.HasUpdate {
			t.Errorf("expected extension HasUpdate = true")
		}
		if res.LatestVersion != "1.2.0" {
			t.Errorf("expected LatestVersion = 1.2.0, got %s", res.LatestVersion)
		}
		if res.AssetName != "SheepGet_1.2.0_extension-chrome-mv3.zip" {
			t.Errorf("expected extension zip asset, got %s", res.AssetName)
		}
	})

	t.Run("Extension update check not available when asset version equals local version", func(t *testing.T) {
		res, err := checker.CheckExtensionUpdate(context.Background(), "1.2.0")
		if err != nil {
			t.Fatalf("CheckExtensionUpdate error: %v", err)
		}
		if res.HasUpdate {
			t.Errorf("expected extension HasUpdate = false when version is equal")
		}
	})

	t.Run("Extension update check when no extension asset present", func(t *testing.T) {
		noExtRelease := GitHubRelease{
			TagName: "v2.0.0",
			Assets:  []GitHubAsset{{Name: "SheepGet_2.0.0_x64-setup.exe"}},
		}
		noExtServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(noExtRelease)
		}))
		defer noExtServer.Close()

		noExtChecker := NewChecker(WithBaseURL(noExtServer.URL))
		res, err := noExtChecker.CheckExtensionUpdate(context.Background(), "1.0.0")
		if err != nil {
			t.Fatalf("CheckExtensionUpdate error: %v", err)
		}
		if res.HasUpdate {
			t.Errorf("expected HasUpdate = false when no extension asset is available")
		}
	})

	t.Run("Interleaved releases: ext-v* release newest does not crash desktop app check", func(t *testing.T) {
		interleavedReleases := []GitHubRelease{
			{
				TagName: "ext-v1.0.2",
				Name:    "Extension 1.0.2",
				Assets: []GitHubAsset{
					{Name: "SheepGet_1.0.2_extension-chrome-mv3.zip", BrowserDownloadURL: "https://dl/ext-1.0.2.zip"},
				},
			},
			{
				TagName: "v1.0.1",
				Name:    "SheepGet 1.0.1",
				Assets: []GitHubAsset{
					{Name: "SheepGet_1.0.1_windows-x64-portable.zip", BrowserDownloadURL: "https://dl/win-port-1.0.1.zip"},
					{Name: "SheepGet_1.0.0_extension-chrome-mv3.zip", BrowserDownloadURL: "https://dl/ext-1.0.0.zip"},
				},
			},
		}

		interleavedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(interleavedReleases)
		}))
		defer interleavedServer.Close()

		c := NewChecker(WithBaseURL(interleavedServer.URL))

		// 1. App check should skip ext-v1.0.2 and match v1.0.1
		appRes, err := c.CheckAppUpdate(context.Background(), "1.0.0", "windows", "amd64", true)
		if err != nil {
			t.Fatalf("CheckAppUpdate unexpectedly failed: %v", err)
		}
		if !appRes.HasUpdate {
			t.Errorf("expected App HasUpdate = true")
		}
		if appRes.LatestVersion != "1.0.1" {
			t.Errorf("expected App LatestVersion = 1.0.1, got %s", appRes.LatestVersion)
		}

		// 2. Extension check should match ext-v1.0.2 asset
		extRes, err := c.CheckExtensionUpdate(context.Background(), "1.0.0")
		if err != nil {
			t.Fatalf("CheckExtensionUpdate unexpectedly failed: %v", err)
		}
		if !extRes.HasUpdate {
			t.Errorf("expected Ext HasUpdate = true")
		}
		if extRes.LatestVersion != "1.0.2" {
			t.Errorf("expected Ext LatestVersion = 1.0.2, got %s", extRes.LatestVersion)
		}
	})
}
