// Package config manages application configuration, schema validation, and atomic persistence.
package config

import (
	"regexp"
	"strings"
)

// ThemeMode specifies the UI theme mode.
type ThemeMode string

const (
	ThemeSystem ThemeMode = "system"
	ThemeLight  ThemeMode = "light"
	ThemeDark   ThemeMode = "dark"
)

// DuplicateURLPolicy specifies how duplicate URLs should be handled.
type DuplicateURLPolicy string

const (
	DuplicatePolicyAsk          DuplicateURLPolicy = "ask"
	DuplicatePolicySkipShowDone DuplicateURLPolicy = "skip_show_done"
	DuplicatePolicyOverwrite    DuplicateURLPolicy = "overwrite"
	DuplicatePolicyNumberedCopy DuplicateURLPolicy = "numbered_copy"
)

const (
	DefaultAccentColor = "#10b981" // Emerald-500
)

var hexColorRegex = regexp.MustCompile(`^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$`)

// AppearanceConfig specifies UI appearance settings.
type AppearanceConfig struct {
	Theme       ThemeMode `json:"theme"`
	AccentColor string    `json:"accentColor"`
}

// DownloadConfig specifies download engine and directory rules.
type DownloadConfig struct {
	DuplicateURLPolicy        DuplicateURLPolicy `json:"duplicateUrlPolicy"`
	PreDownload               bool               `json:"preDownload"`
	MaxConcurrentDownloads    int                `json:"maxConcurrentDownloads"`
	DefaultConnectionsPerTask int                `json:"defaultConnectionsPerTask"`
	DefaultDirectory          string             `json:"defaultDirectory"`
	TempDirectory             string             `json:"tempDirectory"`
}

// Settings represents the root settings structure.
type Settings struct {
	Appearance AppearanceConfig `json:"appearance"`
	Download   DownloadConfig   `json:"download"`
}

// DefaultSettings returns valid default settings with provided default download and temp directories.
func DefaultSettings(defaultDownloadDir, defaultTempDir string) Settings {
	return Settings{
		Appearance: AppearanceConfig{
			Theme:       ThemeSystem,
			AccentColor: DefaultAccentColor,
		},
		Download: DownloadConfig{
			DuplicateURLPolicy:        DuplicatePolicyAsk,
			PreDownload:               false,
			MaxConcurrentDownloads:    3,
			DefaultConnectionsPerTask: 8,
			DefaultDirectory:          defaultDownloadDir,
			TempDirectory:             defaultTempDir,
		},
	}
}

// ValidateAndFallback validates configuration fields and safely falls back to defaults for invalid/empty values.
func (s Settings) ValidateAndFallback(fallbackDownloadDir, fallbackTempDir string) Settings {
	defaults := DefaultSettings(fallbackDownloadDir, fallbackTempDir)

	// Validate Theme
	switch s.Appearance.Theme {
	case ThemeSystem, ThemeLight, ThemeDark:
	default:
		s.Appearance.Theme = defaults.Appearance.Theme
	}

	// Validate AccentColor
	s.Appearance.AccentColor = strings.TrimSpace(s.Appearance.AccentColor)
	if !hexColorRegex.MatchString(s.Appearance.AccentColor) {
		s.Appearance.AccentColor = defaults.Appearance.AccentColor
	}

	// Validate DuplicateURLPolicy
	switch s.Download.DuplicateURLPolicy {
	case DuplicatePolicyAsk, DuplicatePolicySkipShowDone, DuplicatePolicyOverwrite, DuplicatePolicyNumberedCopy:
	default:
		s.Download.DuplicateURLPolicy = defaults.Download.DuplicateURLPolicy
	}

	// Validate MaxConcurrentDownloads (bounded 1-32)
	if s.Download.MaxConcurrentDownloads < 1 || s.Download.MaxConcurrentDownloads > 32 {
		s.Download.MaxConcurrentDownloads = defaults.Download.MaxConcurrentDownloads
	}

	// Validate DefaultConnectionsPerTask (bounded 1-32)
	if s.Download.DefaultConnectionsPerTask < 1 || s.Download.DefaultConnectionsPerTask > 32 {
		s.Download.DefaultConnectionsPerTask = defaults.Download.DefaultConnectionsPerTask
	}

	// Validate DefaultDirectory
	s.Download.DefaultDirectory = strings.TrimSpace(s.Download.DefaultDirectory)
	if s.Download.DefaultDirectory == "" {
		s.Download.DefaultDirectory = defaults.Download.DefaultDirectory
	}

	// Validate TempDirectory
	s.Download.TempDirectory = strings.TrimSpace(s.Download.TempDirectory)
	if s.Download.TempDirectory == "" {
		s.Download.TempDirectory = defaults.Download.TempDirectory
	}

	return s
}
