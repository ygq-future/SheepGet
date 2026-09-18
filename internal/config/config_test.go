package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings("/downloads", "/temp")
	if s.Appearance.Theme != ThemeSystem {
		t.Errorf("expected ThemeSystem, got %s", s.Appearance.Theme)
	}
	if s.Appearance.AccentColor != DefaultAccentColor {
		t.Errorf("expected DefaultAccentColor %s, got %s", DefaultAccentColor, s.Appearance.AccentColor)
	}
	if s.Download.DuplicateURLPolicy != DuplicatePolicyAsk {
		t.Errorf("expected DuplicatePolicyAsk, got %s", s.Download.DuplicateURLPolicy)
	}
	if s.Download.PreDownload != false {
		t.Errorf("expected PreDownload false, got %v", s.Download.PreDownload)
	}
	if s.Download.MaxConcurrentDownloads != 3 {
		t.Errorf("expected MaxConcurrentDownloads 3, got %d", s.Download.MaxConcurrentDownloads)
	}
	if s.Download.DefaultConnectionsPerTask != 8 {
		t.Errorf("expected DefaultConnectionsPerTask 8, got %d", s.Download.DefaultConnectionsPerTask)
	}
	if s.Download.DefaultDirectory != "/downloads" {
		t.Errorf("expected /downloads, got %s", s.Download.DefaultDirectory)
	}
	if !s.Download.ShowProgressWindow {
		t.Errorf("expected ShowProgressWindow true by default")
	}
	if !s.Download.KeepCompletedInfo {
		t.Errorf("expected KeepCompletedInfo true by default")
	}
	if s.Download.AutoRemoveCompletedOnOpen {
		t.Errorf("expected AutoRemoveCompletedOnOpen false by default")
	}
	if s.General.LaunchAtStartup != false {
		t.Errorf("expected LaunchAtStartup false by default")
	}
	if s.Proxy.Mode != ProxyModeSystem {
		t.Errorf("expected ProxyModeSystem, got %s", s.Proxy.Mode)
	}
	if s.Proxy.CustomAddr != "" {
		t.Errorf("expected empty CustomAddr, got %s", s.Proxy.CustomAddr)
	}
	if len(s.Takeover.Extensions) == 0 {
		t.Errorf("expected non-empty default takeover extensions")
	}
	if s.Takeover.PauseShortcut != "Delete" {
		t.Errorf("expected default pause shortcut Delete, got %s", s.Takeover.PauseShortcut)
	}
	if s.Takeover.ForceShortcut != "Insert" {
		t.Errorf("expected default force shortcut Insert, got %s", s.Takeover.ForceShortcut)
	}
	if s.Clipboard.Enabled != false {
		t.Errorf("expected Clipboard.Enabled false by default")
	}
}

func TestDownloadConfig_WindowSettingsJSONUnmarshal(t *testing.T) {
	// Case 1: missing fields in JSON -> defaults true, true, false
	rawMissing := `{"defaultDirectory": "/test"}`
	var c1 DownloadConfig
	if err := json.Unmarshal([]byte(rawMissing), &c1); err != nil {
		t.Fatalf("failed to unmarshal c1: %v", err)
	}
	if !c1.ShowProgressWindow {
		t.Errorf("expected ShowProgressWindow default to true when missing")
	}
	if !c1.KeepCompletedInfo {
		t.Errorf("expected KeepCompletedInfo default to true when missing")
	}
	if c1.AutoRemoveCompletedOnOpen {
		t.Errorf("expected AutoRemoveCompletedOnOpen default to false when missing")
	}

	// Case 2: explicitly set to false, false, true
	rawExplicit := `{"showProgressWindow": false, "keepCompletedInfo": false, "autoRemoveCompletedOnOpen": true}`
	var c2 DownloadConfig
	if err := json.Unmarshal([]byte(rawExplicit), &c2); err != nil {
		t.Fatalf("failed to unmarshal c2: %v", err)
	}
	if c2.ShowProgressWindow {
		t.Errorf("expected ShowProgressWindow false")
	}
	if c2.KeepCompletedInfo {
		t.Errorf("expected KeepCompletedInfo false")
	}
	if !c2.AutoRemoveCompletedOnOpen {
		t.Errorf("expected AutoRemoveCompletedOnOpen true")
	}
}

