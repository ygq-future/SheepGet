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

func TestStorage_PhysicalIsolation(t *testing.T) {
	// Verify portable mode operations remain strictly in ./data and never touch standard user dir
	portableAppDir := t.TempDir()
	portableMarker := filepath.Join(portableAppDir, "portable")
	if err := os.WriteFile(portableMarker, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create portable marker: %v", err)
	}

	portableStorage, err := ResolveDataDir(portableAppDir)
	if err != nil {
		t.Fatalf("failed to resolve portable storage: %v", err)
	}
	if portableStorage.Mode != ModePortable {
		t.Fatalf("expected ModePortable, got %v", portableStorage.Mode)
	}

	standardDir, err := getStandardUserDir()
	if err != nil {
		t.Fatalf("failed to get standard dir: %v", err)
	}
	if portableStorage.DataDir == standardDir {
		t.Fatalf("portable DataDir must NOT match standard user dir %q", standardDir)
	}
	if !filepath.IsLocal(filepath.Clean(portableStorage.DataDir)) && filepath.Dir(portableStorage.DataDir) != portableAppDir {
		t.Fatalf("portable DataDir %q must reside directly under app dir %q", portableStorage.DataDir, portableAppDir)
	}

	// Verify writing to portable storage paths stays strictly within portableAppDir
	testConfig := portableStorage.ConfigFile()
	if err := os.WriteFile(testConfig, []byte(`{"test":true}`), 0644); err != nil {
		t.Fatalf("failed to write test config in portable dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(portableAppDir, "data", "config.json")); err != nil {
		t.Fatalf("expected config.json strictly inside portable data dir: %v", err)
	}

	// Verify installed mode resolution does not touch portableAppDir
	cleanAppDir := t.TempDir()
	installedStorage, err := ResolveDataDir(cleanAppDir)
	if err != nil {
		t.Fatalf("failed to resolve installed storage: %v", err)
	}
	if installedStorage.Mode != ModeInstalled {
		t.Fatalf("expected ModeInstalled, got %v", installedStorage.Mode)
	}
	if _, err := os.Stat(filepath.Join(cleanAppDir, "data")); !os.IsNotExist(err) {
		t.Fatalf("installed mode must never create data/ inside application directory")
	}
}
