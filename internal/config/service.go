package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// OnSettingsChangedFunc is invoked whenever settings are updated.
type OnSettingsChangedFunc func(updated *Settings)

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

	// Atomic write: write to temp file, flush, then rename
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.mu.Unlock()
		return s.current, fmt.Errorf("failed to create config directory: %w", err)
	}

	tempFile := filepath.Join(dir, fmt.Sprintf(".config.json.tmp-%d", os.Getpid()))
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		s.mu.Unlock()
		return s.current, fmt.Errorf("failed to write temp config file: %w", err)
	}

	backupFile := filepath.Join(dir, ".config.json.bak")
	hasExisting := false
	if _, err := os.Stat(s.filePath); err == nil {
		hasExisting = true
		_ = os.Remove(backupFile)
		if err := os.Rename(s.filePath, backupFile); err != nil {
			_ = os.Remove(tempFile)
			s.mu.Unlock()
			return s.current, fmt.Errorf("failed to backup existing config file: %w", err)
		}
	}

	if err := os.Rename(tempFile, s.filePath); err != nil {
		if hasExisting {
			_ = os.Rename(backupFile, s.filePath)
		}
		_ = os.Remove(tempFile)
		s.mu.Unlock()
		return s.current, fmt.Errorf("failed to replace config file: %w", err)
	}
	if hasExisting {
		_ = os.Remove(backupFile)
	}

	s.current = validated
	s.mu.Unlock()

	if s.onChanged != nil {
		snapshot := validated
		s.onChanged(&snapshot)
	}

	return validated, nil
}
