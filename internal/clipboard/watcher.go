// Package clipboard implements background clipboard monitoring and URL detection for SheepGet.
package clipboard

import (
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"sheep-get/internal/config"
)

// Reader abstracts the system clipboard text access for testability.
type Reader interface {
	Text() (string, bool)
}

// TriggerFunc is invoked when a matching download URL is detected on the clipboard.
type TriggerFunc func(urlStr string)

// Watcher monitors the clipboard for downloadable URLs according to takeover extension rules.
type Watcher struct {
	mu           sync.Mutex
	reader       Reader
	getSettings  func() config.Settings
	trigger      TriggerFunc
	lastText     string
	lastSequence uint32
	hasSequence  bool
	stopChan     chan struct{}
	running      bool
	pollInterval time.Duration
}

// NewWatcher creates a new clipboard Watcher.
func NewWatcher(reader Reader, getSettings func() config.Settings, trigger TriggerFunc) *Watcher {
	return &Watcher{
		reader:       reader,
		getSettings:  getSettings,
		trigger:      trigger,
		pollInterval: 800 * time.Millisecond,
		hasSequence:  hasSequenceSupport(),
	}
}

// SetPollInterval sets the interval between clipboard checks (useful in unit tests).
func (w *Watcher) SetPollInterval(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pollInterval = d
}

// SetSequenceSupport configures whether Win32 clipboard sequence checking is used.
func (w *Watcher) SetSequenceSupport(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hasSequence = enabled
}

// MatchDownloadURL checks if text is a valid HTTP/HTTPS URL whose path has an extension
// matching the provided takeover extensions (ignoring query parameters and fragments).
// Returns the matched URL and true, or empty string and false.
func MatchDownloadURL(text string, takeoverExts []string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}

	if u.Host == "" {
		return "", false
	}

	// Extract path only, completely ignoring query string and fragment
	cleanPath := u.Path
	if cleanPath == "" || cleanPath == "/" {
		return "", false
	}

	ext := strings.ToLower(strings.TrimPrefix(path.Ext(cleanPath), "."))
	if ext == "" {
		return "", false
	}

	for _, tExt := range takeoverExts {
		norm := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tExt, ".")))
		if norm != "" && norm == ext {
			return trimmed, true
		}
	}

	return "", false
}

// Start begins the background clipboard polling loop if enabled in settings.
func (w *Watcher) Start() {
	if w.getSettings != nil && !w.getSettings().Clipboard.Enabled {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return
	}

	w.running = true
	w.stopChan = make(chan struct{})
	interval := w.pollInterval
	if interval <= 0 {
		interval = 800 * time.Millisecond
	}

	go w.pollLoop(w.stopChan, interval)
}

// Stop stops the background polling loop.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return
	}

	close(w.stopChan)
	w.running = false
}

// IsRunning reports whether clipboard monitoring is actively polling.
func (w *Watcher) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// OnSettingsUpdated adapts the watcher's running state when settings change.
func (w *Watcher) OnSettingsUpdated(s *config.Settings) {
	if s == nil {
		return
	}
	if s.Clipboard.Enabled {
		w.Start()
	} else {
		w.Stop()
	}
}

func (w *Watcher) pollLoop(stop <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			w.CheckOnce()
		}
	}
}

// CheckOnce checks the clipboard one time and triggers if a new matching URL is detected.
func (w *Watcher) CheckOnce() {
	w.mu.Lock()
	if w.reader == nil || w.getSettings == nil || w.trigger == nil {
		w.mu.Unlock()
		return
	}

	st := w.getSettings()
	if !st.Clipboard.Enabled {
		w.mu.Unlock()
		return
	}

	// If Win32 sequence checking is available and unchanged, skip text retrieval
	if w.hasSequence {
		seq := getClipboardSequence()
		if seq != 0 && seq == w.lastSequence {
			w.mu.Unlock()
			return
		}
		w.lastSequence = seq
	}

	text, ok := w.reader.Text()
	if !ok {
		w.mu.Unlock()
		return
	}
	text = strings.TrimSpace(text)
	if text == "" || text == w.lastText {
		w.mu.Unlock()
		return
	}

	// Always update lastText so we do not repeatedly process the same clipboard text
	w.lastText = text
	triggerFn := w.trigger
	downloadExts := st.Download.AllExtensions()
	w.mu.Unlock()

	if matchedURL, matched := MatchDownloadURL(text, downloadExts); matched {
		triggerFn(matchedURL)
	}
}
