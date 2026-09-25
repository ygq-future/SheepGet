package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFiles(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "old_downloads")
	dstDir := filepath.Join(tempDir, "new_downloads")

	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create test files and nested folders in srcDir
	testFile1 := filepath.Join(srcDir, "hello.txt")
	if err := os.WriteFile(testFile1, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	subFolder := filepath.Join(srcDir, "Videos")
	if err := os.MkdirAll(subFolder, 0755); err != nil {
		t.Fatal(err)
	}
	testFile2 := filepath.Join(subFolder, "movie.mp4")
	if err := os.WriteFile(testFile2, []byte("mp4 content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Execute migration
	if err := MigrateFiles(srcDir, dstDir); err != nil {
		t.Fatalf("MigrateFiles failed: %v", err)
	}

	// Verify target directory has all migrated items
	dstFile1 := filepath.Join(dstDir, "hello.txt")
	content1, err := os.ReadFile(dstFile1)
	if err != nil || string(content1) != "world" {
		t.Fatalf("expected migrated hello.txt with content 'world', err: %v", err)
	}

	dstFile2 := filepath.Join(dstDir, "Videos", "movie.mp4")
	content2, err := os.ReadFile(dstFile2)
	if err != nil || string(content2) != "mp4 content" {
		t.Fatalf("expected migrated Videos/movie.mp4 with content 'mp4 content', err: %v", err)
	}

	// Verify source files are gone
	if _, err := os.Stat(testFile1); !os.IsNotExist(err) {
		t.Errorf("expected source file1 to be removed")
	}
	if _, err := os.Stat(testFile2); !os.IsNotExist(err) {
		t.Errorf("expected source file2 to be removed")
	}
}
