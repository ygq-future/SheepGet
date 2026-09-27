package update

import "testing"

func TestMatchAssets(t *testing.T) {
	assets := []GitHubAsset{
		{Name: "SheepGet_1.1.0_windows-x64-portable.zip", BrowserDownloadURL: "https://dl/win-port.zip"},
		{Name: "SheepGet_1.1.0_x64-setup.exe", BrowserDownloadURL: "https://dl/win-setup.exe"},
		{Name: "SheepGet_1.1.0_x64_en-US.msi", BrowserDownloadURL: "https://dl/win.msi"},
		{Name: "SheepGet_1.1.0_extension-chrome-mv3.zip", BrowserDownloadURL: "https://dl/ext.zip"},
		{Name: "SheepGet_1.1.0_darwin-arm64-portable.zip", BrowserDownloadURL: "https://dl/mac-port.zip"},
		{Name: "SheepGet_1.1.0_arm64.dmg", BrowserDownloadURL: "https://dl/mac.dmg"},
		{Name: "SheepGet_1.1.0_linux-x64-portable.zip", BrowserDownloadURL: "https://dl/linux-port.zip"},
		{Name: "SheepGet_1.1.0_amd64.AppImage", BrowserDownloadURL: "https://dl/linux.AppImage"},
		{Name: "SheepGet_1.1.0_amd64.deb", BrowserDownloadURL: "https://dl/linux.deb"},
		{Name: "SHA256SUMS.txt", BrowserDownloadURL: "https://dl/sha.txt"},
	}

	t.Run("Windows Portable", func(t *testing.T) {
		matched := MatchAppAsset(assets, "windows", "amd64", true)
		if matched == nil || matched.Name != "SheepGet_1.1.0_windows-x64-portable.zip" {
			t.Fatalf("expected windows portable zip, got: %v", matched)
		}
	})

	t.Run("Windows Setup Installer", func(t *testing.T) {
		matched := MatchAppAsset(assets, "windows", "amd64", false)
		if matched == nil || matched.Name != "SheepGet_1.1.0_x64-setup.exe" {
			t.Fatalf("expected windows setup exe, got: %v", matched)
		}
	})

	t.Run("macOS Portable", func(t *testing.T) {
		matched := MatchAppAsset(assets, "darwin", "arm64", true)
		if matched == nil || matched.Name != "SheepGet_1.1.0_darwin-arm64-portable.zip" {
			t.Fatalf("expected mac portable zip, got: %v", matched)
		}
	})

	t.Run("macOS DMG", func(t *testing.T) {
		matched := MatchAppAsset(assets, "darwin", "arm64", false)
		if matched == nil || matched.Name != "SheepGet_1.1.0_arm64.dmg" {
			t.Fatalf("expected mac dmg, got: %v", matched)
		}
	})

	t.Run("Linux Portable", func(t *testing.T) {
		matched := MatchAppAsset(assets, "linux", "amd64", true)
		if matched == nil || matched.Name != "SheepGet_1.1.0_linux-x64-portable.zip" {
			t.Fatalf("expected linux portable zip, got: %v", matched)
		}
	})

	t.Run("Linux AppImage", func(t *testing.T) {
		matched := MatchAppAsset(assets, "linux", "amd64", false)
		if matched == nil || matched.Name != "SheepGet_1.1.0_amd64.AppImage" {
			t.Fatalf("expected linux appimage, got: %v", matched)
		}
	})

	t.Run("Browser Extension", func(t *testing.T) {
		matched := MatchExtensionAsset(assets)
		if matched == nil || matched.Name != "SheepGet_1.1.0_extension-chrome-mv3.zip" {
			t.Fatalf("expected extension zip, got: %v", matched)
		}
	})
}

func TestExtractExtensionVersion(t *testing.T) {
	cases := []struct {
		filename string
		expected string
	}{
		{"SheepGet_1.0.1_extension-chrome-mv3.zip", "1.0.1"},
		{"SheepGet_extension_1.0.2.zip", "1.0.2"},
		{"SheepGet_extension_v2.0.0.zip", "2.0.0"},
		{"extension-chrome-mv3-1.0.0.zip", "1.0.0"},
		{"no-version-extension.zip", ""},
	}

	for _, c := range cases {
		actual := ExtractExtensionVersion(c.filename)
		if actual != c.expected {
			t.Errorf("ExtractExtensionVersion(%q) = %q, expected %q", c.filename, actual, c.expected)
		}
	}
}
