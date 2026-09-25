//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// GeneratePortableUpdateScript creates a Windows batch script for upgrading portable SheepGet.
func GeneratePortableUpdateScript(scriptPath, extractedDir, appDir, exeName string, pid int) error {
	batContent := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion

set PID=%d
set SRC=%s
set DEST=%s
set EXE=%s

:: 1. Wait for parent process (PID) to fully exit (up to 30 seconds)
set COUNT=0
:WAIT_PID
tasklist /FI "PID eq %%PID%%" 2>NUL | find /I "%%PID%%" >NUL
if not errorlevel 1 (
    set /a COUNT+=1
    if !COUNT! geq 30 goto FORCE_KILL
    timeout /t 1 /nobreak >NUL
    goto WAIT_PID
)
goto APPLY_UPDATE

:FORCE_KILL
taskkill /F /PID %%PID%% >NUL 2>NUL
timeout /t 1 /nobreak >NUL

:APPLY_UPDATE
:: Brief delay for OS file locks to release
timeout /t 1 /nobreak >NUL

:: 2. Robocopy all updated files while strictly EXCLUDING the "data" directory
robocopy "%%SRC%%" "%%DEST%%" /E /XD "data" /NFL /NDL /NJH /NJS /nc /ns /np >NUL 2>NUL
if errorlevel 8 (
    :: Fallback to copy & xcopy if robocopy fails
    copy /y "%%SRC%%\%%EXE%%" "%%DEST%%\%%EXE%%" >NUL 2>NUL
    if exist "%%SRC%%\extension" (
        xcopy /y /e /i "%%SRC%%\extension" "%%DEST%%\extension" >NUL 2>NUL
    )
)

:: 3. Launch the updated application
cd /d "%%DEST%%"
start "" "%%DEST%%\%%EXE%%"

:: 4. Clean up temporary extracted folder and this updater script
rd /s /q "%%SRC%%" 2>NUL
(goto) 2>nul & del "%%~f0"
`, pid, extractedDir, appDir, exeName)

	return os.WriteFile(scriptPath, []byte(batContent), 0755)
}

// LaunchPortableUpdater creates the helper script, launches it in a detached process, and prepares to exit.
func LaunchPortableUpdater(extractedDir, appDir, exeName string) error {
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, fmt.Sprintf("sheepget_update_%d.bat", os.Getpid()))

	if err := GeneratePortableUpdateScript(scriptPath, extractedDir, appDir, exeName, os.Getpid()); err != nil {
		return fmt.Errorf("failed to generate portable update script: %w", err)
	}

	cmd := exec.Command("cmd.exe", "/c", "start", "/min", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch portable update script: %w", err)
	}

	return nil
}
