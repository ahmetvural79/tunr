package tunnel

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/tunr-dev/tunr/internal/logger"
	"github.com/tunr-dev/tunr/internal/proxy"
)

// TunnelStatus - tunnel'ın şu anki hali
type TunnelStatus string

const (
	StatusConnecting   TunnelStatus = "connecting"
	StatusActive       TunnelStatus = "active"
	StatusError        TunnelStatus = "error"
	StatusDisconnected TunnelStatus = "disconnected"
)

// Tunnel - tek bir aktif tunnel
type Tunnel struct {
	ID        string
	LocalPort int
	PublicURL string
	Status    TunnelStatus
	StartedAt time.Time

	// Thread-safe request sayacı
	requestCount atomic.Int64

	mu     sync.RWMutex
	cancel context.CancelFunc

	// Cloudflare process handle (cloudflared kullanıyorsak)
	cfProcess *exec.Cmd
}

// RequestCount - kaç istek geldi?
func (t *Tunnel) RequestCount() int64 {
	return t.requestCount.Load()
}

// Manager - tüm tunnel'ları yöneten merkezi yapı
type Manager struct {
	tunnels   map[string]*Tunnel
	mu        sync.RWMutex
	relayURL  string
	authToken string // asla log'a geçmez
}

// NewManager - tunnel manager oluştur
func NewManager(relayURL string) *Manager {
	return &Manager{
		tunnels:  make(map[string]*Tunnel),
		relayURL: relayURL,
	}
}

// SetAuthToken - auth token'ı set et
// GÜVENLİK: Setter ayrı tutuldu ki accidental logging önlensin
func (m *Manager) SetAuthToken(token string) {
	m.authToken = token
}

// Start - tunnel başlat ve public URL dön
// Hem Cloudflare hem de custom relay destekler
func (m *Manager) Start(ctx context.Context, port int, opts StartOptions) (*Tunnel, error) {
	// Port validasyonu - önce bunu yapalım
	if err := validatePort(port); err != nil {
		return nil, err
	}

	// Default port map: Her şey base porta
	routes := map[string]int{"/": port}
	// Path routing verilmişse ez
	if len(opts.PathRoutes) > 0 {
		routes = opts.PathRoutes
		// Eğer kullanıcı "/" kök dizini girmemişse ama base port varsa ekle
		if _, ok := routes["/"]; !ok && port > 0 {
			routes["/"] = port
		}
	}

	// Local proxy hazırla (WebSocket + HTTP handler + Vibecoder Middleware'leri)
	localProxy, err := proxy.NewLocalProxy(port, routes)
	if err != nil {
		return nil, fmt.Errorf("local proxy başlatılamadı: %w", err)
	}

	// Faz 8: Vibecoder Müşteri Demo Özelliklerini proxy'ye yükle
	localProxy.DemoMode = opts.DemoMode
	localProxy.InjectWidget = opts.InjectWidget
	localProxy.AutoLogin = opts.AutoLogin
	localProxy.Password = opts.Password // Basic Auth Password Protection
	if opts.Freeze {
		localProxy.Freeze = proxy.NewFreezeCache(true)
	}
	
	// Middleware zincirini (Demo -> Widget -> Freeze -> ReverseProxy) inşa et
	localProxy.BuildMiddlewareChain()

	// Bağlantı öncesi port kontrolü
	if err := localProxy.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("port %d'e ulaşılamıyor. Önce uygulamanızı başlatın, sonra tunr çalıştırın", port)
	}

	id := uuid.New().String()[:8]
	tunnelCtx, cancel := context.WithCancel(ctx)

	t := &Tunnel{
		ID:        id,
		LocalPort: port,
		Status:    StatusConnecting,
		StartedAt: time.Now(),
		cancel:    cancel,
	}

	m.mu.Lock()
	m.tunnels[id] = t
	m.mu.Unlock()

	// TTL (Auto-Expiring Tunnel) mantığı
	if opts.TTL > 0 {
		time.AfterFunc(opts.TTL, func() {
			logger.Warn("⏳ Tunnel TTL doldu (%v). Kapatılıyor...", opts.TTL)
			m.Remove(id)
		})
		logger.Info("⏳ Bu tunnel otomatik olarak kapanacak: %v", opts.TTL)
	}

	// Arka planda tunnel kur
	go func() {
		if err := m.runTunnel(tunnelCtx, t, localProxy, opts); err != nil {
			if err != context.Canceled {
				logger.Error("Tunnel %s hatayla bitti: %v", id, err)
			}
			t.mu.Lock()
			t.Status = StatusDisconnected
			t.mu.Unlock()
		}
	}()

	// URL hazır olana kadar bekle (max 15s)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.C:
			cancel()
			m.Remove(id)
			return nil, fmt.Errorf("tunnel 15 saniye içinde bağlanamadı. 'tunr doctor' çalıştırın")
		case <-ctx.Done():
			cancel()
			m.Remove(id)
			return nil, ctx.Err()
		case <-ticker.C:
			t.mu.RLock()
			status := t.Status
			pubURL := t.PublicURL
			t.mu.RUnlock()

			if status == StatusActive && pubURL != "" {
				return t, nil
			}
			if status == StatusError {
				m.Remove(id)
				return nil, fmt.Errorf("tunnel başlatılamadı")
			}
		}
	}
}

