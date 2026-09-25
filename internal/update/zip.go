package update

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractZip extracts a zip archive to destDir with ZipSlip path traversal protection.
// Returns the resolved root directory containing the actual extracted files
// (auto-detects if the zip had an extra top-level folder).
func ExtractZip(zipPath, destDir string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to open zip file %s: %w", zipPath, err)
	}
	defer func() { _ = r.Close() }()

	destClean := filepath.Clean(destDir)
	if err := os.MkdirAll(destClean, 0755); err != nil {
		return "", fmt.Errorf("failed to create destination directory: %w", err)
	}

	for _, f := range r.File {
		targetPath := filepath.Join(destClean, f.Name)
		cleanTarget := filepath.Clean(targetPath)

		// ZipSlip protection
		if !strings.HasPrefix(cleanTarget, destClean+string(filepath.Separator)) && cleanTarget != destClean {
			return "", fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, f.Mode()); err != nil {
				return "", fmt.Errorf("failed to create directory %s: %w", cleanTarget, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0755); err != nil {
			return "", fmt.Errorf("failed to create parent directory for %s: %w", cleanTarget, err)
		}

		outFile, err := os.OpenFile(cleanTarget, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return "", fmt.Errorf("failed to open target file %s: %w", cleanTarget, err)
		}

		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close()
			return "", fmt.Errorf("failed to open file inside zip %s: %w", f.Name, err)
		}

		_, err = io.Copy(outFile, rc)
		_ = rc.Close()
		_ = outFile.Close()

		if err != nil {
			return "", fmt.Errorf("failed to write content to %s: %w", cleanTarget, err)
		}
	}

	// Auto-detect if extracted contents are nested in a single top-level directory
	entries, err := os.ReadDir(destClean)
	if err == nil && len(entries) == 1 && entries[0].IsDir() {
		singleSubdir := filepath.Join(destClean, entries[0].Name())
		// If that subdirectory contains SheepGet or manifest.json or extension, use it as root
		return singleSubdir, nil
	}

	return destClean, nil
}
