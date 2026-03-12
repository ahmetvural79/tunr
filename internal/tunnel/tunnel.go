package tunnel

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/proxy"
	"github.com/google/uuid"
)

// Version is set by the build system (cmd/tunr/main.go sets this at init).
var Version = "dev"

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

// runTunnel connects to the relay via WebSocket, performs hello/welcome
// handshake, then enters the request/response loop. Wraps everything in
// WithRetry so dropped connections auto-reconnect.
func (m *Manager) runTunnel(ctx context.Context, t *Tunnel, localProxy *proxy.LocalProxy, opts StartOptions) error {
	return WithRetry(ctx, DefaultRetryConfig, func(ctx context.Context, attempt int) error {
		if attempt > 1 {
			logger.Info("Reconnecting... (attempt %d)", attempt)
		}

		rc, welcome, err := ConnectRelay(ctx, m.relayURL, m.authToken, t.LocalPort, opts.Subdomain, Version)
		if err != nil {
			return fmt.Errorf("failed to connect to relay: %w", err)
		}

		t.mu.Lock()
		t.PublicURL = welcome.PublicURL
		t.Status = StatusActive
		t.mu.Unlock()

		logger.Info("Tunnel active: localhost:%d → %s", t.LocalPort, welcome.PublicURL)

		err = rc.RunLoop(ctx, func(_ context.Context, req *requestData) *responseData {
			t.requestCount.Add(1)
			return forwardViaProxy(localProxy, t.LocalPort, req)
		})

		t.mu.Lock()
		t.Status = StatusDisconnected
		t.mu.Unlock()

		if err == context.Canceled {
			return nil
		}
		return err
	})
}

// forwardViaProxy sends the relay request to the local dev server via
// the existing LocalProxy (which handles path routing, demo mode, etc.)
// and returns the response to be sent back to the relay.
func forwardViaProxy(lp *proxy.LocalProxy, port int, req *requestData) *responseData {
	bodyReader := strings.NewReader(req.Body)
	httpReq, err := http.NewRequest(req.Method, fmt.Sprintf("http://localhost:%d%s", port, req.Path), bodyReader)
	if err != nil {
		return &responseData{
			RequestID:  req.RequestID,
			StatusCode: http.StatusBadGateway,
			Headers:    map[string]string{"Content-Type": "text/plain"},
			Body:       "failed to build request: " + err.Error(),
		}
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	rec := &bufferedResponseWriter{header: http.Header{}, statusCode: http.StatusOK}
	lp.ServeHTTP(rec, httpReq)

	respHeaders := make(map[string]string, len(rec.header))
	for k, vals := range rec.header {
		respHeaders[k] = strings.Join(vals, ", ")
	}

	return &responseData{
		RequestID:  req.RequestID,
		StatusCode: rec.statusCode,
		Headers:    respHeaders,
		Body:       rec.body.String(),
	}
}

// bufferedResponseWriter captures an HTTP response in memory so we can
// serialize it back to the relay over WebSocket.
type bufferedResponseWriter struct {
	header      http.Header
	statusCode  int
	body        bytes.Buffer
	wroteHeader bool
}

func (w *bufferedResponseWriter) Header() http.Header { return w.header }
func (w *bufferedResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
	}
}
func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(b)
}

// Ensure the interface is satisfied at compile time.
var _ http.ResponseWriter = (*bufferedResponseWriter)(nil)

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
