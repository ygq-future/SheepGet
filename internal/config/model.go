// Package config manages application configuration, schema validation, and atomic persistence.
package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
	DuplicatePolicyPrompt            DuplicateURLPolicy = "prompt"
	DuplicatePolicySkipShowCompleted DuplicateURLPolicy = "skip_show_completed"
	DuplicatePolicyContinueOverwrite DuplicateURLPolicy = "continue_overwrite"
	DuplicatePolicyNumberedCopy      DuplicateURLPolicy = "numbered_copy"

	// Aliases maintained for backwards compatibility
	DuplicatePolicyAsk             DuplicateURLPolicy = DuplicatePolicyPrompt
	DuplicatePolicySkipShowDone    DuplicateURLPolicy = DuplicatePolicySkipShowCompleted
	DuplicatePolicyOverwrite       DuplicateURLPolicy = DuplicatePolicyContinueOverwrite
	DuplicatePolicySkipShowLegacy  DuplicateURLPolicy = "skip_show_done"
	DuplicatePolicyOverwriteLegacy DuplicateURLPolicy = "overwrite"
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

// CategoryConfig specifies an automatic archiving category rule.
type CategoryConfig struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Directory  string   `json:"directory"`
	Extensions []string `json:"extensions"`
	IsBuiltin  bool     `json:"isBuiltin"`
}

// DownloadConfig specifies download engine and directory rules.
type DownloadConfig struct {
	DuplicateURLPolicy        DuplicateURLPolicy `json:"duplicateUrlPolicy"`
	PreDownload               bool               `json:"preDownload"`
	MaxConcurrentDownloads    int                `json:"maxConcurrentDownloads"`
	DefaultConnectionsPerTask int                `json:"defaultConnectionsPerTask"`
	DefaultDirectory          string             `json:"defaultDirectory"`
	TempDirectory             string             `json:"tempDirectory"`
	UseServerFileTime         bool               `json:"useServerFileTime"`
	ShowProgressWindow        bool               `json:"showProgressWindow"`
	KeepCompletedInfo         bool               `json:"keepCompletedInfo"`
	AutoRemoveCompletedOnOpen bool               `json:"autoRemoveCompletedOnOpen"`
	BuiltinCategories         []CategoryConfig   `json:"builtinCategories"`
	CustomCategories          []CategoryConfig   `json:"customCategories"`
}

