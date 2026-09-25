package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MigrateFiles moves all files and folders from srcDir to dstDir.
// It prioritizes atomic filesystem renaming (os.Rename) for instant near-zero latency
// moves on the same drive/volume, and automatically falls back to streaming copy + delete
// across different storage volumes or drives.
func MigrateFiles(srcDir, dstDir string) error {
	srcClean := filepath.Clean(srcDir)
	dstClean := filepath.Clean(dstDir)

	if strings.EqualFold(srcClean, dstClean) {
		return nil
	}

	srcInfo, err := os.Stat(srcClean)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Source directory doesn't exist, nothing to migrate
		}
		return fmt.Errorf("failed to inspect source directory %s: %w", srcClean, err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", srcClean)
	}

	if err := os.MkdirAll(dstClean, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory %s: %w", dstClean, err)
	}

	entries, err := os.ReadDir(srcClean)
	if err != nil {
		return fmt.Errorf("failed to read source directory %s: %w", srcClean, err)
	}

	for _, entry := range entries {
		srcItem := filepath.Join(srcClean, entry.Name())
		dstItem := filepath.Join(dstClean, entry.Name())

		if err := moveItem(srcItem, dstItem); err != nil {
			return fmt.Errorf("failed to move %s to %s: %w", srcItem, dstItem, err)
		}
	}

	return nil
}

func moveItem(src, dst string) error {
	// 1. Try atomic rename first (Windows same-drive/NTFS MFT instant pointer move)
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// 2. If destination already exists, handle collision / directory merging
	srcStat, statErr := os.Stat(src)
	if statErr != nil {
		return statErr
	}

	if srcStat.IsDir() {
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		subEntries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, sub := range subEntries {
			if err := moveItem(filepath.Join(src, sub.Name()), filepath.Join(dst, sub.Name())); err != nil {
				return err
			}
		}
		_ = os.Remove(src)
		return nil
	}

	// For files across different drives/volumes: fallback to copy + delete
	if err := copyAndRemoveFile(src, dst); err != nil {
		return err
	}
	return nil
}

func copyAndRemoveFile(src, dst string) error {
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

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = in.Close()

	return os.Remove(src)
}
