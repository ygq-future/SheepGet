package update

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// LaunchInstaller launches the setup installer executable using the OS desktop shell.
func LaunchInstaller(installerPath string) error {
	if _, err := os.Stat(installerPath); err != nil {
		return fmt.Errorf("installer file not found: %w", err)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd.exe", "/c", "start", "", installerPath)
	case "darwin":
		cmd = exec.Command("open", installerPath)
	case "linux":
		cmd = exec.Command("xdg-open", installerPath)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch installer: %w", err)
	}

	return nil
}
