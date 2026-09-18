package atomicfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"sheep-get/internal/atomicfile"
)

func TestWriteCreatesFileAndMissingDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")

	if err := atomicfile.Write(path, []byte(`{"a":1}`), 0644, false); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != `{"a":1}` {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestWriteReplacesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	if err := atomicfile.Write(path, []byte("new"), 0644, false); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read replaced file: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("expected replaced content, got %s", data)
	}
}

func TestWriteWithSyncReplacesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	if err := atomicfile.Write(path, []byte("synced"), 0644, true); err != nil {
		t.Fatalf("Write with sync failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != "synced" {
		t.Errorf("expected synced content, got %s", data)
	}
}

func TestWriteLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	if err := atomicfile.Write(filepath.Join(dir, "config.json"), []byte("payload"), 0644, true); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Errorf("expected only the target file after a successful write, got %v", entryNames(entries))
	}
}

func TestWriteFailsWhenTargetIsDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "occupied")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("failed to create target directory: %v", err)
	}

	if err := atomicfile.Write(target, []byte("payload"), 0644, false); err == nil {
		t.Fatal("expected an error when the target path is a directory")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "occupied" {
		t.Errorf("a failed write must clean up its temporary file, got %v", entryNames(entries))
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
