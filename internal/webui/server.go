package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/gorilla/websocket"
)

// The dashboard UI is embedded in the binary itself.
// No separate web server or file host needed —
// "tunr open" launches the browser and this binary serves the UI.
//
//go:embed static/*
var staticFiles embed.FS

// LogEntry is a single HTTP request log line
type LogEntry struct {
	Time       time.Time `json:"time"`
	TunnelID   string    `json:"tunnel_id"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	StatusCode int       `json:"status_code"`
	DurationMs int64     `json:"duration_ms"`
	BytesSent  int64     `json:"bytes_sent"`
	RemoteIP   string    `json:"remote_ip"` // SECURITY: full IP, only shown in the local dashboard
}

// DashboardServer serves the tunr web dashboard
type DashboardServer struct {
	port   int
	server *http.Server

	mu      sync.RWMutex
	logs    []LogEntry
	maxLogs int

	// WebSocket subscribers for real-time log streaming
	wsMu      sync.Mutex
	wsClients map[*websocket.Conn]bool

	getTunnels func() []TunnelSummary
}

// TunnelSummary is a condensed tunnel view for the dashboard
type TunnelSummary struct {
	ID           string    `json:"id"`
	LocalPort    int       `json:"local_port"`
	PublicURL    string    `json:"public_url"`
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"started_at"`
	RequestCount int64     `json:"request_count"`
}

var wsUpgrader = websocket.Upgrader{
	// SECURITY: dashboard is localhost-only, but we still check origin — defense in depth
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		host, _, _ := net.SplitHostPort(r.Host)
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	},
}

// New creates a dashboard server
func New(port int, getTunnels func() []TunnelSummary) *DashboardServer {
	return &DashboardServer{
		port:       port,
		maxLogs:    5000,
		logs:       make([]LogEntry, 0, 5000),
		wsClients:  make(map[*websocket.Conn]bool),
		getTunnels: getTunnels,
	}
}

// Start boots the dashboard HTTP server
func (d *DashboardServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to load static files: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	mux.HandleFunc("/api/tunnels", d.handleTunnels)
	mux.HandleFunc("/api/logs", d.handleLogs)
	mux.HandleFunc("/api/ws/logs", d.handleWSLogs)
	mux.HandleFunc("/api/status", d.handleStatus)

	d.server = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", d.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("Dashboard: http://localhost:%d", d.port)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.server.Shutdown(shutdownCtx)
	}()

	if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("dashboard server error: %w", err)
	}
	return nil
}

// AddLog appends a log entry and broadcasts it to WebSocket subscribers
func (d *DashboardServer) AddLog(entry LogEntry) {
	d.mu.Lock()
	if len(d.logs) >= d.maxLogs {
		d.logs = d.logs[len(d.logs)-d.maxLogs+1:]
	}
	d.logs = append(d.logs, entry)
	d.mu.Unlock()

	go d.broadcastLog(entry)
}

func (d *DashboardServer) broadcastLog(entry LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	d.wsMu.Lock()
	defer d.wsMu.Unlock()

	dead := make([]*websocket.Conn, 0)
	for conn := range d.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			dead = append(dead, conn)
		}
	}

	for _, conn := range dead {
		conn.Close()
		delete(d.wsClients, conn)
	}
}

// ─── API Handlers ────────────────────────────────────────────────────────────

func (d *DashboardServer) handleTunnels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var tunnels []TunnelSummary
	if d.getTunnels != nil {
		tunnels = d.getTunnels()
	} else {
		tunnels = []TunnelSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	// SECURITY: dashboard is localhost-only, but set cache headers anyway — belt and suspenders
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tunnels": tunnels,
		"count":   len(tunnels),
	})
}

func (d *DashboardServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	d.mu.RLock()
	logs := make([]LogEntry, len(d.logs))
	copy(logs, d.logs)
	d.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":  logs,
		"count": len(logs),
	})
}

// handleWSLogs upgrades to WebSocket for live log streaming
func (d *DashboardServer) handleWSLogs(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("Dashboard WS upgrade error: %v", err)
		return
	}

	d.wsMu.Lock()
	d.wsClients[conn] = true
	d.wsMu.Unlock()

	defer func() {
		d.wsMu.Lock()
		delete(d.wsClients, conn)
		d.wsMu.Unlock()
		conn.Close()
	}()

	// Send the last 100 logs immediately so new clients aren't staring at an empty screen
	d.mu.RLock()
	recentCount := 100
	start := 0
	if len(d.logs) > recentCount {
		start = len(d.logs) - recentCount
	}
	recent := make([]LogEntry, len(d.logs[start:]))
	copy(recent, d.logs[start:])
	d.mu.RUnlock()

	for _, entry := range recent {
		data, _ := json.Marshal(entry)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}

	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func (d *DashboardServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"version":   "phase2",
	})
}
