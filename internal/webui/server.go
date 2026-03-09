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

	"github.com/gorilla/websocket"
	"github.com/tunr-dev/tunr/internal/logger"
)

// Dashboard UI Go binary'nin içine gömülü!
// Bu sayede ayrı bir web sunucusu veya dosya görüntüleyici gerekmez.
// "tunr open" komutu tarayıcıyı açar, binary'nin kendisi serve eder.
//
//go:embed static/*
var staticFiles embed.FS

// LogEntry - tek bir HTTP request log satırı
type LogEntry struct {
	Time       time.Time `json:"time"`
	TunnelID   string    `json:"tunnel_id"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	StatusCode int       `json:"status_code"`
	DurationMs int64     `json:"duration_ms"`
	BytesSent  int64     `json:"bytes_sent"`
	RemoteIP   string    `json:"remote_ip"` // GÜVENLİK: tam IP, sadece dashboard'da gösterilir
}

// DashboardServer - tunr web dashboard sunucusu
type DashboardServer struct {
	port   int
	server *http.Server

	// Log ring buffer — son N kaydı tut
	mu      sync.RWMutex
	logs    []LogEntry
	maxLogs int

	// WebSocket subscribers — gerçek zamanlı log takibi
	wsMu        sync.Mutex
	wsClients   map[*websocket.Conn]bool

	// Tünel bilgisi callback (main'den inject edilir)
	getTunnels func() []TunnelSummary
}

// TunnelSummary - dashboard için özet tünel bilgisi
type TunnelSummary struct {
	ID           string    `json:"id"`
	LocalPort    int       `json:"local_port"`
	PublicURL    string    `json:"public_url"`
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"started_at"`
	RequestCount int64     `json:"request_count"`
}

// wsUpgrader - dashboard WebSocket bağlantıları için
var wsUpgrader = websocket.Upgrader{
	// GÜVENLİK: Dashboard sadece localhost'tan açılır
	// Ama yine de origin kontrolü yapıyoruz, alışkanlık olsun
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		// localhost ve 127.0.0.1'e izin ver
		host, _, _ := net.SplitHostPort(r.Host)
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	},
}

// New - dashboard sunucusu oluştur
func New(port int, getTunnels func() []TunnelSummary) *DashboardServer {
	return &DashboardServer{
		port:       port,
		maxLogs:    5000, // son 5000 isteği tut
		logs:       make([]LogEntry, 0, 5000),
		wsClients:  make(map[*websocket.Conn]bool),
		getTunnels: getTunnels,
	}
}

// Start - dashboard HTTP sunucusunu başlat
func (d *DashboardServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Statik dosyalar (embed.FS'ten)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("static dosyalar yüklenemedi: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// API endpoint'leri
	mux.HandleFunc("/api/tunnels", d.handleTunnels)
	mux.HandleFunc("/api/logs", d.handleLogs)
	mux.HandleFunc("/api/ws/logs", d.handleWSLogs)
	mux.HandleFunc("/api/status", d.handleStatus)

	d.server = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", d.port), // sadece localhost!
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("Dashboard: http://localhost:%d", d.port)

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.server.Shutdown(shutdownCtx)
	}()

	if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("dashboard sunucu hatası: %w", err)
	}
	return nil
}

// AddLog - yeni log kaydı ekle ve WebSocket subscribers'a bildir
func (d *DashboardServer) AddLog(entry LogEntry) {
	d.mu.Lock()
	// Ring buffer: dolunca eskiyi at
	if len(d.logs) >= d.maxLogs {
		d.logs = d.logs[len(d.logs)-d.maxLogs+1:]
	}
	d.logs = append(d.logs, entry)
	d.mu.Unlock()

	// WebSocket'e broadcast et (arka planda)
	go d.broadcastLog(entry)
}

// broadcastLog - tüm WebSocket clients'a log gönder
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

	// Kopuk bağlantıları temizle
	for _, conn := range dead {
		conn.Close()
		delete(d.wsClients, conn)
	}
}

// ─── API Handlers ────────────────────────────────────────────────────────────

// handleTunnels - aktif tunnel listesi
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
	// GÜVENLİK: Dashboard sadece localhost'tan erişilir ama yine de header ekle
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tunnels": tunnels,
		"count":   len(tunnels),
	})
}

// handleLogs - son N log kaydını getir
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

// handleWSLogs - WebSocket canlı log akışı
func (d *DashboardServer) handleWSLogs(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("Dashboard WS upgrade hatası: %v", err)
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

	// Son 100 log'u hemen gönder (yeni bağlanan kullanıcı görsün)
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

	// Bağlantı kopana kadar bekle (ping/pong)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Ping ticker — bağlantının canlı olduğunu kontrol et
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

// handleStatus - sunucu durumu
func (d *DashboardServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"version":   "faz2",
	})
}