func TestSettingsValidation_Fallback(t *testing.T) {
	s := Settings{
		Appearance: AppearanceConfig{
			Theme:       "invalid_theme",
			AccentColor: "not_a_color",
		},
		Download: DownloadConfig{
			DuplicateURLPolicy:        "invalid_policy",
			MaxConcurrentDownloads:    -5,
			DefaultConnectionsPerTask: 100, // too big
			DefaultDirectory:          "",
		},
	}

	validated := s.ValidateAndFallback("/fallback/dir", "/fallback/temp")
	if validated.Appearance.Theme != ThemeSystem {
		t.Errorf("expected fallback to ThemeSystem, got %s", validated.Appearance.Theme)
	}
	if validated.Appearance.AccentColor != DefaultAccentColor {
		t.Errorf("expected fallback to DefaultAccentColor, got %s", validated.Appearance.AccentColor)
	}
	if validated.Download.DuplicateURLPolicy != DuplicatePolicyAsk {
		t.Errorf("expected fallback to DuplicatePolicyAsk, got %s", validated.Download.DuplicateURLPolicy)
	}
	if validated.Download.MaxConcurrentDownloads != 3 {
		t.Errorf("expected fallback to 3, got %d", validated.Download.MaxConcurrentDownloads)
	}
	if validated.Download.DefaultConnectionsPerTask != 8 {
		t.Errorf("expected fallback to 8, got %d", validated.Download.DefaultConnectionsPerTask)
	}
	if validated.Download.DefaultDirectory != "/fallback/dir" {
		t.Errorf("expected fallback dir, got %s", validated.Download.DefaultDirectory)
	}
	if validated.Proxy.Mode != ProxyModeSystem {
		t.Errorf("expected fallback to ProxyModeSystem, got %s", validated.Proxy.Mode)
	}
	if len(validated.Takeover.Extensions) == 0 {
		t.Errorf("expected fallback to default takeover extensions")
	}
	if validated.Takeover.PauseShortcut != "Delete" {
		t.Errorf("expected fallback to Delete, got %s", validated.Takeover.PauseShortcut)
	}
	if validated.Takeover.ForceShortcut != "Insert" {
		t.Errorf("expected fallback to Insert, got %s", validated.Takeover.ForceShortcut)
	}
}

func TestSettingsService_AtomicPersistenceAndBroadcast(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.json")

	var notified *Settings
	service := NewSettingsService(configFile, "/default/downloads", "/default/temp", func(updated *Settings) error {
		notified = updated
		return nil
	})

	// Initial load without file should give default settings
	initial := service.Get()
	if initial.Download.DefaultDirectory != "/default/downloads" {
		t.Fatalf("expected initial default directory, got %s", initial.Download.DefaultDirectory)
	}

	// Update settings
	updateReq := initial
	updateReq.Appearance.Theme = ThemeDark
	updateReq.Appearance.AccentColor = "#3b82f6"
	updateReq.Download.MaxConcurrentDownloads = 5

	updated, err := service.Update(updateReq)
	if err != nil {
		t.Fatalf("failed to update settings: %v", err)
	}

	if updated.Appearance.Theme != ThemeDark {
		t.Errorf("expected ThemeDark, got %s", updated.Appearance.Theme)
	}
	if notified == nil || notified.Appearance.Theme != ThemeDark {
		t.Fatalf("expected broadcast callback to be called with updated settings")
	}

	// Verify file was written on disk atomically
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("failed to read config file from disk: %v", err)
	}

	var diskSettings Settings
	if err := json.Unmarshal(data, &diskSettings); err != nil {
		t.Fatalf("failed to unmarshal saved json: %v", err)
	}
	if diskSettings.Appearance.Theme != ThemeDark {
		t.Errorf("disk config expected ThemeDark, got %s", diskSettings.Appearance.Theme)
	}

	// Create a new service pointing to the same file, should load persisted settings
	reloadedService := NewSettingsService(configFile, "/default/downloads", "/default/temp", nil)
	reloaded := reloadedService.Get()
	if reloaded.Appearance.Theme != ThemeDark {
		t.Errorf("reloaded config expected ThemeDark, got %s", reloaded.Appearance.Theme)
	}
	if reloaded.Appearance.AccentColor != "#3b82f6" {
		t.Errorf("reloaded config expected accent color #3b82f6, got %s", reloaded.Appearance.AccentColor)
	}
}

