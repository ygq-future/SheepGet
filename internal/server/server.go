// Package server implements the local loopback HTTP and WebSocket communication server
// for browser extension integration and desktop download handover according to ADR-0005.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"sheep-get/internal/atomicfile"
)

// PinnedExtensionID is the fixed 32-character extension ID for SheepGet (ADR-0005).
const PinnedExtensionID = "oediboaeofmnlkgcjhnpfnngphkjooam"

// AllowedExtensionOrigin is the expected browser extension Origin header value.
const AllowedExtensionOrigin = "chrome-extension://" + PinnedExtensionID

// Status represents the runtime status of the loopback HTTP and WebSocket server.
type Status struct {
	Running        bool   `json:"running"`
	Port           int    `json:"port"`
	ConnectedCount int    `json:"connectedCount"`
	Error          string `json:"error,omitempty"`
}

// DownloadHandler is implemented by the desktop app to accept handover requests.
type DownloadHandler interface {
	HandleHandover(ctx context.Context, req *HandoverRequest) (*HandoverResponse, error)
	// HandleHLSVariants 读取一份清单的可选清晰度，供扩展悬浮条在交接前弹菜单。
	HandleHLSVariants(ctx context.Context, req *HLSVariantsRequest) (*HLSVariantsResponse, error)
	// HandleMediaProbe 为扩展面板探测一条链接的展示信息（时长与大小）。
	HandleMediaProbe(ctx context.Context, req *MediaProbeRequest) (*MediaProbeResponse, error)
}

// ConfigProvider is implemented by the settings service to provide current takeover config sync data.
type ConfigProvider interface {
	GetTakeoverSync() TakeoverConfigSync
}

// Server provides the local loopback HTTP and WebSocket interface for browser extensions.
type Server struct {
	sessionPath    string
	handler        DownloadHandler
	configProvider ConfigProvider

	targetPort   int
	listener     net.Listener
	httpServer   *http.Server
	port         int
	sessionToken string
	startedAt    int64
	lastErr      string

	mu             sync.RWMutex
	clients        map[*websocket.Conn]struct{}
	configVersion  atomic.Int64
	onStatusChange func(Status)
	statusCh       chan Status
}

// NewServer creates an unstarted local loopback server instance.
func NewServer(sessionPath string, handler DownloadHandler, configProvider ConfigProvider) *Server {
	s := &Server{
		sessionPath:    sessionPath,
		handler:        handler,
		configProvider: configProvider,
		clients:        make(map[*websocket.Conn]struct{}),
		statusCh:       make(chan Status, 64),
	}
	s.configVersion.Store(time.Now().Unix())
	go s.statusWorker()
	return s
}

// SetTargetPort sets the desired listening port before start or restart.
func (s *Server) SetTargetPort(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targetPort = port
}

// SetOnStatusChange configures a callback invoked whenever server status or connected client count changes.
func (s *Server) SetOnStatusChange(fn func(Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onStatusChange = fn
}

// Status returns a snapshot of the current loopback server status.
func (s *Server) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Status{
		Running:        s.listener != nil,
		Port:           s.port,
		ConnectedCount: len(s.clients),
		Error:          s.lastErr,
	}
}

func (s *Server) statusWorker() {
	for st := range s.statusCh {
		s.mu.RLock()
		fn := s.onStatusChange
		s.mu.RUnlock()
		if fn != nil {
			fn(st)
		}
	}
}

func (s *Server) notifyStatusChangeLocked() {
	st := Status{
		Running:        s.listener != nil,
		Port:           s.port,
		ConnectedCount: len(s.clients),
		Error:          s.lastErr,
	}
	select {
	case s.statusCh <- st:
	default:
		select {
		case <-s.statusCh:
		default:
		}
		s.statusCh <- st
	}
}

// Start binds to the configured or ephemeral loopback port, writes session metadata, and starts serving.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked()
}

// Restart safely restarts listening on newPort without dropping the existing server if newPort is occupied.
func (s *Server) Restart(newPort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetPort := s.targetPort
	if newPort > 0 {
		targetPort = newPort
	}

	var newLn net.Listener
	var err error
	if targetPort > 0 {
		addr := fmt.Sprintf("127.0.0.1:%d", targetPort)
		newLn, err = net.Listen("tcp", addr)
		if err != nil {
			s.lastErr = err.Error()
			s.notifyStatusChangeLocked()
			return fmt.Errorf("failed to bind loopback server on %s: %w", addr, err)
		}
	} else {
		newLn, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.lastErr = err.Error()
			s.notifyStatusChangeLocked()
			return fmt.Errorf("failed to bind loopback server: %w", err)
		}
	}

	// Successfully bound new port; now safely stop previous server and switch
	s.stopLocked()
	s.targetPort = targetPort
	return s.startWithListenerLocked(newLn)
}

// Stop gracefully terminates all active connections, closes the server, and deletes the session file.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	return nil
}

func (s *Server) stopLocked() {
	for conn := range s.clients {
		_ = conn.Close(websocket.StatusNormalClosure, "server shutting down")
		delete(s.clients, conn)
	}

	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.httpServer.Shutdown(ctx)
		cancel()
		s.httpServer = nil
	}

	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}

	if s.sessionPath != "" {
		_ = os.Remove(s.sessionPath)
	}
	s.notifyStatusChangeLocked()

}

func (s *Server) startLocked() error {
	var ln net.Listener
	var err error
	if s.targetPort > 0 {
		addr := fmt.Sprintf("127.0.0.1:%d", s.targetPort)
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			s.lastErr = err.Error()
			s.notifyStatusChangeLocked()
			return fmt.Errorf("failed to bind loopback server on %s: %w", addr, err)
		}
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.lastErr = err.Error()
			s.notifyStatusChangeLocked()
			return fmt.Errorf("failed to bind loopback server: %w", err)
		}
	}
	return s.startWithListenerLocked(ln)
}

