//go:build !windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// GeneratePortableUpdateScript creates a shell script for upgrading portable SheepGet on Unix-like OSes.
func GeneratePortableUpdateScript(scriptPath, extractedDir, appDir, exeName string, pid int) error {
	shContent := fmt.Sprintf(`#!/bin/sh

PID=%d
SRC="%s"
DEST="%s"
EXE="%s"

# 1. Wait for process to exit
while kill -0 $PID 2>/dev/null; do
    sleep 0.5
done

sleep 0.5

# 2. Copy files excluding "data" directory
for item in "$SRC"/*; do
    name=$(basename "$item")
    if [ "$name" != "data" ]; then
        cp -R "$item" "$DEST/"
    fi
done

chmod +x "$DEST/$EXE"

# 3. Launch updated application in background
(
    cd "$DEST" && "$DEST/$EXE" &
)

# 4. Clean up temporary directory and script
rm -rf "$SRC" "$0"
`, pid, extractedDir, appDir, exeName)

	return os.WriteFile(scriptPath, []byte(shContent), 0755)
}

// LaunchPortableUpdater creates the helper script, launches it in a detached process, and prepares to exit.
func LaunchPortableUpdater(extractedDir, appDir, exeName string) error {
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, fmt.Sprintf("sheepget_update_%d.sh", os.Getpid()))

	if err := GeneratePortableUpdateScript(scriptPath, extractedDir, appDir, exeName, os.Getpid()); err != nil {
		return fmt.Errorf("failed to generate portable update script: %w", err)
	}

	cmd := exec.Command("/bin/sh", scriptPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch portable update script: %w", err)
	}

	return nil
}
