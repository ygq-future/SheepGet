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
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"sheep-get/internal/atomicfile"
	"sheep-get/internal/config"
)

// DownloadHandler is implemented by the desktop app to accept handover requests.
type DownloadHandler interface {
	HandleHandover(ctx context.Context, req *HandoverRequest) (*HandoverResponse, error)
}

// ConfigProvider is implemented by the settings service to provide current takeover config.
type ConfigProvider interface {
	GetTakeoverConfig() config.TakeoverConfig
}

// Server provides the local loopback HTTP and WebSocket interface for browser extensions.
type Server struct {
	sessionPath    string
	handler        DownloadHandler
	configProvider ConfigProvider

	listener     net.Listener
	httpServer   *http.Server
	port         int
	sessionToken string
	startedAt    int64

	mu            sync.RWMutex
	clients       map[*websocket.Conn]struct{}
	configVersion atomic.Int64
}

// NewServer creates an unstarted local loopback server instance.
func NewServer(sessionPath string, handler DownloadHandler, configProvider ConfigProvider) *Server {
	s := &Server{
		sessionPath:    sessionPath,
		handler:        handler,
		configProvider: configProvider,
		clients:        make(map[*websocket.Conn]struct{}),
	}
	s.configVersion.Store(time.Now().Unix())
	return s
}

// Start binds to an ephemeral loopback port, writes session metadata, and starts serving.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to bind loopback server: %w", err)
	}
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
	mux.HandleFunc("GET /api/v1/ping", s.authMiddleware(s.handlePing))
	mux.HandleFunc("GET /api/v1/config/takeover", s.authMiddleware(s.handleTakeoverConfig))
	mux.HandleFunc("HEAD /api/v1/config/takeover", s.authMiddleware(s.handleTakeoverConfig))
	mux.HandleFunc("POST /api/v1/handover", s.authMiddleware(s.handleHandover))
	mux.HandleFunc("GET /api/v1/events", s.handleEvents)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return nil
}

// Stop gracefully terminates all active connections, closes the server, and deletes the session file.
func (s *Server) Stop() error {
	s.mu.Lock()
	for conn := range s.clients {
		_ = conn.Close(websocket.StatusNormalClosure, "server shutting down")
		delete(s.clients, conn)
	}
	s.mu.Unlock()

	var err error
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = s.httpServer.Shutdown(ctx)
	}

	if s.sessionPath != "" {
		_ = os.Remove(s.sessionPath)
	}

	return err
}

// Port returns the active bound port.
func (s *Server) Port() int {
	return s.port
}

// SessionToken returns the active session token.
func (s *Server) SessionToken() string {
	return s.sessionToken
}

// ConfigVersion returns the current version timestamp of takeover configuration.
func (s *Server) ConfigVersion() int64 {
	return s.configVersion.Load()
}

// BroadcastTakeoverConfig increments the configuration version and broadcasts it to connected clients.
func (s *Server) BroadcastTakeoverConfig(cfg config.TakeoverConfig) {
	newVersion := time.Now().Unix()
	s.configVersion.Store(newVersion)

	syncData := TakeoverConfigSync{
		Version:       newVersion,
		Extensions:    cfg.Extensions,
		ExcludedSites: cfg.ExcludedSites,
		PauseShortcut: cfg.PauseShortcut,
		ForceShortcut: cfg.ForceShortcut,
	}

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

// authMiddleware validates the X-SheepGet-Token header.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-SheepGet-Token")
		if token == "" || token != s.sessionToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next(w, r)
	}
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

	var cfg config.TakeoverConfig
	if s.configProvider != nil {
		cfg = s.configProvider.GetTakeoverConfig()
	}

	syncData := TakeoverConfigSync{
		Version:       ver,
		Extensions:    cfg.Extensions,
		ExcludedSites: cfg.ExcludedSites,
		PauseShortcut: cfg.PauseShortcut,
		ForceShortcut: cfg.ForceShortcut,
	}

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
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
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
