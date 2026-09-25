package update

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type extensionManifest struct {
	Version string `json:"version"`
}

// ReadExtensionVersion reads the version string from manifest.json inside the specified extension directory.
func ReadExtensionVersion(extDir string) (string, error) {
	manifestPath := filepath.Join(extDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("failed to read extension manifest at %s: %w", manifestPath, err)
	}

	var m extensionManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("failed to parse extension manifest: %w", err)
	}

	if m.Version == "" {
		return "", fmt.Errorf("manifest.json missing version field")
	}

	return m.Version, nil
}

// ApplyExtensionUpdate copies all files from extractedDir to targetExtDir, overwriting existing files.
func ApplyExtensionUpdate(extractedDir, targetExtDir string) error {
	if _, err := os.Stat(extractedDir); err != nil {
		return fmt.Errorf("extracted extension directory does not exist: %w", err)
	}

	if err := os.MkdirAll(targetExtDir, 0755); err != nil {
		return fmt.Errorf("failed to create target extension directory: %w", err)
	}

	return copyDir(extractedDir, targetExtDir)
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read directory %s failed: %w", src, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return fmt.Errorf("create dir %s failed: %w", dstPath, err)
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
