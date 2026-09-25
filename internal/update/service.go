package update

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"sheep-get/internal/browser"
	"sheep-get/internal/storage"
	"sheep-get/internal/version"
)

// Service provides a high-level facade for application and extension updating.
type Service struct {
	mu           sync.RWMutex
	checker      *Checker
	downloader   *Downloader
	storage      *storage.Storage
	proxyMode    string
	customProxy  string
	lastCheckApp *AppUpdateResult
	lastCheckExt *ExtensionUpdateResult
}

// NewService creates a new update Service.
func NewService(st *storage.Storage, checker *Checker) *Service {
	if checker == nil {
		checker = NewChecker()
	}
	return &Service{
		checker:    checker,
		downloader: NewDownloader(nil),
		storage:    st,
	}
}

// SetProxy updates the proxy settings used for update checks and downloads.
func (s *Service) SetProxy(mode, customAddr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.proxyMode = mode
	s.customProxy = customAddr

	if err := s.checker.ConfigureProxy(mode, customAddr); err != nil {
		return err
	}

	transport, err := createTransport(mode, customAddr)
	if err != nil {
		return err
	}
	s.downloader = NewDownloader(&http.Client{
		Transport: transport,
	})
	return nil
}

// CheckAppUpdate checks for desktop application updates.
func (s *Service) CheckAppUpdate(ctx context.Context) (*AppUpdateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	isPortable := s.storage != nil && s.storage.Mode == storage.ModePortable
	res, err := s.checker.CheckAppUpdate(ctx, version.Version, runtime.GOOS, runtime.GOARCH, isPortable)
	if err != nil {
		return nil, err
	}
	s.lastCheckApp = res
	return res, nil
}

// GetInstalledExtensionVersion returns the version of the currently bundled browser extension.
func (s *Service) GetInstalledExtensionVersion() (string, error) {
	extDir, err := browser.ExtensionDirectory()
	if err != nil {
		return "", err
	}
	return ReadExtensionVersion(extDir)
}

// CheckExtensionUpdate checks for updates to the browser extension.
func (s *Service) CheckExtensionUpdate(ctx context.Context) (*ExtensionUpdateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	extVer, err := s.GetInstalledExtensionVersion()
	if err != nil {
		// Fallback to app version if extension manifest cannot be read
		extVer = version.Version
	}

	res, err := s.checker.CheckExtensionUpdate(ctx, extVer)
	if err != nil {
		return nil, err
	}
	s.lastCheckExt = res
	return res, nil
}

// DownloadAppUpdate downloads the app update asset to a temporary file.
func (s *Service) DownloadAppUpdate(ctx context.Context, assetURL string, onProgress ProgressFunc) (string, error) {
	s.mu.RLock()
	dl := s.downloader
	s.mu.RUnlock()

	tempDir := os.TempDir()
	if s.storage != nil && s.storage.TempDir() != "" {
		tempDir = s.storage.TempDir()
	}

	// Extract filename from URL
	parts := strings.Split(assetURL, "/")
	fileName := "sheepget_update_package"
	if len(parts) > 0 {
		fileName = parts[len(parts)-1]
	}
	targetPath := filepath.Join(tempDir, "updates", fileName)

	if err := dl.Download(ctx, assetURL, targetPath, onProgress); err != nil {
		return "", err
	}
	return targetPath, nil
}

// ApplyAppUpdate applies the downloaded app update.
// For portable mode: extracts the zip and launches the detached updater script.
// For setup mode: launches the installer executable.
func (s *Service) ApplyAppUpdate(downloadedPath string) error {
	s.mu.RLock()
	isPortable := s.storage != nil && s.storage.Mode == storage.ModePortable
	s.mu.RUnlock()

	if isPortable {
		// 1. Extract zip to temp directory
		extractDir := filepath.Join(filepath.Dir(downloadedPath), "extracted")
		_ = os.RemoveAll(extractDir)
		resolvedRoot, err := ExtractZip(downloadedPath, extractDir)
		if err != nil {
			return fmt.Errorf("failed to extract portable zip: %w", err)
		}

		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to determine executable path: %w", err)
		}
		appDir := filepath.Dir(exePath)
		exeName := filepath.Base(exePath)

		// 2. Launch portable updater script
		if err := LaunchPortableUpdater(resolvedRoot, appDir, exeName); err != nil {
			return fmt.Errorf("failed to launch portable updater: %w", err)
		}
		return nil
	}

	// Installed mode: launch installer
	if err := LaunchInstaller(downloadedPath); err != nil {
		return fmt.Errorf("failed to launch installer: %w", err)
	}
	return nil
}

// UpdateExtension downloads and extracts the extension package, updating the local extension directory.
func (s *Service) UpdateExtension(ctx context.Context, assetURL string, onProgress ProgressFunc) error {
	s.mu.RLock()
	dl := s.downloader
	s.mu.RUnlock()

	targetExtDir, err := browser.ExtensionDirectory()
	if err != nil {
		return fmt.Errorf("cannot locate browser extension directory: %w", err)
	}

	tempDir := os.TempDir()
	if s.storage != nil && s.storage.TempDir() != "" {
		tempDir = s.storage.TempDir()
	}

	extZipPath := filepath.Join(tempDir, "updates", "extension_update.zip")
	if err := dl.Download(ctx, assetURL, extZipPath, onProgress); err != nil {
		return fmt.Errorf("failed to download extension package: %w", err)
	}
	defer func() { _ = os.Remove(extZipPath) }()

	extractDir := filepath.Join(tempDir, "updates", "extension_extracted")
	_ = os.RemoveAll(extractDir)
	defer func() { _ = os.RemoveAll(extractDir) }()

	resolvedRoot, err := ExtractZip(extZipPath, extractDir)
	if err != nil {
		return fmt.Errorf("failed to extract extension package: %w", err)
	}

	if err := ApplyExtensionUpdate(resolvedRoot, targetExtDir); err != nil {
		return fmt.Errorf("failed to apply extension update: %w", err)
	}

	return nil
}
