// Package storage handles portable vs installed storage mode resolution and directory layout.
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Mode defines the storage mode: portable or installed.
type Mode string

const (
	ModePortable  Mode = "portable"
	ModeInstalled Mode = "installed"
)

// Storage represents the resolved storage directory and configuration.
type Storage struct {
	Mode    Mode   `json:"mode"`
	DataDir string `json:"dataDir"`
}

// ConfigFile returns the path to config.json
func (s *Storage) ConfigFile() string {
	return filepath.Join(s.DataDir, "config.json")
}

// TasksDB returns the path to tasks.json (metadata persistence)
func (s *Storage) TasksDB() string {
	return filepath.Join(s.DataDir, "tasks.json")
}

// TempDir returns the path to default temp directory
func (s *Storage) TempDir() string {
	return filepath.Join(s.DataDir, "temp")
}

// LogsDir returns the path to logs directory
func (s *Storage) LogsDir() string {
	return filepath.Join(s.DataDir, "logs")
}

// SessionFile returns the path to session.json (runtime metadata for the active loopback server)
func (s *Storage) SessionFile() string {
	return filepath.Join(s.DataDir, "session.json")
}

// ResolveDataDir checks whether portable mode applies according to ADR-0003,
// and returns the initialized Storage struct.
// execDir: the directory containing the executable (or passed explicitly for testing).
func ResolveDataDir(execDir string) (*Storage, error) {
	if execDir == "" {
		exePath, err := os.Executable()
		if err != nil {
			execDir = "."
		} else {
			execDir = filepath.Dir(exePath)
		}
	}

	portableMarker := filepath.Join(execDir, "portable")
	dataSubdir := filepath.Join(execDir, "data")

	isPortable := false
	if _, err := os.Stat(portableMarker); err == nil {
		isPortable = true
	} else if info, err := os.Stat(dataSubdir); err == nil && info.IsDir() {
		isPortable = true
	}

	var dataDir string
	var mode Mode

	if isPortable {
		mode = ModePortable
		dataDir = dataSubdir
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("portable mode: failed to create data dir at %s: %w", dataDir, err)
		}
		// Verify writability in portable mode per ADR-0003
		probeFile := filepath.Join(dataDir, ".probe_write")
		if err := os.WriteFile(probeFile, []byte("ok"), 0644); err != nil {
			return nil, fmt.Errorf("portable mode: data dir %s is read-only or not writable: %w", dataDir, err)
		}
		_ = os.Remove(probeFile)
	} else {
		mode = ModeInstalled
		standardDir, err := getStandardUserDir()
		if err != nil {
			return nil, fmt.Errorf("installed mode: failed to resolve standard user dir: %w", err)
		}
		dataDir = standardDir
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("installed mode: failed to create data dir at %s: %w", dataDir, err)
		}
	}

	// Ensure subdirectories exist
	_ = os.MkdirAll(filepath.Join(dataDir, "temp"), 0755)
	_ = os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)

	return &Storage{
		Mode:    mode,
		DataDir: dataDir,
	}, nil
}

func getStandardUserDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			userConfigDir, err := os.UserConfigDir()
			if err != nil {
				return "", err
			}
			return filepath.Join(userConfigDir, "SheepGet"), nil
		}
		return filepath.Join(appData, "SheepGet"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "SheepGet"), nil
	default:
		// Linux / other Unix
		xdgData := os.Getenv("XDG_DATA_HOME")
		if xdgData != "" {
			return filepath.Join(xdgData, "SheepGet"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "SheepGet"), nil
	}
}