func (s *Server) startWithListenerLocked(ln net.Listener) error {
	s.listener = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	// Generate secure random token (32 bytes = 64 hex characters)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		_ = ln.Close()
		return fmt.Errorf("failed to generate secure session token: %w", err)
	}
	s.sessionToken = hex.EncodeToString(tokenBytes)
	s.startedAt = time.Now().Unix()

	// Write session.json atomically
	meta := SessionMetadata{
		Port:         s.port,
		SessionToken: s.sessionToken,
		PID:          os.Getpid(),
		StartedAt:    s.startedAt,
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("failed to marshal session metadata: %w", err)
	}
	if err := atomicfile.Write(s.sessionPath, metaBytes, 0600, false); err != nil {
		_ = ln.Close()
		return fmt.Errorf("failed to write session file: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/discover", s.handleDiscover)
	mux.HandleFunc("OPTIONS /api/v1/discover", s.handleDiscover)
	mux.HandleFunc("POST /api/v1/discover", s.handleDiscover)
	mux.HandleFunc("GET /api/v1/ping", s.authMiddleware(s.handlePing))
	mux.HandleFunc("GET /api/v1/config/takeover", s.authMiddleware(s.handleTakeoverConfig))
	mux.HandleFunc("HEAD /api/v1/config/takeover", s.authMiddleware(s.handleTakeoverConfig))
	mux.HandleFunc("POST /api/v1/handover", s.authMiddleware(s.handleHandover))
	mux.HandleFunc("POST /api/v1/hls/variants", s.authMiddleware(s.handleHLSVariants))
	mux.HandleFunc("POST /api/v1/media/probe", s.authMiddleware(s.handleMediaProbe))
	mux.HandleFunc("GET /api/v1/events", s.handleEvents)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	s.lastErr = ""
	s.notifyStatusChangeLocked()
	return nil
}

// Port returns the active bound port.
func (s *Server) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.port
}

// SessionToken returns the active session token.
func (s *Server) SessionToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionToken
}

// ConfigVersion returns the current version timestamp of takeover configuration.
func (s *Server) ConfigVersion() int64 {
	return s.configVersion.Load()
}

// BroadcastTakeoverConfig increments the configuration version and broadcasts it to connected clients.
func (s *Server) BroadcastTakeoverConfig(syncData TakeoverConfigSync) {
	newVersion := time.Now().Unix()
	s.configVersion.Store(newVersion)
	syncData.Version = newVersion

	msg := EventMessage{
		Event: "takeover_config_updated",
		Data:  syncData,
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for conn := range s.clients {
		go func(c *websocket.Conn) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = wsjson.Write(ctx, c, msg)
		}(conn)
	}
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-SheepGet-Token")
		s.mu.RLock()
		currentToken := s.sessionToken
		s.mu.RUnlock()
		if token == "" || token != currentToken {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
		http.Error(w, `{"error":"forbidden web origin"}`, http.StatusForbidden)
		return
	}
	if origin != "" && origin != AllowedExtensionOrigin {
		http.Error(w, `{"error":"forbidden origin"}`, http.StatusForbidden)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", AllowedExtensionOrigin)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-SheepGet-Token")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	s.mu.RLock()
	resp := map[string]any{
		"status":       "ok",
		"version":      "1.0.0",
		"port":         s.port,
		"sessionToken": s.sessionToken,
		"startedAt":    s.startedAt,
	}
	s.mu.RUnlock()
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","version":"1.0.0"}`))
}
func (s *Server) handleTakeoverConfig(w http.ResponseWriter, r *http.Request) {
	ver := s.configVersion.Load()
	eTag := fmt.Sprintf(`"%d"`, ver)
	w.Header().Set("ETag", eTag)
	w.Header().Set("X-Config-Version", fmt.Sprintf("%d", ver))
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	var syncData TakeoverConfigSync
	if s.configProvider != nil {
		syncData = s.configProvider.GetTakeoverSync()
	}
	syncData.Version = ver

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(syncData)
}

func (s *Server) handleHandover(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req HandoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(HandoverResponse{
			Accepted: false,
			Reason:   "invalid JSON payload",
		})
		return
	}

	// Validate URL
	parsedURL, err := url.Parse(req.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(HandoverResponse{
			Accepted: false,
			Reason:   "invalid URL scheme (only http/https supported)",
		})
		return
	}

	if s.handler == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(HandoverResponse{
			Accepted: false,
			Reason:   "handler not configured",
		})
		return
	}

	resp, err := s.handler.HandleHandover(r.Context(), &req)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HandoverResponse{
			Accepted: false,
			Reason:   err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleHLSVariants(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req HLSVariantsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid JSON payload"}`))
		return
	}
	if s.handler == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"handler not configured"}`))
		return
	}

	resp, err := s.handler.HandleHLSVariants(r.Context(), &req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleMediaProbe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req MediaProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid JSON payload"}`))
		return
	}
	if s.handler == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"handler not configured"}`))
		return
	}

	resp, err := s.handler.HandleMediaProbe(r.Context(), &req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("X-SheepGet-Token")
	}
	if token != s.sessionToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}

	s.mu.Lock()
	s.clients[conn] = struct{}{}
	s.notifyStatusChangeLocked()
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.notifyStatusChangeLocked()
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Hold the connection open until client disconnects or an error occurs
	ctx := r.Context()
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			var closeErr websocket.CloseError
			if errors.As(err, &closeErr) || errors.Is(err, context.Canceled) {
				return
			}
			return
		}
	}
}