// UnmarshalJSON customizes unmarshaling to ensure showProgressWindow and keepCompletedInfo default to true when omitted.
func (d *DownloadConfig) UnmarshalJSON(data []byte) error {
	type alias DownloadConfig
	aux := &struct {
		ShowProgressWindow *bool `json:"showProgressWindow"`
		KeepCompletedInfo  *bool `json:"keepCompletedInfo"`
		*alias
	}{
		alias: (*alias)(d),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.ShowProgressWindow == nil {
		d.ShowProgressWindow = true
	} else {
		d.ShowProgressWindow = *aux.ShowProgressWindow
	}
	if aux.KeepCompletedInfo == nil {
		d.KeepCompletedInfo = true
	} else {
		d.KeepCompletedInfo = *aux.KeepCompletedInfo
	}
	return nil
}

// Settings represents the root settings structure.
type Settings struct {
	Appearance AppearanceConfig `json:"appearance"`
	Download   DownloadConfig   `json:"download"`
}

// DefaultBuiltinCategories returns the 6 preset built-in categories with their default directories and extensions.
func DefaultBuiltinCategories(defaultDownloadDir string) []CategoryConfig {
	base := defaultDownloadDir
	if base == "" {
		base = "Downloads"
	}
	return []CategoryConfig{
		{
			ID:         "builtin-video",
			Name:       "视频",
			Directory:  filepath.Join(base, "Videos"),
			Extensions: []string{"mp4", "mkv", "avi", "mov", "wmv", "flv", "webm", "m4v", "ts"},
			IsBuiltin:  true,
		},
		{
			ID:         "builtin-audio",
			Name:       "音频",
			Directory:  filepath.Join(base, "Audio"),
			Extensions: []string{"mp3", "wav", "flac", "aac", "ogg", "m4a", "wma", "opus"},
			IsBuiltin:  true,
		},
		{
			ID:         "builtin-image",
			Name:       "图片",
			Directory:  filepath.Join(base, "Images"),
			Extensions: []string{"jpg", "jpeg", "png", "gif", "webp", "svg", "bmp", "ico", "tiff"},
			IsBuiltin:  true,
		},
		{
			ID:         "builtin-archive",
			Name:       "压缩包",
			Directory:  filepath.Join(base, "Archives"),
			Extensions: []string{"zip", "rar", "7z", "tar", "gz", "bz2", "xz", "tgz"},
			IsBuiltin:  true,
		},
		{
			ID:         "builtin-software",
			Name:       "软件",
			Directory:  filepath.Join(base, "Software"),
			Extensions: []string{"exe", "msi", "dmg", "pkg", "deb", "rpm", "apk", "appimage", "iso"},
			IsBuiltin:  true,
		},
		{
			ID:         "builtin-file",
			Name:       "文件",
			Directory:  filepath.Join(base, "Files"),
			Extensions: []string{"doc", "docx", "pdf", "txt", "xls", "xlsx", "ppt", "pptx", "epub"},
			IsBuiltin:  true,
		},
	}
}

// NormalizeExtensions trims, lowercases, removes leading dots and eliminates duplicates.
func NormalizeExtensions(exts []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, ext := range exts {
		clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ext, ".")))
		if clean != "" && !seen[clean] {
			seen[clean] = true
			result = append(result, clean)
		}
	}
	return result
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
			UseServerFileTime:         false,
			ShowProgressWindow:        true,
			KeepCompletedInfo:         true,
			AutoRemoveCompletedOnOpen: false,
			BuiltinCategories:         DefaultBuiltinCategories(defaultDownloadDir),
			CustomCategories:          []CategoryConfig{},
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
	case DuplicatePolicyPrompt, DuplicatePolicySkipShowDone, DuplicatePolicyOverwrite, DuplicatePolicyNumberedCopy:
	case "ask":
		s.Download.DuplicateURLPolicy = DuplicatePolicyPrompt
	case "skip_show_done":
		s.Download.DuplicateURLPolicy = DuplicatePolicySkipShowDone
	case "overwrite":
		s.Download.DuplicateURLPolicy = DuplicatePolicyOverwrite
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

	// Validate BuiltinCategories
	defaultBuiltins := DefaultBuiltinCategories(s.Download.DefaultDirectory)
	if len(s.Download.BuiltinCategories) == 0 {
		s.Download.BuiltinCategories = defaultBuiltins
	} else {
		existingMap := make(map[string]CategoryConfig)
		for _, cat := range s.Download.BuiltinCategories {
			if cat.ID != "" {
				existingMap[cat.ID] = cat
			} else if cat.Name != "" {
				existingMap[cat.Name] = cat
			}
		}

		var merged []CategoryConfig
		for _, defCat := range defaultBuiltins {
			existing, ok := existingMap[defCat.ID]
			if !ok {
				existing, ok = existingMap[defCat.Name]
			}
			if ok {
				dir := strings.TrimSpace(existing.Directory)
				var exts []string
				if existing.Extensions != nil {
					exts = NormalizeExtensions(existing.Extensions)
				} else {
					exts = defCat.Extensions
				}
				merged = append(merged, CategoryConfig{
					ID:         defCat.ID,
					Name:       defCat.Name,
					Directory:  dir,
					Extensions: exts,
					IsBuiltin:  true,
				})
			} else {
				merged = append(merged, defCat)
			}
		}
		s.Download.BuiltinCategories = merged
	}

	// Validate CustomCategories
	if s.Download.CustomCategories == nil {
		s.Download.CustomCategories = []CategoryConfig{}
	} else {
		var validCustom []CategoryConfig
		for i, cat := range s.Download.CustomCategories {
			name := strings.TrimSpace(cat.Name)
			if name == "" {
				continue
			}
			id := cat.ID
			if id == "" {
				id = fmt.Sprintf("custom-%d", i+1)
			}
			dir := strings.TrimSpace(cat.Directory)
			if dir == "" {
				dir = s.Download.DefaultDirectory
			}
			validCustom = append(validCustom, CategoryConfig{
				ID:         id,
				Name:       name,
				Directory:  dir,
				Extensions: NormalizeExtensions(cat.Extensions),
				IsBuiltin:  false,
			})
		}
		s.Download.CustomCategories = validCustom
	}

	return s
}

// ResolveCategory returns the matched CategoryConfig and whether a matching rule was found.
// Precedence:
// 1. First matching custom category in CustomCategories (top-to-bottom order).
// 2. Matching non-file built-in category (video, audio, image, archive, software).
// 3. Explicit extension match in built-in "文件" category.
// 4. Unmatched fallback into built-in "文件" category (or DefaultDirectory).
func (d DownloadConfig) ResolveCategory(filename string) (CategoryConfig, bool) {
	ext := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(filepath.Ext(filename), ".")))

	buildResult := func(cat CategoryConfig) (CategoryConfig, bool) {
		dir := cat.Directory
		if dir == "" {
			dir = d.DefaultDirectory
		}
		return CategoryConfig{
			ID:         cat.ID,
			Name:       cat.Name,
			Directory:  dir,
			Extensions: cat.Extensions,
			IsBuiltin:  cat.IsBuiltin,
		}, true
	}

	if ext != "" {
		// 1. Custom categories (first match wins)
		for _, cat := range d.CustomCategories {
			for _, catExt := range cat.Extensions {
				if strings.EqualFold(strings.TrimPrefix(catExt, "."), ext) {
					return buildResult(cat)
				}
			}
		}

		// 2. Built-in non-file categories
		var fileCat *CategoryConfig
		for i := range d.BuiltinCategories {
			cat := &d.BuiltinCategories[i]
			if cat.ID == "builtin-file" || cat.Name == "文件" {
				fileCat = cat
				continue
			}
			for _, catExt := range cat.Extensions {
				if strings.EqualFold(strings.TrimPrefix(catExt, "."), ext) {
					return buildResult(*cat)
				}
			}
		}

		// 3. Check explicit file category extensions
		if fileCat != nil {
			for _, catExt := range fileCat.Extensions {
				if strings.EqualFold(strings.TrimPrefix(catExt, "."), ext) {
					return buildResult(*fileCat)
				}
			}
		}
	}

	// 4. Fallback: unmatched files enter "文件" category
	for i := range d.BuiltinCategories {
		cat := &d.BuiltinCategories[i]
		if cat.ID == "builtin-file" || cat.Name == "文件" {
			return buildResult(*cat)
		}
	}

	return CategoryConfig{
		ID:        "builtin-file",
		Name:      "文件",
		Directory: d.DefaultDirectory,
		IsBuiltin: true,
	}, false
}

