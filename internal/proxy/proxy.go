package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/gorilla/websocket"
)

// LocalProxy sits between the tunnel and your local dev server,
// handling both regular HTTP and WebSocket traffic.
type LocalProxy struct {
	Port         int
	PathRoutes   map[string]int // e.g. "/api" -> 3001, "/" -> 3000
	reverseProxy *httputil.ReverseProxy
	localURL     *url.URL

	// Vibecoder Demo Modes
	Freeze       *FreezeCache
	DemoMode     bool
	InjectWidget bool
	AutoLogin    string // Cookie injection

	// Advanced Features
	Password string // Basic Auth credentials

	// Traffic stats for the curious
	mu           sync.RWMutex
	requestCount int64
	bytesSent    int64

	// Pre-built middleware chain for hot-path performance
	handler http.Handler
}

var upgrader = websocket.Upgrader{
	// SECURITY: Validate origins to prevent SSRF.
	// Local dev + public tunr tunnel viewers (Next/Vite HMR from https://*.tunr.sh).
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		originURL, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := strings.ToLower(originURL.Hostname())
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return true
		}
		if host == "tunr.sh" || strings.HasSuffix(host, ".tunr.sh") {
			return true
		}
		// Optional: TUNR_WS_EXTRA_ALLOWED_ORIGIN_SUFFIXES=.mycompany.test,other.dev
		if extra := os.Getenv("TUNR_WS_EXTRA_ALLOWED_ORIGIN_SUFFIXES"); extra != "" {
			for _, suf := range strings.Split(extra, ",") {
				suf = strings.TrimSpace(strings.ToLower(suf))
				if suf != "" && strings.HasSuffix(host, suf) {
					return true
				}
			}
		}
		return false
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// NewLocalProxy spins up a reverse proxy targeting the given local port.
func NewLocalProxy(port int, pathRoutes map[string]int) (*LocalProxy, error) {
	if port < 1024 || port > 65535 {
		if len(pathRoutes) == 0 {
			return nil, fmt.Errorf("invalid port %d: must be between 1024-65535", port)
		}
	}

	localAddr := fmt.Sprintf("http://127.0.0.1:%d", port)
	localURL, err := url.Parse(localAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse local URL: %w", err)
	}

	rp := httputil.NewSingleHostReverseProxy(localURL)

	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)

		// Route to different local ports based on path prefix
		if len(pathRoutes) > 0 {
			bestPrefix := ""
			bestPort := 0
			for prefix, targetPort := range pathRoutes {
				if !strings.HasPrefix(req.URL.Path, prefix) {
					continue
				}
				// Prefer the longest matching prefix so /api beats /
				if len(prefix) > len(bestPrefix) {
					bestPrefix = prefix
					bestPort = targetPort
				}
			}
			if bestPort > 0 {
				req.URL.Host = fmt.Sprintf("127.0.0.1:%d", bestPort)
			}
		}
	}

	rp.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:       100,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
		// SECURITY: Runs without HTTPS since it's a local proxy,
		// but we still enforce TLS handshake timeout.
		TLSHandshakeTimeout: 10 * time.Second,
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("Upstream proxy error: %v (is anything running on port %d?)", err, port)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"upstream unavailable","port":%d,"hint":"run tunr doctor"}`, port)
	}

	rp.ModifyResponse = func(resp *http.Response) error {
		// Strip framework fingerprint headers — no need to advertise the stack
		resp.Header.Del("X-Powered-By")
		resp.Header.Del("Server")

		resp.Header.Set("X-Tunr-Proxy", "true")
		return nil
	}

	proxy := &LocalProxy{
		Port:         port,
		PathRoutes:   pathRoutes,
		reverseProxy: rp,
		localURL:     localURL,
	}

	proxy.handler = rp

	return proxy, nil
}

// ResolvePortForPath returns the local TCP port used for a request path (honours --route prefixes).
func (p *LocalProxy) ResolvePortForPath(requestPath string) int {
	path := requestPath
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	if len(p.PathRoutes) == 0 {
		return p.Port
	}
	bestPrefix := ""
	bestPort := 0
	for prefix, targetPort := range p.PathRoutes {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		if len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			bestPort = targetPort
		}
	}
	if bestPort > 0 {
		return bestPort
	}
	return p.Port
}

// ServeHTTP dispatches incoming requests — WebSocket gets forwarded, everything else gets proxied.
func (p *LocalProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.requestCount++
	p.mu.Unlock()

	logger.Debug("%s %s", r.Method, r.URL.Path)

	if isWebSocketRequest(r) {
		p.handleWebSocket(w, r)
		return
	}

	if p.InjectWidget && r.Method == http.MethodPost {
		switch r.URL.Path {
		case "/__tunr/feedback":
			p.handleFeedback(w, r)
			return
		case "/__tunr/error":
			p.handleError(w, r)
			return
		}
	}

	// Auto-Login Cookie Injection
	if p.AutoLogin != "" {
		if r.Header.Get("Cookie") != "" {
			r.Header.Set("Cookie", r.Header.Get("Cookie")+"; "+p.AutoLogin)
		} else {
			r.Header.Set("Cookie", p.AutoLogin)
		}
		// Also set as Authorization header if it looks like a bearer/basic token
		if strings.HasPrefix(strings.ToLower(p.AutoLogin), "bearer ") || strings.HasPrefix(strings.ToLower(p.AutoLogin), "basic ") {
			r.Header.Set("Authorization", p.AutoLogin)
		}
	}

	p.handler.ServeHTTP(w, r)
}

// BuildMiddlewareChain assembles the middleware stack based on proxy settings.
// Call this after all flags/config have been applied — order matters here.
func (p *LocalProxy) BuildMiddlewareChain() {
	var h http.Handler = p.reverseProxy

	// 0. Password Protection (innermost layer — checked first)
	if p.Password != "" {
		h = BasicAuthMiddleware(p.Password, h)
	}

	// 1. Freeze Mode (closest to upstream — catches crashes)
	if p.Freeze != nil && p.Freeze.enabled {
		h = p.Freeze.Middleware(h)

		// Hijack the error handler so Bad Gateway falls back to cache
		originalErrorHandler := p.reverseProxy.ErrorHandler
		p.reverseProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			if served := p.Freeze.ServeFromCache(w, r); served {
				return
			}
			originalErrorHandler(w, r, err)
		}
	}

	// 2. HTML Injector (Feedback widget)
	if p.InjectWidget {
		h = InjectMiddleware(h)
	}

	// 3. Demo Mode (outermost — intercepts before hitting local server)
	if p.DemoMode {
		h = DemoMiddleware(h)
	}

	p.handler = h
}

// isWebSocketRequest sniffs the Upgrade + Connection headers.
func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// handleWebSocket proxies WebSocket connections to the local server.
// Critical for Vite HMR, Next.js fast refresh, and friends.
func (p *LocalProxy) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	localWsURL := *p.localURL
	localWsURL.Scheme = "ws"
	localWsURL.Path = r.URL.Path
	localWsURL.RawQuery = r.URL.RawQuery

	upstreamConn, wsResp, err := websocket.DefaultDialer.Dial(localWsURL.String(), nil)
	if wsResp != nil && wsResp.Body != nil {
		defer wsResp.Body.Close()
	}
	if err != nil {
		logger.Warn("WS upstream connection failed (port %d): %v", p.Port, err)
		http.Error(w, "websocket upstream unavailable", http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("WS client upgrade failed: %v", err)
		return
	}
	defer clientConn.Close()

	// Bidirectional message relay via goroutine pair
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// Client → Upstream
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			msgType, msg, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if err := upstreamConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	// Upstream → Client
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			msgType, msg, err := upstreamConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	// When one side drops, tear down the other
	go func() {
		<-ctx.Done()
		clientConn.Close()
		upstreamConn.Close()
	}()

	wg.Wait()
}

// Stats returns request count and bytes sent for dashboard consumption.
func (p *LocalProxy) Stats() (requests int64, bytes int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.requestCount, p.bytesSent
}

// HealthCheck pings the local port to make sure something's actually listening.
func (p *LocalProxy) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://localhost:%d", p.Port), nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("port %d is unreachable: %w", p.Port, err)
	}
	defer resp.Body.Close()
	// Drain the body to avoid leaking connections
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ─── Vibecoder Demo Endpoints ────────────────────────────────────────────────

func (p *LocalProxy) handleFeedback(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Message   string `json:"message"`
		URL       string `json:"url"`
		UserAgent string `json:"user_agent"`
		Viewport  string `json:"viewport"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	logger.Info("")
	logger.Info("💬 NEW CLIENT FEEDBACK RECEIVED!")
	logger.Info("   ---------------------------")
	logger.Info("   Page    : %s", payload.URL)
	logger.Info("   Message : %s", payload.Message)
	logger.Info("   Screen  : %s", payload.Viewport)
	logger.Info("   ---------------------------")
	logger.Info("")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (p *LocalProxy) handleError(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Source  string `json:"source"`
		Line    int    `json:"line"`
		Col     int    `json:"col"`
		URL     string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	logger.Warn("")
	logger.Warn("🛑 REMOTE JS ERROR CAUGHT!")
	logger.Warn("   ---------------------------")
	logger.Warn("   Type    : %s", payload.Type)
	logger.Warn("   Error   : %s", payload.Message)
	logger.Warn("   Page    : %s", payload.URL)
	if payload.Source != "" {
		logger.Warn("   File    : %s:%d:%d", payload.Source, payload.Line, payload.Col)
	}
	logger.Warn("   ---------------------------")
	logger.Warn("")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
