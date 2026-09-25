package update

import (
	"strings"
)

// MatchAppAsset finds the best matching release asset for the given platform and mode.
func MatchAppAsset(assets []GitHubAsset, goos, goarch string, isPortable bool) *GitHubAsset {
	normOS := strings.ToLower(goos)
	normArch := strings.ToLower(goarch)
	if normArch == "amd64" {
		normArch = "x64"
	}

	if isPortable {
		// Look for zip packages with "portable" and OS + arch indicators
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if !strings.HasSuffix(name, ".zip") && !strings.HasSuffix(name, ".tar.gz") {
				continue
			}
			if !strings.Contains(name, "portable") {
				continue
			}
			if matchesPlatform(name, normOS, normArch) {
				return &assets[i]
			}
		}
		return nil
	}

	// Installed / Setup package matching
	switch normOS {
	case "windows":
		// Priority 1: NSIS setup installer (.exe)
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.HasSuffix(name, "-setup.exe") || strings.HasSuffix(name, "installer.exe") {
				if matchesArch(name, normArch) {
					return &assets[i]
				}
			}
		}
		// Priority 2: Generic .exe that is not portable
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.HasSuffix(name, ".exe") && !strings.Contains(name, "portable") {
				if matchesArch(name, normArch) {
					return &assets[i]
				}
			}
		}
		// Priority 3: MSI installer (.msi)
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.HasSuffix(name, ".msi") {
				if matchesArch(name, normArch) {
					return &assets[i]
				}
			}
		}

	case "darwin":
		// Priority 1: .dmg
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.HasSuffix(name, ".dmg") {
				return &assets[i]
			}
		}
		// Priority 2: .tar.gz or .zip bundle
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip")) && strings.Contains(name, "darwin") {
				return &assets[i]
			}
		}

	case "linux":
		// Priority 1: .AppImage
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.HasSuffix(name, ".appimage") {
				return &assets[i]
			}
		}
		// Priority 2: .deb
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.HasSuffix(name, ".deb") {
				return &assets[i]
			}
		}
	}

	return nil
}

// MatchExtensionAsset finds the standalone browser extension zip asset.
func MatchExtensionAsset(assets []GitHubAsset) *GitHubAsset {
	for i := range assets {
		name := strings.ToLower(assets[i].Name)
		if strings.HasSuffix(name, ".zip") && strings.Contains(name, "extension") {
			return &assets[i]
		}
	}
	return nil
}

func matchesPlatform(name, normOS, normArch string) bool {
	osMatch := false
	switch normOS {
	case "windows":
		osMatch = strings.Contains(name, "windows") || strings.Contains(name, "win")
	case "darwin":
		osMatch = strings.Contains(name, "darwin") || strings.Contains(name, "macos") || strings.Contains(name, "mac")
	case "linux":
		osMatch = strings.Contains(name, "linux")
	}
	if !osMatch {
		return false
	}
	return matchesArch(name, normArch)
}

func matchesArch(name, normArch string) bool {
	switch normArch {
	case "x64", "amd64":
		return strings.Contains(name, "x64") || strings.Contains(name, "amd64") || strings.Contains(name, "x86_64")
	case "arm64":
		return strings.Contains(name, "arm64") || strings.Contains(name, "aarch64")
	default:
		return true
	}
}
