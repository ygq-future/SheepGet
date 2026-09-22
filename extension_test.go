package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveExtensionDir(t *testing.T) {
	t.Run("finds extension in exec dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		extDir := filepath.Join(tmpDir, "extension")
		if err := os.MkdirAll(extDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		resolved, err := resolveExtensionDir(tmpDir, t.TempDir())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resolved != extDir {
			t.Errorf("expected %s, got %s", extDir, resolved)
		}
	})

	t.Run("finds extension in cwd dist-extension", func(t *testing.T) {
		execDir := t.TempDir()
		cwdDir := t.TempDir()
		extDir := filepath.Join(cwdDir, "dist-extension", "chrome-mv3")
		if err := os.MkdirAll(extDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		resolved, err := resolveExtensionDir(execDir, cwdDir)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resolved != extDir {
			t.Errorf("expected %s, got %s", extDir, resolved)
		}
	})

	t.Run("returns error when not found", func(t *testing.T) {
		execDir := t.TempDir()
		cwdDir := t.TempDir()
		_, err := resolveExtensionDir(execDir, cwdDir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "manifest.json") {
			t.Errorf("expected error mentioning manifest.json, got: %v", err)
		}
	})
}

func TestPlanExtensionInstall(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		browser     string
		exePath     string
		wantAddress string
		wantExe     string
		wantArgs    []string
	}{
		{
			name:        "chrome on windows uses resolved executable",
			goos:        "windows",
			browser:     "chrome",
			exePath:     `C:\Program Files\Google\Chrome\Application\chrome.exe`,
			wantAddress: "chrome://extensions",
			wantExe:     `C:\Program Files\Google\Chrome\Application\chrome.exe`,
		},
		{
			name:        "edge on windows falls back to shell start",
			goos:        "windows",
			browser:     "Edge",
			wantAddress: "edge://extensions",
			wantExe:     "cmd",
			wantArgs:    []string{"/c", "start", "", "msedge"},
		},
		{
			name:        "chrome on darwin",
			goos:        "darwin",
			browser:     "chrome",
			wantAddress: "chrome://extensions",
			wantExe:     "open",
			wantArgs:    []string{"-a", "Google Chrome"},
		},
		{
			name:        "edge on darwin",
			goos:        "darwin",
			browser:     "edge",
			wantAddress: "edge://extensions",
			wantExe:     "open",
			wantArgs:    []string{"-a", "Microsoft Edge"},
		},
		{
			name:        "chrome on linux",
			goos:        "linux",
			browser:     "chrome",
			wantAddress: "chrome://extensions",
			wantExe:     "google-chrome",
		},
		{
			name:        "edge on linux",
			goos:        "linux",
			browser:     "edge",
			wantAddress: "edge://extensions",
			wantExe:     "microsoft-edge",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planExtensionInstall(tc.goos, tc.browser, tc.exePath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if plan.address != tc.wantAddress {
				t.Errorf("expected address %s, got %s", tc.wantAddress, plan.address)
			}
			if plan.exe != tc.wantExe {
				t.Errorf("expected exe %s, got %s", tc.wantExe, plan.exe)
			}
			if len(plan.args) != len(tc.wantArgs) {
				t.Fatalf("expected args %v, got %v", tc.wantArgs, plan.args)
			}
			for i := range tc.wantArgs {
				if plan.args[i] != tc.wantArgs[i] {
					t.Errorf("expected args %v, got %v", tc.wantArgs, plan.args)
					break
				}
			}
			// 特权地址一旦出现在命令行里就会被浏览器丢弃，因此它只能走剪贴板。
			for _, arg := range plan.args {
				if strings.Contains(arg, "://extensions") {
					t.Errorf("privileged address leaked into command args: %v", plan.args)
				}
			}
		})
	}
}

func TestPlanExtensionInstall_Unsupported(t *testing.T) {
	if _, err := planExtensionInstall("windows", "safari", ""); err == nil {
		t.Fatal("expected error for unsupported browser")
	}
	if _, err := planExtensionInstall("plan9", "chrome", ""); err == nil {
		t.Fatal("expected error for unsupported platform")
	}
}

func TestExtensionDirectory_Live(t *testing.T) {
	dir, err := extensionDirectory()
	if err != nil {
		t.Fatalf("unexpected error in repo environment: %v", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest.json not found in resolved dir %s: %v", dir, err)
	}
}
