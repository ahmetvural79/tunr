package relay

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/logger"
	"github.com/google/uuid"
)

// Proxy — gelen HTTP isteklerini doğru tunnel'a yönlendirir.
//
// Reverse proxy mantığı:
//   GET https://abc1x2y3.tunr.sh/api/users
//   → subdomain = "abc1x2y3"
//   → registry'de tunnel aranır
//   → istek WS üzerinden CLI'ya iletilir
//   → CLI local:3000/api/users'a forward eder
//   → cevap WS üzerinden relay'e döner
//   → relay HTTP yanıtı olarak dışarıya gönderir

// Proxy — HTTP istek proxy'si
type Proxy struct {
	registry *Registry
	domain   string
}

// NewProxy — oluştur
func NewProxy(registry *Registry, domain string) *Proxy {
	return &Proxy{registry: registry, domain: domain}
}

// ServeHTTP — gelen isteği ilgili tunnel'a proxy'le
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Subdomain'i host header'dan çıkar
	host := r.Host
	subdomain := extractSubdomain(host, p.domain)
	if subdomain == "" {
		// Subdomain yok veya ana domain → dashboard/landing'e yönlendir
		http.Redirect(w, r, "https://tunr.sh", http.StatusFound)
		return
	}

	// Tunnel'ı bul
	entry, ok := p.registry.Lookup(subdomain)
	if !ok {
		writeTunnelNotFound(w, subdomain)
		return
	}

	// Tunnel hala aktif mi?
	if !entry.IsAlive() {
		writeTunnelGone(w, subdomain)
		return
	}

	if isBrowserWebSocket(r) {
		p.serveBrowserWebSocket(w, r, entry)
		return
	}

	// Request body'yi oku
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(io.LimitReader(r.Body, 32*1024*1024)) // 32MB limit
		r.Body.Close()
	}

	// İstek oluştur
	req := &TunnelRequest{
		ID:       uuid.New().String()[:8],
		Method:   r.Method,
		Path:     r.URL.RequestURI(), // path + query string
		Headers:  r.Header.Clone(),
		Body:     body,
		Response: make(chan *TunnelResponse, 1),
	}

	// GÜVENLİK: Hassas header'ları temizle
	// X-Forwarded-For'u güvenilir şekilde set et (SSRF koruması)
	req.Headers.Del("X-Forwarded-Host")
	req.Headers.Del("X-Real-IP")
	req.Headers.Set("X-Forwarded-Host", r.Host)
	req.Headers.Set("X-Forwarded-For", realIP(r))
	req.Headers.Set("X-Forwarded-Proto", "https")
	req.Headers.Set("X-Tunr-Tunnel-ID", entry.ID)

	// Tunnel'a ilet
	start := time.Now()
	resp, err := entry.ForwardRequest(req)
	duration := time.Since(start)

	if err != nil {
		logger.Warn("Proxy hata (tunnel %s): %v", entry.ID, err)
		writeTunnelError(w, err.Error())
		return
	}

	// Cevabı dışarıya yaz
	for key, vals := range resp.Headers {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	// GÜVENLİK: Güvenlik header'larını her zaman set et
	// CLI'nın unutması ihtimaline karşı
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Del("Server") // sunucu bilgisi sızmasın

	w.WriteHeader(resp.StatusCode)
	w.Write(resp.Body)

	logger.Info("PROXY %s %s%s → %d (%dms)",
		req.Method, subdomain, req.Path, resp.StatusCode, duration.Milliseconds())
}

// extractSubdomain — "abc1x2y3.tunr.sh" → "abc1x2y3"
func extractSubdomain(host, domain string) string {
	host = strings.Split(host, ":")[0] // port'u sil
	if !strings.HasSuffix(host, "."+domain) {
		return ""
	}
	return strings.TrimSuffix(host, "."+domain)
}

// realIP — gerçek client IP alınır (Fly.io/Cloudflare header'ları dahil)
// GÜVENLİK: Bu değer sadece log için — asla auth'ta kullanma
func realIP(r *http.Request) string {
	// Fly.io
	if ip := r.Header.Get("Fly-Client-IP"); ip != "" {
		return ip
	}
	// Cloudflare
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	// Genel reverse proxy
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

// ─── Hata sayfaları ─────────────────────────────────────────────────────────

func writeTunnelNotFound(w http.ResponseWriter, subdomain string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="tr">
<head>
  <meta charset="UTF-8">
  <title>Tunnel bulunamadı — tunr</title>
  <style>
    body { font-family: system-ui, sans-serif; background: #080b14; color: #f1f5f9;
           display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
    .box { text-align: center; }
    h1 { font-size: 48px; color: #00d4ff; margin-bottom: 8px; }
    p  { color: #94a3b8; }
    code { background: #0d1220; padding: 4px 8px; border-radius: 4px; color: #00d4ff; }
    a  { color: #00d4ff; }
  </style>
</head>
<body>
  <div class="box">
    <h1>404</h1>
    <p>Tunnel <code>%s</code> bulunamadı veya süresi doldu.</p>
    <p><a href="https://tunr.sh">tunr.sh</a> — yeni tunnel aç</p>
  </div>
</body>
</html>`, subdomain)
}

func writeTunnelGone(w http.ResponseWriter, subdomain string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusGone)
	fmt.Fprintf(w, "Tunnel %s kapatıldı. Yeniden bağlanmak için: tunr share --port <PORT>", subdomain)
}

func writeTunnelError(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadGateway)
	fmt.Fprintf(w, `{"error":"tunnel error","detail":%q}`, reason)
}
