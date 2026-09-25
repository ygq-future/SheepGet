package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratePortableUpdateScript(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "test_update_script")

	err := GeneratePortableUpdateScript(scriptPath, "C:\\src", "C:\\dest", "SheepGet.exe", 1234)
	if err != nil {
		t.Fatalf("GeneratePortableUpdateScript failed: %v", err)
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read generated script: %v", err)
	}

	str := string(content)
	// Must verify PID is embedded
	if !strings.Contains(str, "1234") {
		t.Errorf("expected script to contain PID 1234")
	}
	// Must verify "data" directory is excluded
	if !strings.Contains(str, "data") {
		t.Errorf("expected script to contain data exclusion rule")
	}
	// Must verify executable name is present
	if !strings.Contains(str, "SheepGet.exe") {
		t.Errorf("expected script to contain SheepGet.exe")
	}
}
