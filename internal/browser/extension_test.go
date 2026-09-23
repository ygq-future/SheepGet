package browser

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

		resolved, err := ResolveExtensionDir(tmpDir, t.TempDir())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resolved != extDir {
			t.Errorf("expected %s, got %s", extDir, resolved)
		}
	})

	t.Run("finds extension in cwd build dist-extension", func(t *testing.T) {
		execDir := t.TempDir()
		cwdDir := t.TempDir()
		extDir := filepath.Join(cwdDir, "build", "dist-extension", "chrome-mv3")
		if err := os.MkdirAll(extDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		resolved, err := ResolveExtensionDir(execDir, cwdDir)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resolved != extDir {
			t.Errorf("expected %s, got %s", extDir, resolved)
		}
	})

	t.Run("finds extension in cwd dist-extension legacy", func(t *testing.T) {
		execDir := t.TempDir()
		cwdDir := t.TempDir()
		extDir := filepath.Join(cwdDir, "dist-extension", "chrome-mv3")
		if err := os.MkdirAll(extDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		resolved, err := ResolveExtensionDir(execDir, cwdDir)
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
		_, err := ResolveExtensionDir(execDir, cwdDir)
		if err == nil {
			t.Fatal("expected error when extension not found")
		}
		if !strings.Contains(err.Error(), "未在候选路径中检索到") {
			t.Errorf("unexpected error message: %v", err)
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
			name:        "windows chrome with exe",
			goos:        "windows",
			browser:     "chrome",
			exePath:     `C:\Program Files\Google\Chrome\Application\chrome.exe`,
			wantAddress: "chrome://extensions",
			wantExe:     `C:\Program Files\Google\Chrome\Application\chrome.exe`,
			wantArgs:    nil,
		},
		{
			name:        "windows chrome fallback",
			goos:        "windows",
			browser:     "chrome",
			exePath:     "",
			wantAddress: "chrome://extensions",
			wantExe:     "cmd",
			wantArgs:    []string{"/c", "start", "", "chrome"},
		},
		{
			name:        "windows edge fallback",
			goos:        "windows",
			browser:     "edge",
			exePath:     "",
			wantAddress: "edge://extensions",
			wantExe:     "cmd",
			wantArgs:    []string{"/c", "start", "", "msedge"},
		},
		{
			name:        "darwin chrome",
			goos:        "darwin",
			browser:     "chrome",
			exePath:     "",
			wantAddress: "chrome://extensions",
			wantExe:     "open",
			wantArgs:    []string{"-a", "Google Chrome"},
		},
		{
			name:        "darwin edge",
			goos:        "darwin",
			browser:     "edge",
			exePath:     "",
			wantAddress: "edge://extensions",
			wantExe:     "open",
			wantArgs:    []string{"-a", "Microsoft Edge"},
		},
		{
			name:        "linux chrome",
			goos:        "linux",
			browser:     "chrome",
			exePath:     "",
			wantAddress: "chrome://extensions",
			wantExe:     "google-chrome",
			wantArgs:    nil,
		},
		{
			name:        "linux edge",
			goos:        "linux",
			browser:     "edge",
			exePath:     "",
			wantAddress: "edge://extensions",
			wantExe:     "microsoft-edge",
			wantArgs:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanExtensionInstall(tc.goos, tc.browser, tc.exePath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if plan.Address != tc.wantAddress {
				t.Errorf("address: want %s, got %s", tc.wantAddress, plan.Address)
			}
			if plan.Exe != tc.wantExe {
				t.Errorf("exe: want %s, got %s", tc.wantExe, plan.Exe)
			}
			if len(plan.Args) != len(tc.wantArgs) {
				t.Fatalf("args len: want %d, got %d", len(tc.wantArgs), len(plan.Args))
			}
			for i := range plan.Args {
				if plan.Args[i] != tc.wantArgs[i] {
					t.Errorf("args[%d]: want %s, got %s", i, tc.wantArgs[i], plan.Args[i])
				}
			}
		})
	}
}

func TestPlanExtensionInstall_Unsupported(t *testing.T) {
	if _, err := PlanExtensionInstall("windows", "safari", ""); err == nil {
		t.Fatal("expected error for unsupported browser")
	}
	if _, err := PlanExtensionInstall("plan9", "chrome", ""); err == nil {
		t.Fatal("expected error for unsupported platform")
	}
}