// runTunnel - gerçek tunnel bağlantı döngüsü
// Retry ile birlikte çalışır, bağlantı kopunca yeniden kurar
func (m *Manager) runTunnel(ctx context.Context, t *Tunnel, localProxy *proxy.LocalProxy, opts StartOptions) error {
	// Local HTTP sunucusu başlat (relay bu adrese bağlanacak)
	localServer := &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", t.LocalPort+10000), // offset ile çakışma önle
		Handler:      localProxy,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	_ = localServer // Faz 1'de açılacak

	// Cloudflare quicktunnel veya relay'e bağlan
	relayClient := NewRelayClient(m.relayURL, m.authToken)

	return WithRetry(ctx, DefaultRetryConfig, func(ctx context.Context, attempt int) error {
		if attempt > 1 {
			logger.Info("Yeniden bağlanılıyor... (deneme %d)", attempt)
		}

		// Relay'den tunnel URL iste
		resp, err := relayClient.RequestTunnel(ctx, t.LocalPort, TunnelRequestOptions{
			Subdomain: opts.Subdomain,
			HTTPS:     opts.HTTPS,
		})
		if err != nil {
			return fmt.Errorf("relay bağlantısı kurulamadı: %w", err)
		}

		// URL hazır, güncelle
		t.mu.Lock()
		t.PublicURL = resp.PublicURL
		t.Status = StatusActive
		t.mu.Unlock()

		logger.Info("Tunnel aktif: localhost:%d → %s", t.LocalPort, resp.PublicURL)

		// Şimdi tunnel'ı canlı tut (context iptal olana kadar)
		<-ctx.Done()

		t.mu.Lock()
		t.Status = StatusDisconnected
		t.mu.Unlock()

		return nil
	})
}

// Remove - tunnel'ı durdur ve listeden sil
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.tunnels[id]; ok {
		if t.cancel != nil {
			t.cancel()
		}
		// Cloudflare process varsa öldür
		if t.cfProcess != nil && t.cfProcess.Process != nil {
			_ = t.cfProcess.Process.Kill()
		}
		delete(m.tunnels, id)
	}
}

// List - aktif tunnel listesi (thread-safe)
func (m *Manager) List() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		result = append(result, t)
	}
	return result
}

// StopAll - tüm tunnel'ları durdur
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.tunnels))
	for id := range m.tunnels {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.Remove(id)
	}
	logger.Info("Tüm tunnel'lar kapatıldı.")
}

// StartOptions - tunnel başlatma parametreleri
type StartOptions struct {
	Subdomain string
	HTTPS     bool
	AuthToken string `json:"-"` // JSON'a yazılmaz, log'a geçmez

	// Faz 8: Vibecoder Özellikleri
	DemoMode     bool   // GET/HEAD dışı istekleri durdur
	Freeze       bool   // Sayfa kapanırsa cache'den son sürümü dön
	InjectWidget bool   // </body> tag'ine Feedback+Error widget bas
	AutoLogin    string // Otomatik geçici Auth cookie/header yükle
	
	// Advanced Features
	Password   string         // Tünele özel Basic Auth şifresi (boşsa kapalı)
	TTL        time.Duration  // Otomatik kapanma süresi (0 ise süresiz)
	PathRoutes map[string]int // Çoklu port yönlendirme (örn: /api -> 3001)
}

// validatePort - port numarası mantıklı mı?
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d geçersiz (1-65535 arası olmalı)", port)
	}
	if port < 1024 {
		return fmt.Errorf("port %d için root yetkisi gerekir; 1024 ve üzeri bir port kullanın", port)
	}
	return nil
}

// checkPortReachable - port açık mı? (HealthCheck ile aynı, backward compat için)
func checkPortReachable(port int) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d", port))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
