package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"sheep-get/internal/atomicfile"
)

// OnSettingsChangedFunc is invoked whenever settings are updated. Returning an error
// reports a failure to apply the new settings back to the caller performing the update.
type OnSettingsChangedFunc func(updated *Settings) error

// SettingsService manages reading, atomic writing, and broadcasting of settings.
type SettingsService struct {
	mu                  sync.RWMutex
	filePath            string
	fallbackDownloadDir string
	fallbackTempDir     string
	current             Settings
	onChanged           OnSettingsChangedFunc
}

// NewSettingsService creates a new SettingsService, loading from filePath if present or initializing defaults.
func NewSettingsService(filePath, fallbackDownloadDir, fallbackTempDir string, onChanged OnSettingsChangedFunc) *SettingsService {
	s := &SettingsService{
		filePath:            filePath,
		fallbackDownloadDir: fallbackDownloadDir,
		fallbackTempDir:     fallbackTempDir,
		onChanged:           onChanged,
	}

	s.load()
	return s
}

// load reads the configuration from disk, validating fields and falling back to safe defaults.
func (s *SettingsService) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		s.current = DefaultSettings(s.fallbackDownloadDir, s.fallbackTempDir)
		return
	}

	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		s.current = DefaultSettings(s.fallbackDownloadDir, s.fallbackTempDir)
		return
	}

	s.current = loaded.ValidateAndFallback(s.fallbackDownloadDir, s.fallbackTempDir)
}

// Get returns the current active settings snapshot safely.
func (s *SettingsService) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Update validates, atomically persists, updates in-memory state, and notifies listeners.
func (s *SettingsService) Update(req Settings) (Settings, error) {
	s.mu.Lock()

	validated := req.ValidateAndFallback(s.fallbackDownloadDir, s.fallbackTempDir)

	data, err := json.MarshalIndent(validated, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return s.current, fmt.Errorf("failed to marshal settings: %w", err)
	}

	// 配置只在用户点保存时写入，频率低，因此同步落盘，避免关机或断电后读到空配置。
	if err := atomicfile.Write(s.filePath, data, 0644, true); err != nil {
		s.mu.Unlock()
		return s.current, fmt.Errorf("failed to persist settings: %w", err)
	}

	s.current = validated
	s.mu.Unlock()

	if s.onChanged != nil {
		snapshot := validated
		if err := s.onChanged(&snapshot); err != nil {
			return validated, err
		}
	}

	return validated, nil
}
