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
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tunr-dev/tunr/internal/logger"
)

// LocalProxy - local port'a gelen istekleri karşılayan HTTP proxy.
// Hem normal HTTP hem de WebSocket trafiğini handle eder.
type LocalProxy struct {
	Port         int
	PathRoutes   map[string]int // Örn: "/api" -> 3001, "/" -> 3000
	reverseProxy *httputil.ReverseProxy
	localURL     *url.URL

	// Vibecoder Demo Modları
	Freeze       *FreezeCache
	DemoMode     bool
	InjectWidget bool
	AutoLogin    string // Cookie injection
	
	// Advanced Features
	Password     string // Basic Auth credentials

	// İstatistikler - kaç istek geldi, meraklılar için
	mu           sync.RWMutex
	requestCount int64
	bytesSent    int64
	
	// Middleware chain kopyası (performans için)
	handler http.Handler
}

// WebSocket upgrader - HTTP bağlantısını WS'a upgrade etmek için
var upgrader = websocket.Upgrader{
	// GÜVENLİK: Origin kontrolü yapıyoruz.
	// CheckOrigin'i override etmek SSRF riskini artırır.
	// Local proxy olduğu için localhost'a izin veriyoruz.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // WebSocket origin header olmayabilir
		}
		// Sadece localhost origin'e izin ver
		// (bu bir local dev proxy, dışarıdan bağlantı olmamalı)
		originURL, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := originURL.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// NewLocalProxy - local port için reverse proxy oluştur
