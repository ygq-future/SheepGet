package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"sheep-get/internal/config"
)

type mockDownloadHandler struct {
	handledReq *HandoverRequest
	resp       *HandoverResponse
	err        error
}

func (m *mockDownloadHandler) HandleHandover(_ context.Context, req *HandoverRequest) (*HandoverResponse, error) {
	m.handledReq = req
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

type mockConfigProvider struct {
	cfg config.TakeoverConfig
}

func (m *mockConfigProvider) GetTakeoverConfig() config.TakeoverConfig {
	return m.cfg
}

func TestServer_LifecycleAndEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")

	handler := &mockDownloadHandler{
		resp: &HandoverResponse{
			Accepted:    true,
			QueueItemID: "test_queue_item_123",
		},
	}
	cfgProvider := &mockConfigProvider{
		cfg: config.TakeoverConfig{
			Extensions:    []string{"zip", "rar"},
			ExcludedSites: []string{"example.com"},
			PauseShortcut: "Delete",
			ForceShortcut: "Insert",
		},
	}

	srv := NewServer(sessionPath, handler, cfgProvider)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// 1. Check session file
	sessionData, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("failed to read session file: %v", err)
	}
	var meta SessionMetadata
	if err := json.Unmarshal(sessionData, &meta); err != nil {
		t.Fatalf("failed to parse session json: %v", err)
	}
	if meta.Port != srv.Port() || meta.SessionToken != srv.SessionToken() {
		t.Errorf("session metadata mismatch: got port %d token %s", meta.Port, meta.SessionToken)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", srv.Port())

	// 2. Ping without token (Must fail with 401)
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/ping", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized without token, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 3. Ping with token (Must succeed with 200)
	req, _ = http.NewRequest("GET", baseURL+"/api/v1/ping", nil)
	req.Header.Set("X-SheepGet-Token", srv.SessionToken())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK with token, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 4. GET takeover config
	req, _ = http.NewRequest("GET", baseURL+"/api/v1/config/takeover", nil)
	req.Header.Set("X-SheepGet-Token", srv.SessionToken())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get takeover config failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for takeover config, got %d", resp.StatusCode)
	}
	var syncData TakeoverConfigSync
	if err := json.NewDecoder(resp.Body).Decode(&syncData); err != nil {
		t.Fatalf("failed to decode takeover config: %v", err)
	}
	_ = resp.Body.Close()
	if len(syncData.Extensions) != 2 || syncData.PauseShortcut != "Delete" {
		t.Errorf("unexpected sync data: %+v", syncData)
	}

	// 5. POST handover
	handoverPayload := HandoverRequest{
		SourceType:         "browser_takeover",
		URL:                "https://example.com/archive.zip",
		FilenameSuggestion: "archive.zip",
		PageContext: PageContext{
			PageURL: "https://example.com/download.html",
		},
		Credentials: &CredentialsPayload{
			Cookies: "session=xyz",
			Headers: map[string]string{
				"User-Agent": "TestBrowser",
			},
		},
	}
	payloadBytes, _ := json.Marshal(handoverPayload)
	req, _ = http.NewRequest("POST", baseURL+"/api/v1/handover", bytes.NewReader(payloadBytes))
	req.Header.Set("X-SheepGet-Token", srv.SessionToken())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("handover failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for handover, got %d", resp.StatusCode)
	}
	var handoverResp HandoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&handoverResp); err != nil {
		t.Fatalf("failed to decode handover response: %v", err)
	}
	_ = resp.Body.Close()
	if !handoverResp.Accepted || handoverResp.QueueItemID != "test_queue_item_123" {
		t.Errorf("unexpected handover response: %+v", handoverResp)
	}
	if handler.handledReq == nil || handler.handledReq.URL != handoverPayload.URL {
		t.Errorf("expected handler to receive handover payload")
	}

	// 6. WebSocket event connection and broadcast
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/api/v1/events?token=%s", srv.Port(), srv.SessionToken())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Trigger broadcast
	go func() {
		time.Sleep(50 * time.Millisecond)
		srv.BroadcastTakeoverConfig(config.TakeoverConfig{
			Extensions:    []string{"7z"},
			ExcludedSites: []string{"bad.com"},
			PauseShortcut: "Alt",
			ForceShortcut: "Ctrl",
		})
	}()

	var eventMsg EventMessage
	if err := wsjson.Read(ctx, conn, &eventMsg); err != nil {
		t.Fatalf("failed to read websocket event: %v", err)
	}
	if eventMsg.Event != "takeover_config_updated" {
		t.Errorf("expected takeover_config_updated event, got %s", eventMsg.Event)
	}

	// 7. Test Stop removes session file
	if err := srv.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Errorf("expected session file to be deleted upon server stop")
	}
}