func TestResolveCategory_BuiltinAndFallback(t *testing.T) {
	s := DefaultSettings("/downloads", "/temp")

	// Video
	vCat, ok := s.Download.ResolveCategory("movie.mp4")
	if !ok || vCat.Name != "视频" {
		t.Errorf("expected video category for movie.mp4, got %v (%v)", vCat.Name, ok)
	}
	if vCat.Directory != filepath.Join("/downloads", "Videos") {
		t.Errorf("expected video dir, got %s", vCat.Directory)
	}

	// Audio
	aCat, ok := s.Download.ResolveCategory("song.mp3")
	if !ok || aCat.Name != "音频" {
		t.Errorf("expected audio category for song.mp3, got %v (%v)", aCat.Name, ok)
	}

	// Image
	iCat, ok := s.Download.ResolveCategory("photo.png")
	if !ok || iCat.Name != "图片" {
		t.Errorf("expected image category for photo.png, got %v (%v)", iCat.Name, ok)
	}

	// Archive
	arcCat, ok := s.Download.ResolveCategory("data.tar.gz")
	if !ok || arcCat.Name != "压缩包" {
		t.Errorf("expected archive category for data.tar.gz, got %v (%v)", arcCat.Name, ok)
	}

	// Software
	softCat, ok := s.Download.ResolveCategory("setup.exe")
	if !ok || softCat.Name != "软件" {
		t.Errorf("expected software category for setup.exe, got %v (%v)", softCat.Name, ok)
	}

	// Document / File
	docCat, ok := s.Download.ResolveCategory("report.pdf")
	if !ok || docCat.Name != "文件" {
		t.Errorf("expected file category for report.pdf, got %v (%v)", docCat.Name, ok)
	}

	// Unmatched extension: falls back into "文件"
	unmatchedCat, ok := s.Download.ResolveCategory("mystery.xyz123")
	if !ok || unmatchedCat.Name != "文件" {
		t.Errorf("expected fallback to file category for mystery.xyz123, got %v (%v)", unmatchedCat.Name, ok)
	}
	if unmatchedCat.Directory != filepath.Join("/downloads", "Files") {
		t.Errorf("expected fallback to Files directory, got %s", unmatchedCat.Directory)
	}

	// No extension: falls back into "文件"
	noExtCat, ok := s.Download.ResolveCategory("Makefile")
	if !ok || noExtCat.Name != "文件" {
		t.Errorf("expected fallback to file category for Makefile, got %v (%v)", noExtCat.Name, ok)
	}
}

func TestResolveCategory_OverlappingRulesAndCustomPrecedence(t *testing.T) {
	s := DefaultSettings("/downloads", "/temp")

	// Add custom category that overlaps with built-in "压缩包" (rar)
	customWork := CategoryConfig{
		ID:         "custom-work",
		Name:       "工作压缩包",
		Directory:  "/work/archives",
		Extensions: []string{"rar", "workzip"},
	}
	s.Download.CustomCategories = []CategoryConfig{customWork}

	// When custom category is present, .rar matches the custom category first!
	res, ok := s.Download.ResolveCategory("package.rar")
	if !ok || res.Name != "工作压缩包" {
		t.Fatalf("expected custom category 工作压缩包 for package.rar, got %s", res.Name)
	}
	if res.Directory != "/work/archives" {
		t.Errorf("expected /work/archives, got %s", res.Directory)
	}

	// Other archive extensions still match built-in "压缩包"
	zipRes, ok := s.Download.ResolveCategory("normal.zip")
	if !ok || zipRes.Name != "压缩包" {
		t.Fatalf("expected builtin 压缩包 for normal.zip, got %s", zipRes.Name)
	}

	// If custom category is removed, .rar must immediately fall back to built-in "压缩包"
	s.Download.CustomCategories = nil
	fallbackRes, ok := s.Download.ResolveCategory("package.rar")
	if !ok || fallbackRes.Name != "压缩包" {
		t.Fatalf("expected fallback to builtin 压缩包 after deleting custom category, got %s", fallbackRes.Name)
	}
	if fallbackRes.Directory != filepath.Join("/downloads", "Archives") {
		t.Errorf("expected builtin Archives directory, got %s", fallbackRes.Directory)
	}
}

