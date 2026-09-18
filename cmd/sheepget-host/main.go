// Package main implements the lightweight Native Messaging Host for SheepGet.
// It acts as a silent wake-up springboard for Chrome and Edge extensions (ADR-0005 Decision 2),
// launching the main SheepGet desktop application if it is not already running and returning
// the active local loopback server port and session token over standard input/output.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"sheep-get/internal/server"
	"sheep-get/internal/storage"
)

// HostResponse is the message returned over stdout to the browser extension.
type HostResponse struct {
	Status       string `json:"status"` // "ok" or "error"
	Port         int    `json:"port,omitempty"`
	SessionToken string `json:"sessionToken,omitempty"`
	Running      bool   `json:"running"`
	Message      string `json:"message,omitempty"`
}

// HostRequest is the message received over stdin from the browser extension.
type HostRequest struct {
	Action string `json:"action"` // "query" or "launch"
}

func readMessage(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length == 0 || length > 10*1024*1024 {
		return nil, fmt.Errorf("invalid message length: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeMessage(w io.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	length := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func checkLoopbackAlive(ctx context.Context, port int, token string) bool {
	if port <= 0 || token == "" {
		return false
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/ping", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-SheepGet-Token", token)

	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func readSessionMetadata(sessionFile string) (*server.SessionMetadata, error) {
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		return nil, err
	}
	var meta server.SessionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func findDesktopExecutable() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	execDir := filepath.Dir(execPath)

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}

	candidates := []string{
		filepath.Join(execDir, "sheep-get"+ext),
		filepath.Join(execDir, "quality-app"+ext),
		filepath.Join(execDir, "..", "sheep-get"+ext),
		filepath.Join(execDir, "..", "build", "bin", "quality-app"+ext),
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("main desktop executable not found in %s", execDir)
}

func launchDesktopApp() error {
	exePath, err := findDesktopExecutable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exePath)
	cmd.Dir = filepath.Dir(exePath)
	return cmd.Start()
}

func handleRequest(req *HostRequest) HostResponse {
	storeDir, err := storage.ResolveDataDir("")
	if err != nil {
		return HostResponse{Status: "error", Message: fmt.Sprintf("storage error: %v", err)}
	}

	sessionFile := storeDir.SessionFile()

	// 1. Check if existing session is active
	meta, err := readSessionMetadata(sessionFile)
	if err == nil && meta != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if checkLoopbackAlive(ctx, meta.Port, meta.SessionToken) {
			return HostResponse{
				Status:       "ok",
				Port:         meta.Port,
				SessionToken: meta.SessionToken,
				Running:      true,
			}
		}
	}

	// 2. If action is query only and not running, return inactive status
	if req != nil && req.Action == "query" {
		return HostResponse{
			Status:  "ok",
			Running: false,
			Message: "desktop not running",
		}
	}

	// 3. Launch main program in background
	if err := launchDesktopApp(); err != nil {
		return HostResponse{
			Status:  "error",
			Message: fmt.Sprintf("failed to launch desktop app: %v", err),
		}
	}

	// 4. Poll for session file creation and loopback server readiness (up to 4s)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(150 * time.Millisecond)
		meta, err := readSessionMetadata(sessionFile)
		if err == nil && meta != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			alive := checkLoopbackAlive(ctx, meta.Port, meta.SessionToken)
			cancel()
			if alive {
				return HostResponse{
					Status:       "ok",
					Port:         meta.Port,
					SessionToken: meta.SessionToken,
					Running:      true,
				}
			}
		}
	}

	return HostResponse{
		Status:  "error",
		Message: "timed out waiting for desktop app to start loopback server",
	}
}

func main() {
	raw, err := readMessage(os.Stdin)
	if err != nil {
		_ = writeMessage(os.Stdout, HostResponse{
			Status:  "error",
			Message: fmt.Sprintf("failed to read native message: %v", err),
		})
		return
	}

	var req HostRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}

	resp := handleRequest(&req)
	_ = writeMessage(os.Stdout, resp)
}