// ResolveCategoryDirectory returns the save directory for a given filename.
func (d DownloadConfig) ResolveCategoryDirectory(filename string) string {
	cat, _ := d.ResolveCategory(filename)
	if cat.Directory != "" {
		return cat.Directory
	}
	return d.DefaultDirectory
}

// AssignExtensionToCategory assigns an extension to the target category by ID.
// Returns the updated DownloadConfig and whether any changes were made.
// Rules:
//  1. If extension is empty or target category does not exist: no-op.
//  2. If extension is already in target category: no-op.
//  3. If target is a custom category: remove extension from all other custom categories,
//     and preserve in builtin categories (overlapping rules).
//  4. If target is a builtin category: remove extension from all custom categories AND
//     all other builtin categories.
func (d DownloadConfig) AssignExtensionToCategory(ext, targetCategoryID string) (DownloadConfig, bool) {
	cleanExt := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ext, ".")))
	if cleanExt == "" || targetCategoryID == "" {
		return d, false
	}

	targetIsCustom := false
	targetIsBuiltin := false
	for _, c := range d.CustomCategories {
		if c.ID == targetCategoryID {
			targetIsCustom = true
			break
		}
	}
	if !targetIsCustom {
		for _, c := range d.BuiltinCategories {
			if c.ID == targetCategoryID {
				targetIsBuiltin = true
				break
			}
		}
	}
	if !targetIsCustom && !targetIsBuiltin {
		return d, false
	}

	removeExt := func(exts []string) ([]string, bool) {
		var filtered []string
		removed := false
		for _, e := range exts {
			if strings.EqualFold(strings.TrimPrefix(e, "."), cleanExt) {
				removed = true
			} else {
				filtered = append(filtered, e)
			}
		}
		return filtered, removed
	}

	changed := false

	// Check if already in target
	targetHasExt := false
	if targetIsCustom {
		for _, c := range d.CustomCategories {
			if c.ID == targetCategoryID {
				for _, e := range c.Extensions {
					if strings.EqualFold(strings.TrimPrefix(e, "."), cleanExt) {
						targetHasExt = true
						break
					}
				}
			}
		}
	} else {
		for _, c := range d.BuiltinCategories {
			if c.ID == targetCategoryID {
				for _, e := range c.Extensions {
					if strings.EqualFold(strings.TrimPrefix(e, "."), cleanExt) {
						targetHasExt = true
						break
					}
				}
			}
		}
	}

	// 1. Remove from all conflicting custom categories
	// (If target is custom: remove from other custom categories; if target is builtin: remove from ALL custom categories)
	newCustom := make([]CategoryConfig, len(d.CustomCategories))
	for i, c := range d.CustomCategories {
		if targetIsCustom && c.ID == targetCategoryID {
			newCustom[i] = c
		} else {
			filtered, rem := removeExt(c.Extensions)
			if rem {
				changed = true
			}
			c.Extensions = filtered
			newCustom[i] = c
		}
	}

	// 2. If target is builtin: remove from all other builtin categories.
	// If target is custom: keep builtin categories intact (overlapping rule).
	newBuiltin := make([]CategoryConfig, len(d.BuiltinCategories))
	for i, c := range d.BuiltinCategories {
		if targetIsBuiltin && c.ID != targetCategoryID {
			filtered, rem := removeExt(c.Extensions)
			if rem {
				changed = true
			}
			c.Extensions = filtered
			newBuiltin[i] = c
		} else {
			newBuiltin[i] = c
		}
	}

	// 3. Add to target category if not already present
	if !targetHasExt {
		if targetIsCustom {
			for i := range newCustom {
				if newCustom[i].ID == targetCategoryID {
					newCustom[i].Extensions = append(newCustom[i].Extensions, cleanExt)
					changed = true
					break
				}
			}
		} else {
			for i := range newBuiltin {
				if newBuiltin[i].ID == targetCategoryID {
					newBuiltin[i].Extensions = append(newBuiltin[i].Extensions, cleanExt)
					changed = true
					break
				}
			}
		}
	}

	d.CustomCategories = newCustom
	d.BuiltinCategories = newBuiltin
	return d, changed
}