func TestResolveCategory_CustomOrderingTopToBottom(t *testing.T) {
	s := DefaultSettings("/downloads", "/temp")

	catA := CategoryConfig{
		ID:         "cat-a",
		Name:       "分类A",
		Directory:  "/dir/a",
		Extensions: []string{"log"},
	}
	catB := CategoryConfig{
		ID:         "cat-b",
		Name:       "分类B",
		Directory:  "/dir/b",
		Extensions: []string{"log"},
	}

	// Case 1: catA is on top -> matches catA
	s.Download.CustomCategories = []CategoryConfig{catA, catB}
	res1, _ := s.Download.ResolveCategory("server.log")
	if res1.Name != "分类A" || res1.Directory != "/dir/a" {
		t.Errorf("expected 分类A, got %s (%s)", res1.Name, res1.Directory)
	}

	// Case 2: drag/reorder so catB is on top -> matches catB
	s.Download.CustomCategories = []CategoryConfig{catB, catA}
	res2, _ := s.Download.ResolveCategory("server.log")
	if res2.Name != "分类B" || res2.Directory != "/dir/b" {
		t.Errorf("expected 分类B, got %s (%s)", res2.Name, res2.Directory)
	}
}

func TestSettingsValidation_CategoriesAndServerFileTime(t *testing.T) {
	rawJSON := `{
		"download": {
			"defaultDirectory": "/custom/downloads",
			"tempDirectory": "/custom/temp",
			"useServerFileTime": true,
			"customCategories": [
				{"name": "电子书", "directory": "", "extensions": [".epub", "mobi", "EPUB"]}
			]
		}
	}`

	var s Settings
	if err := json.Unmarshal([]byte(rawJSON), &s); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	validated := s.ValidateAndFallback("/fallback/dl", "/fallback/tmp")
	if !validated.Download.UseServerFileTime {
		t.Errorf("expected UseServerFileTime true")
	}
	if len(validated.Download.BuiltinCategories) != 6 {
		t.Errorf("expected 6 builtin categories populated, got %d", len(validated.Download.BuiltinCategories))
	}
	if len(validated.Download.CustomCategories) != 1 {
		t.Fatalf("expected 1 custom category, got %d", len(validated.Download.CustomCategories))
	}

	custom := validated.Download.CustomCategories[0]
	if custom.Name != "电子书" {
		t.Errorf("expected 电子书, got %s", custom.Name)
	}
	if custom.Directory != "/custom/downloads" {
		t.Errorf("expected fallback to default directory when empty, got %s", custom.Directory)
	}
	// Extensions should be normalized (lowercase, no leading dots, deduplicated)
	if len(custom.Extensions) != 2 || custom.Extensions[0] != "epub" || custom.Extensions[1] != "mobi" {
		t.Errorf("expected [epub, mobi], got %v", custom.Extensions)
	}
}

