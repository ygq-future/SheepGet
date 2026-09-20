package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeTransferCrossDevice_Success(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "source.bin")
	dstFile := filepath.Join(dstDir, "target.bin")

	content := []byte("hello cross device transfer content")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	if err := safeTransferCrossDevice(srcFile, dstFile); err != nil {
		t.Fatalf("safeTransferCrossDevice failed: %v", err)
	}

	// Destination must exist and have identical content
	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("failed to read destination: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}

	// Source file must be cleaned up on success
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatalf("source file was not removed after successful transfer")
	}
}

func TestSafeTransferCrossDevice_PreservesSourceOnFailure(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "source.bin")
	content := []byte("precious partial download data that must be preserved")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	// Destination path pointing to an impossible location (e.g. file acting as directory)
	blockingFile := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blockingFile, []byte("blocker"), 0644); err != nil {
		t.Fatalf("failed to create blocker: %v", err)
	}
	impossibleDst := filepath.Join(blockingFile, "impossible", "target.bin")

	err := safeTransferCrossDevice(srcFile, impossibleDst)
	if err == nil {
		t.Fatalf("expected transfer to fail on impossible destination path")
	}

	// Source file MUST be preserved on failure!
	got, readErr := os.ReadFile(srcFile)
	if readErr != nil {
		t.Fatalf("source file was lost on failure! %v", readErr)
	}
	if string(got) != string(content) {
		t.Fatalf("source file was corrupted: got %q, want %q", got, content)
	}
}
