package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"sheep-get/internal/protocol"
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

func (m *mockDownloadHandler) HandleHLSVariants(_ context.Context, _ *HLSVariantsRequest) (*HLSVariantsResponse, error) {
	return &HLSVariantsResponse{Variants: []HLSVariantOption{}}, nil
}

func (m *mockDownloadHandler) HandleMediaProbe(_ context.Context, _ *MediaProbeRequest) (*MediaProbeResponse, error) {
	return &MediaProbeResponse{}, nil
}

type mockConfigProvider struct {
	syncData TakeoverConfigSync
}

func (m *mockConfigProvider) GetTakeoverSync() TakeoverConfigSync {
	return m.syncData
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
		syncData: TakeoverConfigSync{
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
	req, _ := http.NewRequest("GET", baseURL+protocol.PathPing, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized without token, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 3. Ping with token (Must succeed with 200)
	req, _ = http.NewRequest("GET", baseURL+protocol.PathPing, nil)
	req.Header.Set(protocol.HeaderToken, srv.SessionToken())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK with token, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 4. GET takeover config
	req, _ = http.NewRequest("GET", baseURL+protocol.PathTakeoverConfig, nil)
	req.Header.Set(protocol.HeaderToken, srv.SessionToken())
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
	req, _ = http.NewRequest("POST", baseURL+protocol.PathHandover, bytes.NewReader(payloadBytes))
	req.Header.Set(protocol.HeaderToken, srv.SessionToken())
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
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d%s?%s=%s", srv.Port(), protocol.PathEvents, protocol.QueryParamToken, srv.SessionToken())
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
		srv.BroadcastTakeoverConfig(TakeoverConfigSync{
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
	if eventMsg.Event != protocol.EventTakeoverConfigUpdated {
		t.Errorf("expected %s event, got %s", protocol.EventTakeoverConfigUpdated, eventMsg.Event)
	}

	// 7. Test Stop removes session file
	if err := srv.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Errorf("expected session file to be deleted upon server stop")
	}
}

func TestServer_Discover(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")
	handler := &mockDownloadHandler{}
	cfgProvider := &mockConfigProvider{syncData: TakeoverConfigSync{ExcludedSites: []string{}}}

	srv := NewServer(sessionPath, handler, cfgProvider)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", srv.Port())

	// Case 1: Extension origin succeeds
	req, _ := http.NewRequest("GET", baseURL+protocol.PathDiscover, nil)
	req.Header.Set("Origin", protocol.ExtensionOrigin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("discover request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for discover, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != protocol.ExtensionOrigin {
		t.Errorf("expected CORS header %s, got %s", protocol.ExtensionOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode discover response: %v", err)
	}
	_ = resp.Body.Close()
	if payload["sessionToken"] != srv.SessionToken() {
		t.Errorf("expected sessionToken %s, got %v", srv.SessionToken(), payload["sessionToken"])
	}

	// Case 2: Web origin is blocked (CSRF defense)
	reqWeb, _ := http.NewRequest("GET", baseURL+protocol.PathDiscover, nil)
	reqWeb.Header.Set("Origin", "https://malicious-website.com")
	respWeb, err := http.DefaultClient.Do(reqWeb)
	if err != nil {
		t.Fatalf("web origin discover failed: %v", err)
	}
	if respWeb.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for web origin, got %d", respWeb.StatusCode)
	}
	_ = respWeb.Body.Close()

	// Case 3: Other extension origin is blocked
	reqOther, _ := http.NewRequest("GET", baseURL+protocol.PathDiscover, nil)
	reqOther.Header.Set("Origin", "chrome-extension://malicious-extension-id-12345678")
	respOther, err := http.DefaultClient.Do(reqOther)
	if err != nil {
		t.Fatalf("other extension discover failed: %v", err)
	}
	if respOther.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for other extension, got %d", respOther.StatusCode)
	}
	_ = respOther.Body.Close()

	// Case 4: Request without origin (standard Chrome extension service worker GET) succeeds
	reqEmpty, _ := http.NewRequest("GET", baseURL+protocol.PathDiscover, nil)
	respEmpty, err := http.DefaultClient.Do(reqEmpty)
	if err != nil {
		t.Fatalf("empty origin discover failed: %v", err)
	}
	if respEmpty.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for extension GET without origin, got %d", respEmpty.StatusCode)
	}
	_ = respEmpty.Body.Close()
}

func TestServer_RestartOnPort(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")
	handler := &mockDownloadHandler{}
	cfgProvider := &mockConfigProvider{syncData: TakeoverConfigSync{ExcludedSites: []string{}}}

	srv := NewServer(sessionPath, handler, cfgProvider)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	initialPort := srv.Port()

	// Find an available port for testing restart
	testLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get available port: %v", err)
	}
	newPort := testLn.Addr().(*net.TCPAddr).Port
	_ = testLn.Close()

	// Restart on newPort
	if err := srv.Restart(newPort); err != nil {
		t.Fatalf("failed to restart on port %d: %v", newPort, err)
	}
	if srv.Port() != newPort {
		t.Errorf("expected server port %d after restart, got %d", newPort, srv.Port())
	}

	// Verify new port is responsive
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", newPort, protocol.PathDiscover), nil)
	req.Header.Set("Origin", protocol.ExtensionOrigin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request to restarted server failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK after restart, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Verify that restarting on an occupied port automatically increments within span
	occupiedLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on test port: %v", err)
	}
	occupiedPort := occupiedLn.Addr().(*net.TCPAddr).Port
	defer func() { _ = occupiedLn.Close() }()

	if err := srv.Restart(occupiedPort); err != nil {
		t.Fatalf("expected restart on occupied port %d to auto-increment and succeed: %v", occupiedPort, err)
	}
	if srv.Port() <= occupiedPort || srv.Port() > occupiedPort+protocol.PortFallbackSpan {
		t.Errorf("expected server port to auto-increment within span [%d, %d], got %d", occupiedPort, occupiedPort+protocol.PortFallbackSpan, srv.Port())
	}

	// Verify that when all ports in the auto-increment span are occupied, restart fails gracefully and keeps previous server running
	currentPort := srv.Port()
	exhaustLns := make([]net.Listener, 0, protocol.PortFallbackSpan+1)
	baseBlockedPort := 54000
	for p := baseBlockedPort; p <= baseBlockedPort+protocol.PortFallbackSpan; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			t.Fatalf("failed to occupy test port %d: %v", p, err)
		}
		exhaustLns = append(exhaustLns, ln)
	}
	defer func() {
		for _, ln := range exhaustLns {
			_ = ln.Close()
		}
	}()

	if err := srv.Restart(baseBlockedPort); err == nil {
		t.Errorf("expected restart on completely occupied port span [%d-%d] to fail", baseBlockedPort, baseBlockedPort+protocol.PortFallbackSpan)
	}
	if srv.Port() != currentPort {
		t.Errorf("expected server to retain port %d after failed restart attempt, got %d", currentPort, srv.Port())
	}
	_ = initialPort
}