func TestAssignExtensionToCategory(t *testing.T) {
	s := DefaultSettings("/dl", "/tmp")
	customA := CategoryConfig{
		ID:         "custom-a",
		Name:       "自定义A",
		Directory:  "/dl/a",
		Extensions: []string{"customext", "shared"},
	}
	customB := CategoryConfig{
		ID:         "custom-b",
		Name:       "自定义B",
		Directory:  "/dl/b",
		Extensions: []string{"another"},
	}
	s.Download.CustomCategories = []CategoryConfig{customA, customB}

	// Case 1: Extension already in target -> no-op
	updated, changed := s.Download.AssignExtensionToCategory("customext", "custom-a")
	if changed {
		t.Errorf("expected changed=false for already present extension")
	}

	// Case 2: In builtin "压缩包" (rar), assign to custom "custom-a"
	// Overlapping rule: must be added to custom-a, but NOT removed from builtin-archive!
	updated, changed = s.Download.AssignExtensionToCategory(".RAR", "custom-a")
	if !changed {
		t.Errorf("expected changed=true")
	}
	// Check custom-a has rar
	foundInCustom := false
	for _, e := range updated.CustomCategories[0].Extensions {
		if e == "rar" {
			foundInCustom = true
		}
	}
	if !foundInCustom {
		t.Errorf("expected rar in custom-a")
	}
	// Check builtin-archive STILL has rar
	foundInBuiltin := false
	for _, b := range updated.BuiltinCategories {
		if b.ID == "builtin-archive" {
			for _, e := range b.Extensions {
				if e == "rar" {
					foundInBuiltin = true
				}
			}
		}
	}
	if !foundInBuiltin {
		t.Errorf("expected rar to be preserved in builtin-archive under overlapping rule")
	}

	// Case 2b: .rar is now in custom-a AND builtin-archive.
	// Assigning .rar to builtin-archive (which already has rar) MUST remove rar from custom-a!
	reassignedToBuiltin, changed2b := updated.AssignExtensionToCategory("rar", "builtin-archive")
	if !changed2b {
		t.Errorf("expected changed2b=true when pruning rar from custom-a")
	}
	for _, e := range reassignedToBuiltin.CustomCategories[0].Extensions {
		if e == "rar" {
			t.Errorf("expected rar to be removed from custom-a after re-assigning to builtin-archive")
		}
	}
	// Next resolution for package.rar must now resolve to builtin-archive!
	resolvedCat, _ := reassignedToBuiltin.ResolveCategory("package.rar")
	if resolvedCat.ID != "builtin-archive" {
		t.Errorf("expected builtin-archive after re-assigning rar back to builtin, got %s", resolvedCat.ID)
	}

	// Continue with updated for Case 3
	updated = reassignedToBuiltin

	// Case 3: In custom-a (shared), assign to builtin "builtin-software"
	// Must be removed from custom-a and added to builtin-software!
	updated, changed = updated.AssignExtensionToCategory("shared", "builtin-software")
	if !changed {
		t.Errorf("expected changed=true")
	}
	for _, e := range updated.CustomCategories[0].Extensions {
		if e == "shared" {
			t.Errorf("expected shared to be removed from custom-a")
		}
	}
	foundInSoftware := false
	for _, b := range updated.BuiltinCategories {
		if b.ID == "builtin-software" {
			for _, e := range b.Extensions {
				if e == "shared" {
					foundInSoftware = true
				}
			}
		}
	}
	if !foundInSoftware {
		t.Errorf("expected shared to be added to builtin-software")
	}

	// Case 4: In custom-a (customext), assign to custom-b
	// Must be removed from custom-a and added to custom-b
	updated, changed = updated.AssignExtensionToCategory("customext", "custom-b")
	if !changed {
		t.Errorf("expected changed=true")
	}
	for _, e := range updated.CustomCategories[0].Extensions {
		if e == "customext" {
			t.Errorf("expected customext removed from custom-a")
		}
	}
	foundInB := false
	for _, e := range updated.CustomCategories[1].Extensions {
		if e == "customext" {
			foundInB = true
		}
	}
	if !foundInB {
		t.Errorf("expected customext in custom-b")
	}

	// Case 5: In builtin-video (mp4), assign to builtin-audio
	// Both are builtin: must be removed from builtin-video and added to builtin-audio!
	updated, changed = updated.AssignExtensionToCategory("mp4", "builtin-audio")
	if !changed {
		t.Errorf("expected changed=true")
	}
	for _, b := range updated.BuiltinCategories {
		if b.ID == "builtin-video" {
			for _, e := range b.Extensions {
				if e == "mp4" {
					t.Errorf("expected mp4 removed from builtin-video")
				}
			}
		}
		if b.ID == "builtin-audio" {
			hasMp4 := false
			for _, e := range b.Extensions {
				if e == "mp4" {
					hasMp4 = true
				}
			}
			if !hasMp4 {
				t.Errorf("expected mp4 added to builtin-audio")
			}
		}
	}
}

