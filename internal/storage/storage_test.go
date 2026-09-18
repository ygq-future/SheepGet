package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDataDir_PortableModeWithDataDir(t *testing.T) {
	tempDir := t.TempDir()
	dataSubDir := filepath.Join(tempDir, "data")
	if err := os.Mkdir(dataSubDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	res, err := ResolveDataDir(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Mode != ModePortable {
		t.Errorf("expected ModePortable, got %v", res.Mode)
	}
	expectedPath := filepath.Join(tempDir, "data")
	if res.DataDir != expectedPath {
		t.Errorf("expected DataDir %q, got %q", expectedPath, res.DataDir)
	}
}

func TestResolveDataDir_PortableModeWithPortableFile(t *testing.T) {
	tempDir := t.TempDir()
	portableFile := filepath.Join(tempDir, "portable")
	if err := os.WriteFile(portableFile, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create portable file: %v", err)
	}

	res, err := ResolveDataDir(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Mode != ModePortable {
		t.Errorf("expected ModePortable, got %v", res.Mode)
	}
	expectedPath := filepath.Join(tempDir, "data")
	if res.DataDir != expectedPath {
		t.Errorf("expected DataDir %q, got %q", expectedPath, res.DataDir)
	}
	if info, err := os.Stat(expectedPath); err != nil || !info.IsDir() {
		t.Errorf("expected data dir to be automatically created: %v", err)
	}
}

func TestResolveDataDir_InstalledMode(t *testing.T) {
	tempDir := t.TempDir()

	res, err := ResolveDataDir(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Mode != ModeInstalled {
		t.Errorf("expected ModeInstalled, got %v", res.Mode)
	}
	if res.DataDir == "" {
		t.Errorf("expected non-empty DataDir in installed mode")
	}
	// Verify directory layout subdirs can be retrieved
	if res.ConfigFile() != filepath.Join(res.DataDir, "config.json") {
		t.Errorf("unexpected ConfigFile path: %s", res.ConfigFile())
	}
	if res.TasksDB() != filepath.Join(res.DataDir, "tasks.json") {
		t.Errorf("unexpected TasksDB path: %s", res.TasksDB())
	}
	if res.TempDir() != filepath.Join(res.DataDir, "temp") {
		t.Errorf("unexpected TempDir path: %s", res.TempDir())
	}
	if res.LogsDir() != filepath.Join(res.DataDir, "logs") {
		t.Errorf("unexpected LogsDir path: %s", res.LogsDir())
	}
	if res.SessionFile() != filepath.Join(res.DataDir, "session.json") {
		t.Errorf("unexpected SessionFile path: %s", res.SessionFile())
	}
}
