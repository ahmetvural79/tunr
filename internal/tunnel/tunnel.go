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
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/proxy"
)

// TunnelStatus represents where a tunnel is in its lifecycle
type TunnelStatus string

const (
	StatusConnecting   TunnelStatus = "connecting"
	StatusActive       TunnelStatus = "active"
	StatusError        TunnelStatus = "error"
	StatusDisconnected TunnelStatus = "disconnected"
)

// Tunnel is a single active tunnel instance
type Tunnel struct {
	ID        string
	LocalPort int
	PublicURL string
	Status    TunnelStatus
	StartedAt time.Time

	requestCount atomic.Int64 // atomic so we don't need a lock for reads

	mu     sync.RWMutex
	cancel context.CancelFunc

	cfProcess *exec.Cmd // non-nil when using cloudflared
}

// RequestCount returns how many requests have flowed through this tunnel
func (t *Tunnel) RequestCount() int64 {
	return t.requestCount.Load()
}

// Manager owns all tunnels and orchestrates their lifecycle
type Manager struct {
	tunnels   map[string]*Tunnel
	mu        sync.RWMutex
	relayURL  string
	authToken string // SECURITY: never logged
}

// NewManager spins up a fresh tunnel manager pointed at the given relay
func NewManager(relayURL string) *Manager {
	return &Manager{
		tunnels:  make(map[string]*Tunnel),
		relayURL: relayURL,
	}
}

// SetAuthToken sets the bearer token for relay auth.
// SECURITY: kept as a separate setter to prevent accidental logging during construction
func (m *Manager) SetAuthToken(token string) {
	m.authToken = token
}

// Start creates a tunnel and returns once the public URL is live.
// Works with both Cloudflare quicktunnels and custom relays.
func (m *Manager) Start(ctx context.Context, port int, opts StartOptions) (*Tunnel, error) {
	if err := validatePort(port); err != nil {
		return nil, err
	}

	routes := map[string]int{"/": port}
	if len(opts.PathRoutes) > 0 {
		routes = opts.PathRoutes
		// fall back to base port for root if the user didn't specify one
		if _, ok := routes["/"]; !ok && port > 0 {
			routes["/"] = port
		}
	}

	localProxy, err := proxy.NewLocalProxy(port, routes)
	if err != nil {
		return nil, fmt.Errorf("failed to start local proxy: %w", err)
	}

	localProxy.DemoMode = opts.DemoMode
	localProxy.InjectWidget = opts.InjectWidget
	localProxy.AutoLogin = opts.AutoLogin
	localProxy.Password = opts.Password // Basic Auth Password Protection
	if opts.Freeze {
		localProxy.Freeze = proxy.NewFreezeCache(true)
	}
	
	localProxy.BuildMiddlewareChain()

	if err := localProxy.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("can't reach port %d — start your app first, then run tunr", port)
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

	if opts.TTL > 0 {
		time.AfterFunc(opts.TTL, func() {
			logger.Warn("⏳ Tunnel TTL expired (%v). Shutting down...", opts.TTL)
			m.Remove(id)
		})
		logger.Info("⏳ This tunnel will auto-expire in %v", opts.TTL)
	}

	go func() {
		if err := m.runTunnel(tunnelCtx, t, localProxy, opts); err != nil {
			if err != context.Canceled {
				logger.Error("Tunnel %s failed: %v", id, err)
			}
			t.mu.Lock()
			t.Status = StatusDisconnected
			t.mu.Unlock()
		}
	}()

	// poll until the public URL is ready (max 15s)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.C:
			cancel()
			m.Remove(id)
			return nil, fmt.Errorf("tunnel failed to connect within 15 seconds — try 'tunr doctor'")
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
				return nil, fmt.Errorf("tunnel failed to start")
			}
		}
	}
}

// runTunnel is the actual connection loop.
// Wraps everything in WithRetry so dropped connections auto-reconnect.
func (m *Manager) runTunnel(ctx context.Context, t *Tunnel, localProxy *proxy.LocalProxy, opts StartOptions) error {
	localServer := &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", t.LocalPort+10000), // offset to avoid port collision
		Handler:      localProxy,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	_ = localServer // TODO: wire up in phase 1

	relayClient := NewRelayClient(m.relayURL, m.authToken)

	return WithRetry(ctx, DefaultRetryConfig, func(ctx context.Context, attempt int) error {
		if attempt > 1 {
			logger.Info("Reconnecting... (attempt %d)", attempt)
		}

		resp, err := relayClient.RequestTunnel(ctx, t.LocalPort, TunnelRequestOptions{
			Subdomain: opts.Subdomain,
			HTTPS:     opts.HTTPS,
		})
		if err != nil {
			return fmt.Errorf("failed to connect to relay: %w", err)
		}

		t.mu.Lock()
		t.PublicURL = resp.PublicURL
		t.Status = StatusActive
		t.mu.Unlock()

		logger.Info("Tunnel active: localhost:%d → %s", t.LocalPort, resp.PublicURL)

		// keep alive until context is cancelled
		<-ctx.Done()

		t.mu.Lock()
		t.Status = StatusDisconnected
		t.mu.Unlock()

		return nil
	})
}

// Remove stops a tunnel and evicts it from the map
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.tunnels[id]; ok {
		if t.cancel != nil {
			t.cancel()
		}
		if t.cfProcess != nil && t.cfProcess.Process != nil {
			_ = t.cfProcess.Process.Kill()
		}
		delete(m.tunnels, id)
	}
}

// List returns all active tunnels (thread-safe snapshot)
func (m *Manager) List() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		result = append(result, t)
	}
	return result
}

// StopAll tears down every tunnel — the nuclear option
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
	logger.Info("All tunnels shut down.")
}

// StartOptions holds everything you can tweak when starting a tunnel
type StartOptions struct {
	Subdomain string
	Domain    string
	HTTPS     bool
	AuthToken string `json:"-"`

	DemoMode     bool
	Freeze       bool
	InjectWidget bool
	AutoLogin    string

	Password   string
	TTL        time.Duration
	PathRoutes map[string]int
}

// validatePort makes sure you're not asking for something silly
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d is invalid (must be 1-65535)", port)
	}
	if port < 1024 {
		return fmt.Errorf("port %d requires root privileges — use 1024 or above", port)
	}
	return nil
}

// checkPortReachable is a legacy health check — kept around for backward compat
func checkPortReachable(port int) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d", port))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