func TestSettings_SetCategoryDirectory(t *testing.T) {
	s := DefaultSettings("/dl", "/tmp")
	s.Download.CustomCategories = []CategoryConfig{
		{ID: "custom-1", Name: "文档", Directory: "/old/custom"},
	}

	// 1. Update custom category directory
	updated, changed := s.Download.SetCategoryDirectory("custom-1", "/new/custom")
	if !changed {
		t.Fatalf("expected changed=true for custom category")
	}
	if updated.CustomCategories[0].Directory != "/new/custom" {
		t.Errorf("expected /new/custom, got %s", updated.CustomCategories[0].Directory)
	}

	// 2. Same directory -> no change
	_, changed = updated.SetCategoryDirectory("custom-1", "/new/custom")
	if changed {
		t.Errorf("expected changed=false when directory is identical")
	}

	// 3. Update builtin category directory
	updatedBuiltin, changedBuiltin := s.Download.SetCategoryDirectory("builtin-video", "/new/videos")
	if !changedBuiltin {
		t.Fatalf("expected changed=true for builtin category")
	}
	found := false
	for _, b := range updatedBuiltin.BuiltinCategories {
		if b.ID == "builtin-video" {
			found = true
			if b.Directory != "/new/videos" {
				t.Errorf("expected /new/videos, got %s", b.Directory)
			}
		}
	}
	if !found {
		t.Errorf("builtin-video not found")
	}

	// 4. Non-existent category -> no change
	_, changedNone := s.Download.SetCategoryDirectory("non-existent", "/some/path")
	if changedNone {
		t.Errorf("expected changed=false for non-existent category")
	}
}

func TestSiteMatchesExcluded(t *testing.T) {
	excluded := []string{
		"example.com",
		"https://github.com/path",
		"download.mysite.org:8080",
		"*.bilibili.com",
		"*cdn*",
	}

	norm := NormalizeSites(excluded)
	expected := []string{"example.com", "github.com", "download.mysite.org", "*.bilibili.com", "*cdn*"}
	if len(norm) != len(expected) {
		t.Fatalf("expected %d normalized sites, got %d: %v", len(expected), len(norm), norm)
	}
	for i, exp := range expected {
		if norm[i] != exp {
			t.Errorf("expected norm[%d] == %s, got %s", i, exp, norm[i])
		}
	}
	// Test matching
	tests := []struct {
		page     string
		expected bool
	}{
		{"example.com", true},
		{"sub.example.com", true},
		{"deep.nested.sub.example.com", true},
		{"notexample.com", false},
		{"github.com", true},
		{"api.github.com", true},
		{"mygithub.com", false},
		{"download.mysite.org", true},
		{"bilibili.com", true},
		{"www.bilibili.com", true},
		{"api.live.bilibili.com", true},
		{"notbilibili.com", false},
		{"fastcdn.org", true},
		{"static-cdn-asset.com", true},
		{"other.org", false},
		{"", false},
	}

	for _, tc := range tests {
		got := SiteMatchesExcluded(tc.page, norm)
		if got != tc.expected {
			t.Errorf("SiteMatchesExcluded(%q) = %v, want %v", tc.page, got, tc.expected)
		}
	}
}

