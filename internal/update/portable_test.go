package update

import (
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		if !strings.Contains(str, "exit") {
			t.Errorf("expected Windows update script to contain explicit exit")
		}
		if !strings.Contains(str, "chcp 65001") {
			t.Errorf("expected Windows update script to set UTF-8 code page (chcp 65001)")
		}
		// Windows batch script must strictly use CRLF line endings
		if !strings.Contains(str, "\r\n") {
			t.Errorf("expected Windows update script to contain CRLF line endings")
		}
		normalized := strings.ReplaceAll(str, "\r\n", "")
		if strings.Contains(normalized, "\n") {
			t.Errorf("found bare LF line endings in Windows update script")
		}
	}
}

func TestGeneratePortableUpdateScript_ChinesePath(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "test_update_script_cn")

	cnSrc := "D:\\测试下载\\临时目录"
	cnDest := "D:\\软件工具\\SheepGet便携版"
	err := GeneratePortableUpdateScript(scriptPath, cnSrc, cnDest, "SheepGet.exe", 9999)
	if err != nil {
		t.Fatalf("GeneratePortableUpdateScript failed: %v", err)
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read generated script: %v", err)
	}

	str := string(content)
	if !strings.Contains(str, cnSrc) {
		t.Errorf("expected script to contain Chinese source path: %s", cnSrc)
	}
	if !strings.Contains(str, cnDest) {
		t.Errorf("expected script to contain Chinese destination path: %s", cnDest)
	}
}
