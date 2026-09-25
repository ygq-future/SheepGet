package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtension_ReadAndUpdate(t *testing.T) {
	tempDir := t.TempDir()
	extDir := filepath.Join(tempDir, "extension")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(extDir, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"version":"1.0.0"}`), 0644); err != nil {
		t.Fatal(err)
	}

	ver, err := ReadExtensionVersion(extDir)
	if err != nil {
		t.Fatalf("ReadExtensionVersion failed: %v", err)
	}
	if ver != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", ver)
	}

	// Prepare updated extension source
	updateSrcDir := filepath.Join(tempDir, "updated_ext")
	if err := os.MkdirAll(updateSrcDir, 0755); err != nil {
		t.Fatal(err)
	}
	newManifest := filepath.Join(updateSrcDir, "manifest.json")
	if err := os.WriteFile(newManifest, []byte(`{"version":"1.1.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	extraFile := filepath.Join(updateSrcDir, "background.js")
	if err := os.WriteFile(extraFile, []byte(`console.log("updated");`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyExtensionUpdate(updateSrcDir, extDir); err != nil {
		t.Fatalf("ApplyExtensionUpdate failed: %v", err)
	}

	updatedVer, err := ReadExtensionVersion(extDir)
	if err != nil {
		t.Fatalf("ReadExtensionVersion after update failed: %v", err)
	}
	if updatedVer != "1.1.0" {
		t.Fatalf("expected updated version 1.1.0, got %s", updatedVer)
	}

	if _, err := os.Stat(filepath.Join(extDir, "background.js")); err != nil {
		t.Fatalf("expected background.js to exist in target ext dir")
	}
}

func TestExtractZip_ZipSlipProtection(t *testing.T) {
	tempDir := t.TempDir()
	zipPath := filepath.Join(tempDir, "test_malicious.zip")

	// Create a zip with path traversal entry
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	_, err = zw.Create("../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	_ = f.Close()

	destDir := filepath.Join(tempDir, "extract_dest")
	_, err = ExtractZip(zipPath, destDir)
	if err == nil {
		t.Fatalf("expected ZipSlip protection to return error, got nil")
	}
}
