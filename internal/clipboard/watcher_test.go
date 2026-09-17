package clipboard_test

import (
	"sync"
	"testing"
	"time"

	"sheep-get/internal/clipboard"
	"sheep-get/internal/config"
)

type mockClipboard struct {
	mu   sync.Mutex
	text string
	ok   bool
}

func (m *mockClipboard) Text() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.text, m.ok
}

func (m *mockClipboard) SetText(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.text = text
	m.ok = true
}

func TestMatchDownloadURL(t *testing.T) {
	takeoverExts := []string{"zip", "rar", "7z", "tar", "gz", "exe", "msi", "mp4", "mkv", "pdf"}

	tests := []struct {
		name     string
		input    string
		wantURL  string
		wantBool bool
	}{
		{
			name:     "Valid simple zip URL",
			input:    "https://example.com/archive.zip",
			wantURL:  "https://example.com/archive.zip",
			wantBool: true,
		},
		{
			name:     "URL with query params and tokens",
			input:    "https://cdn.example.com/downloads/package.zip?sign=abc123xyz&expire=99999",
			wantURL:  "https://cdn.example.com/downloads/package.zip?sign=abc123xyz&expire=99999",
			wantBool: true,
		},
		{
			name:     "URL with hash fragment",
			input:    "http://files.site.org/app.exe#section1",
			wantURL:  "http://files.site.org/app.exe#section1",
			wantBool: true,
		},
		{
			name:     "URL with uppercase extension",
			input:    "https://example.com/video.MP4",
			wantURL:  "https://example.com/video.MP4",
			wantBool: true,
		},
		{
			name:     "Web page without extension",
			input:    "https://github.com/ygq-future/SheepGet",
			wantURL:  "",
			wantBool: false,
		},
		{
			name:     "Web page root",
			input:    "https://www.google.com/",
			wantURL:  "",
			wantBool: false,
		},
		{
			name:     "Non-takeover extension HTML",
			input:    "https://example.com/page.html",
			wantURL:  "",
			wantBool: false,
		},
		{
			name:     "Non-HTTP scheme FTP",
			input:    "ftp://ftp.example.com/archive.zip",
			wantURL:  "",
			wantBool: false,
		},
		{
			name:     "Plain text string",
			input:    "Just a random copied string without URL",
			wantURL:  "",
			wantBool: false,
		},
		{
			name:     "Empty string",
			input:    "   ",
			wantURL:  "",
			wantBool: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotBool := clipboard.MatchDownloadURL(tc.input, takeoverExts)
			if gotBool != tc.wantBool {
				t.Fatalf("MatchDownloadURL(%q) bool = %v, want %v", tc.input, gotBool, tc.wantBool)
			}
			if gotURL != tc.wantURL {
				t.Fatalf("MatchDownloadURL(%q) URL = %q, want %q", tc.input, gotURL, tc.wantURL)
			}
		})
	}
}

func TestWatcher_CheckOnce(t *testing.T) {
	mockClip := &mockClipboard{}
	st := config.DefaultSettings("/dl", "/tmp")
	st.Clipboard.Enabled = true

	var triggered []string
	var trigMu sync.Mutex

	w := clipboard.NewWatcher(mockClip, func() config.Settings {
		return st
	}, func(urlStr string) {
		trigMu.Lock()
		defer trigMu.Unlock()
		triggered = append(triggered, urlStr)
	})
	w.SetSequenceSupport(false)

	// 1. Initially empty clipboard
	w.CheckOnce()
	if len(triggered) != 0 {
		t.Fatalf("expected 0 triggered on empty clipboard")
	}

	// 2. Set non-matching URL (web page)
	mockClip.SetText("https://github.com/ygq-future")
	w.CheckOnce()
	if len(triggered) != 0 {
		t.Fatalf("expected 0 triggered for web URL")
	}

	// 3. Set matching URL
	mockClip.SetText("https://example.com/release.zip?token=123")
	w.CheckOnce()
	trigMu.Lock()
	if len(triggered) != 1 || triggered[0] != "https://example.com/release.zip?token=123" {
		t.Fatalf("expected 1 triggered matching URL, got %v", triggered)
	}
	trigMu.Unlock()

	// 4. Same clipboard text repeated does NOT trigger again
	w.CheckOnce()
	trigMu.Lock()
	if len(triggered) != 1 {
		t.Fatalf("expected trigger not repeated for identical clipboard text")
	}
	trigMu.Unlock()

	// 5. Disabled in settings does not trigger
	st.Clipboard.Enabled = false
	mockClip.SetText("https://example.com/another.mp4")
	w.CheckOnce()
	trigMu.Lock()
	if len(triggered) != 1 {
		t.Fatalf("expected no trigger when Clipboard.Enabled is false")
	}
	trigMu.Unlock()
}

func TestWatcher_StartStopAndSettingsUpdate(t *testing.T) {
	mockClip := &mockClipboard{}
	st := config.DefaultSettings("/dl", "/tmp")
	st.Clipboard.Enabled = false

	var triggered []string
	var trigMu sync.Mutex

	w := clipboard.NewWatcher(mockClip, func() config.Settings {
		return st
	}, func(urlStr string) {
		trigMu.Lock()
		defer trigMu.Unlock()
		triggered = append(triggered, urlStr)
	})
	w.SetSequenceSupport(false)
	w.SetPollInterval(10 * time.Millisecond)

	// Start while disabled does not run
	w.Start()
	if w.IsRunning() {
		t.Fatalf("expected watcher not running when disabled in settings")
	}

	// Update settings to enabled
	st.Clipboard.Enabled = true
	w.OnSettingsUpdated(&st)
	if !w.IsRunning() {
		t.Fatalf("expected watcher running after setting enabled")
	}

	mockClip.SetText("https://download.example.com/test.zip")
	// Give background poll loop a moment
	time.Sleep(50 * time.Millisecond)

	trigMu.Lock()
	if len(triggered) != 1 {
		t.Fatalf("expected 1 triggered from polling loop, got %d", len(triggered))
	}
	trigMu.Unlock()

	// Update settings to disabled
	st.Clipboard.Enabled = false
	w.OnSettingsUpdated(&st)
	if w.IsRunning() {
		t.Fatalf("expected watcher stopped after setting disabled")
	}
}