// TestResolveDestination_SingleRule 保证"命中分类"与"保存目录"出自同一规则，
// 避免两条入口对同一文件名给出不同落点。
func TestResolveDestination_SingleRule(t *testing.T) {
	d := DownloadConfig{
		DefaultDirectory: "/downloads",
		BuiltinCategories: []CategoryConfig{
			{ID: "builtin-video", Name: "视频", Directory: "/downloads/Videos", Extensions: []string{"mp4"}},
			{ID: "builtin-file", Name: "文件", Directory: "/downloads/Files", Extensions: []string{"pdf"}},
		},
	}

	cat, dir := d.ResolveDestination("clip.mp4")
	if cat.ID != "builtin-video" || dir != "/downloads/Videos" {
		t.Fatalf("expected video category and its directory, got %q / %q", cat.ID, dir)
	}
	if onlyDir := d.ResolveCategoryDirectory("clip.mp4"); onlyDir != dir {
		t.Fatalf("ResolveCategoryDirectory gave %q but ResolveDestination gave %q", onlyDir, dir)
	}

	// 分类未配置自己的目录时落到默认目录。
	d.BuiltinCategories[0].Directory = ""
	if _, dir := d.ResolveDestination("clip.mp4"); dir != "/downloads" {
		t.Fatalf("expected default directory fallback, got %q", dir)
	}
}

func TestProxyModeValidation(t *testing.T) {
	s := Settings{
		Proxy: ProxyConfig{
			Mode:       ProxyModeCustom,
			CustomAddr: "  http://127.0.0.1:7890  ",
		},
	}
	validated := s.ValidateAndFallback("/d", "/t")
	if validated.Proxy.Mode != ProxyModeCustom {
		t.Errorf("expected ProxyModeCustom, got %s", validated.Proxy.Mode)
	}
	if validated.Proxy.CustomAddr != "http://127.0.0.1:7890" {
		t.Errorf("expected trimmed customAddr, got %q", validated.Proxy.CustomAddr)
	}
}

// TestProxyCustomAddrValidation pins the invariant the transport layer relies on:
// a custom proxy address reaching it is always a parseable absolute URL, so an
// unusable address can never stay in settings while the UI reports it as active.
func TestProxyCustomAddrValidation(t *testing.T) {
	cases := []struct {
		name string
		addr string
	}{
		{"empty address", ""},
		{"malformed scheme", "://bad-url"},
		{"missing scheme", "127.0.0.1:7890"},
		{"missing host", "http://"},
		{"unsupported scheme", "ftp://127.0.0.1:7890"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Settings{Proxy: ProxyConfig{Mode: ProxyModeCustom, CustomAddr: tc.addr}}
			validated := s.ValidateAndFallback("/d", "/t")
			if validated.Proxy.Mode != ProxyModeSystem {
				t.Fatalf(
					"expected fallback to ProxyModeSystem for %q, got %s",
					tc.addr,
					validated.Proxy.Mode,
				)
			}
			if validated.Proxy.CustomAddr != "" {
				t.Fatalf("expected cleared customAddr for %q, got %q", tc.addr, validated.Proxy.CustomAddr)
			}
		})
	}
}

// TestSettingsService_ReportsApplyFailure 保证应用新设置失败时错误回到发起更新的调用方，
// 而不是被丢弃后让界面显示一个没有生效的状态。
func TestSettingsService_ReportsApplyFailure(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.json")
	applyErr := errors.New("apply rejected")

	service := NewSettingsService(configFile, "/dl", "/temp", func(updated *Settings) error {
		return applyErr
	})

	if _, err := service.Update(DefaultSettings("/dl", "/temp")); !errors.Is(err, applyErr) {
		t.Fatalf("expected apply failure to reach the caller, got %v", err)
	}
}

func TestSettingsService_ReentrantDeadlock(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.json")

	var service *SettingsService
	service = NewSettingsService(configFile, "/dl", "/temp", func(updated *Settings) error {
		// Attempting to read settings inside onChanged callback (exact calling chain of watcher.Start())
		_ = service.Get()
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		st := service.Get()
		st.Clipboard.Enabled = true
		_, _ = service.Update(st)
	}()

	select {
	case <-done:
		// Succeeded without deadlock
	case <-time.After(1 * time.Second):
		t.Fatal("DEADLOCK: service.Update deadlocked when onChanged callback called service.Get()")
	}
}
