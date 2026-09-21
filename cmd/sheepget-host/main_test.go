package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"sheep-get/internal/server"
)

func TestNativeMessaging_ReadWriteMessage(t *testing.T) {
	req := HostRequest{Action: "launch"}
	var buf bytes.Buffer

	// Test write
	if err := writeMessage(&buf, req); err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	// Verify 4-byte header
	raw := buf.Bytes()
	if len(raw) < 4 {
		t.Fatalf("output too short: %d bytes", len(raw))
	}
	var length uint32
	_ = binary.Read(bytes.NewReader(raw[:4]), binary.LittleEndian, &length)
	if int(length) != len(raw)-4 {
		t.Errorf("length prefix mismatch: header said %d, payload was %d", length, len(raw)-4)
	}

	// Test read
	readData, err := readMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}
	var decoded HostRequest
	if err := json.Unmarshal(readData, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if decoded.Action != "launch" {
		t.Errorf("expected action 'launch', got %q", decoded.Action)
	}
}

func TestCheckLoopbackAlive(t *testing.T) {
	token := "test_token_123"

	// Mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/ping" && r.Header.Get("X-SheepGet-Token") == token {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port

	ctx := context.Background()

	// Valid token & port -> true
	if !checkLoopbackAlive(ctx, port, token) {
		t.Errorf("expected checkLoopbackAlive to be true for valid token and port")
	}

	// Invalid token -> false
	if checkLoopbackAlive(ctx, port, "wrong_token") {
		t.Errorf("expected checkLoopbackAlive to be false for wrong token")
	}

	// Inactive port -> false
	if checkLoopbackAlive(ctx, 1, token) {
		t.Errorf("expected checkLoopbackAlive to be false for inactive port")
	}
}

func TestReadSessionMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.json")

	meta := server.SessionMetadata{
		Port:         54321,
		SessionToken: "test_token_xyz",
		PID:          1234,
		StartedAt:    1742000000,
	}
	data, _ := json.Marshal(meta)
	_ = os.WriteFile(sessionFile, data, 0600)

	loaded, err := readSessionMetadata(sessionFile)
	if err != nil {
		t.Fatalf("readSessionMetadata failed: %v", err)
	}
	if loaded.Port != 54321 || loaded.SessionToken != "test_token_xyz" {
		t.Errorf("unexpected session metadata: %+v", loaded)
	}
}

func TestFindDesktopExecutableInDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Case 1: none exists -> error
	if _, err := findDesktopExecutableInDir(tmpDir); err == nil {
		t.Errorf("expected error when no executable exists")
	}

	// Case 2: sheep-get binary exists
	ext := ""
	if os.PathSeparator == '\\' {
		ext = ".exe"
	}
	target := filepath.Join(tmpDir, "sheep-get"+ext)
	if err := os.WriteFile(target, []byte("binary"), 0755); err != nil {
		t.Fatalf("failed to create dummy binary: %v", err)
	}

	found, err := findDesktopExecutableInDir(tmpDir)
	if err != nil {
		t.Fatalf("expected to find executable, got: %v", err)
	}
	if found != target {
		t.Errorf("expected %q, got %q", target, found)
	}
}
