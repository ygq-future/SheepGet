//go:build windows

package sys

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

var startupApprovedKeyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`

func cleanStartupApprovedAt(root registry.Key, subKey string, identifiers ...string) error {
	key, err := registry.OpenKey(root, subKey, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() { _ = key.Close() }()

	var lastErr error
	for _, id := range identifiers {
		if id == "" {
			continue
		}
		if delErr := key.DeleteValue(id); delErr != nil && !errors.Is(delErr, registry.ErrNotExist) {
			lastErr = delErr
		}
	}
	return lastErr
}

// CleanStartupApproved clears any disabled/override state recorded by Windows Explorer or
// Windows Settings under HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run.
//
// In Windows 10/11, when an app is disabled in Task Manager or Windows Settings, Windows creates
// an entry in StartupApproved\Run with a binary value starting with 01 or 03. When autostart is
// re-enabled in application settings, Wails v3 only writes to CurrentVersion\Run and leaves
// StartupApproved\Run untouched, causing Windows to continue blocking autostart on user login
// and displaying the toggle as "Off" in Windows Settings. Deleting the value restores the default
// enabled state.
func CleanStartupApproved(identifiers ...string) error {
	return cleanStartupApprovedAt(registry.CURRENT_USER, startupApprovedKeyPath, identifiers...)
}
