package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
}

func TestSettingsService_AtomicPersistenceAndBroadcast(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.json")

	var notified *Settings
	service := NewSettingsService(configFile, "/default/downloads", "/default/temp", func(updated *Settings) {
		notified = updated
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