func NewLocalProxy(port int, pathRoutes map[string]int) (*LocalProxy, error) {
	// Port validasyonu
	if port < 1024 || port > 65535 {
		if len(pathRoutes) == 0 {
			return nil, fmt.Errorf("geçersiz port %d: 1024-65535 arası olmalı", port)
		}
	}

	localAddr := fmt.Sprintf("http://localhost:%d", port)
	localURL, err := url.Parse(localAddr)
	if err != nil {
		return nil, fmt.Errorf("local URL oluşturulamadı: %w", err)
	}

	rp := httputil.NewSingleHostReverseProxy(localURL)

	// Path Routing (Dinamic Director Override)
	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)

		// PathRoutes boş değilse ve mevcut path bir rota ile eşleşiyorsa hedef portu değiştir
		if len(pathRoutes) > 0 {
			for prefix, targetPort := range pathRoutes {
				if strings.HasPrefix(req.URL.Path, prefix) {
					req.URL.Host = fmt.Sprintf("localhost:%d", targetPort)
					// URL rewrite isteniyorsa (örn: /api/x -> /x) buraya eklenebilir. 
					// Slim.sh standartında prefix genelde korunur.
					break
				}
			}
		}
	}

	// Transport ayarları - timeout ve connection pooling
	rp.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,  // bağlantı timeout
			KeepAlive: 30 * time.Second, // keepalive
		}).DialContext,
		MaxIdleConns:       100,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
		// GÜVENLİK: Local proxy olduğu için HTTPS olmadan çalışır.
		// Ama TLS inspect'e izin vermiyoruz.
		TLSHandshakeTimeout: 10 * time.Second,
	}

	// Proxy hata handler - güzel hata mesajları
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("Upstream proxy hatası: %v (port %d'de bir şey çalışıyor mu?)", err, port)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"upstream unavailable","port":%d,"hint":"tunr doctor calistirin"}`, port)
	}

	// Response modifier - info sızıntısı önle
	rp.ModifyResponse = func(resp *http.Response) error {
		// Framework'ün kendini açıklayan header'larını kaldır
		// (Security through obscurity değil, ama gereksiz bilgi vermeme)
		resp.Header.Del("X-Powered-By")
		resp.Header.Del("Server")

		// tunr header'ını ekle (debugging için yararlı)
		resp.Header.Set("X-Tunr-Proxy", "true")
		return nil
	}

	proxy := &LocalProxy{
		Port:         port,
		PathRoutes:   pathRoutes,
		reverseProxy: rp,
		localURL:     localURL,
	}
	
	// Base handler olarak reverse proxy'yi ayarla
	proxy.handler = rp

	return proxy, nil
}

// ServeHTTP - gelen istekleri handle et
// WebSocket mi? Forward et. Normal HTTP mi? proxy et.
func (p *LocalProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.requestCount++
	p.mu.Unlock()

	logger.Debug("%s %s", r.Method, r.URL.Path)

	// WebSocket upgrade isteği mi?
	if isWebSocketRequest(r) {
		p.handleWebSocket(w, r)
		return
	}

	// Auto-Login Cookie Injection
	if p.AutoLogin != "" {
		if r.Header.Get("Cookie") != "" {
			r.Header.Set("Cookie", r.Header.Get("Cookie")+"; "+p.AutoLogin)
		} else {
			r.Header.Set("Cookie", p.AutoLogin)
		}
		// Authorization header formunda verilmişse
		if strings.HasPrefix(strings.ToLower(p.AutoLogin), "bearer ") || strings.HasPrefix(strings.ToLower(p.AutoLogin), "basic ") {
			r.Header.Set("Authorization", p.AutoLogin)
		}
	}

	// Middleware Zinciri Oluştur (Yalnızca bir kez değil, her istekte
	// handler'ı kullanıyoruz. rp'nin sarmalanmış hali ServeHTTP'ye veriliyor)
	p.handler.ServeHTTP(w, r)
}

// BuildMiddlewareChain — Proxy ayarlarına göre middleware zincirini oluşturur.
// Bu metot CLI tarafından ayarlamalar yapıldıktan SONRA çağrılmalıdır.
func (p *LocalProxy) BuildMiddlewareChain() {
	var h http.Handler = p.reverseProxy

	// 0. Password Protection (En içte, ilk bu kontrol edilsin)
	if p.Password != "" {
		h = BasicAuthMiddleware(p.Password, h)
	}

	// 1. Freeze Mode (Upstream'e en yakın 2. katman)
	if p.Freeze != nil && p.Freeze.enabled {
		h = p.Freeze.Middleware(h)
		
		// Eğer reverse proxy "Bad Gateway" dönerse cache'i kullanmak için 
		// ErrorHandler'ı güncelliyoruz.
		originalErrorHandler := p.reverseProxy.ErrorHandler
		p.reverseProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			if served := p.Freeze.ServeFromCache(w, r); served {
				return // Cache'den başarıyla döndük!
			}
			originalErrorHandler(w, r, err)
		}
	}

	// 2. HTML Injector (Feedback widget)
	if p.InjectWidget {
		h = InjectMiddleware(h)
	}

	// 3. Demo Mode (En dışta, istek daha local sunucuya gitmeden kesilsin)
	if p.DemoMode {
		h = DemoMiddleware(h)
	}

	p.handler = h
}

// isWebSocketRequest - istek WebSocket upgrade mı?
func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// handleWebSocket - WebSocket bağlantısını local'e proxy et
// Bu Vite HMR, Next.js fast refresh vb. için kritik!
func (p *LocalProxy) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Local'deki WS adresini oluştur
	localWsURL := *p.localURL
	localWsURL.Scheme = "ws"
	localWsURL.Path = r.URL.Path
	localWsURL.RawQuery = r.URL.RawQuery

	// Upstream'e (local port) bağlan
	upstreamConn, _, err := websocket.DefaultDialer.Dial(localWsURL.String(), nil)
	if err != nil {
		logger.Warn("WS upstream bağlantısı kurulamadı (port %d): %v", p.Port, err)
		http.Error(w, "websocket upstream unavailable", http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	// Client'ı upgrade et
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("WS client upgrade hatası: %v", err)
		return
	}
	defer clientConn.Close()

	// İki yönlü mesaj kopyalama - goroutine pair
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

	// Birisi bitince diğerini de kapat
	go func() {
		<-ctx.Done()
		clientConn.Close()
		upstreamConn.Close()
	}()

	wg.Wait()
}

// Stats - proxy istatistikleri (dashboard için)
func (p *LocalProxy) Stats() (requests int64, bytes int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.requestCount, p.bytesSent
}

// HealthCheck - local port'un sağlıklı olduğunu doğrula
func (p *LocalProxy) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://localhost:%d", p.Port), nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("port %d'e ulaşılamıyor: %w", p.Port, err)
	}
	defer resp.Body.Close()
	// Body'yi oku ve at - resource leak olmasın
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
	logger.Info("💬 YENİ MÜŞTERİ FEEDBACK GELDİ!")
	logger.Info("   ---------------------------")
	logger.Info("   Sayfa  : %s", payload.URL)
	logger.Info("   Mesaj  : %s", payload.Message)
	logger.Info("   Ekran  : %s", payload.Viewport)
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
	logger.Warn("🛑 UZAK İSTEMCİDE JS HATASI YAKALANDI!")
	logger.Warn("   ---------------------------")
	logger.Warn("   Tür    : %s", payload.Type)
	logger.Warn("   Hata   : %s", payload.Message)
	logger.Warn("   Sayfa  : %s", payload.URL)
	if payload.Source != "" {
		logger.Warn("   Dosya  : %s:%d:%d", payload.Source, payload.Line, payload.Col)
	}
	logger.Warn("   ---------------------------")
	logger.Warn("")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
