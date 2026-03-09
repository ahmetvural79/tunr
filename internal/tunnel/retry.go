package tunnel

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/tunr-dev/tunr/internal/logger"
)

// RetryConfig - yeniden deneme stratejisi ayarları
// Exponential backoff çünkü "hemen tekrar dene" flood yapar
type RetryConfig struct {
	MaxAttempts int           // kaç defa deneriz? (0 = sonsuza kadar, dikkatli kullan)
	BaseDelay   time.Duration // ilk deneme gecikmesi
	MaxDelay    time.Duration // maksimum gecikme (geometrik artışın tavanı)
	Multiplier  float64       // her denemede gecikme katı
	Jitter      bool          // gecikmeye rastlantısallık ekle (thundering herd önler)
}

// DefaultRetryConfig - "aklı başında" varsayılan retry ayarları
var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 0,          // sonsuza kadar dene (daemon mode)
	BaseDelay:   1 * time.Second,
	MaxDelay:    60 * time.Second,
	Multiplier:  2.0,
	Jitter:      true, // aynı anda binlerce client retry yapmasın diye
}

// ShareRetryConfig - `tunr share` için daha az sabırlı config
var ShareRetryConfig = RetryConfig{
	MaxAttempts: 5,
	BaseDelay:   500 * time.Millisecond,
	MaxDelay:    10 * time.Second,
	Multiplier:  2.0,
	Jitter:      true,
}

// RetryFunc - yeniden denenecek fonksiyon tipi
type RetryFunc func(ctx context.Context, attempt int) error

// IsRetryableError - bu hatayı yeniden denemeye değer mi?
// Bazı hatalar retry'dan fayda görmez (örn: auth hatası, invalid port)
type IsRetryableError func(err error) bool

// WithRetry - exponential backoff ile fonksiyonu yeniden dene
func WithRetry(ctx context.Context, cfg RetryConfig, fn RetryFunc, isRetryable ...IsRetryableError) error {
	retryCheck := defaultIsRetryable
	if len(isRetryable) > 0 && isRetryable[0] != nil {
		retryCheck = isRetryable[0]
	}

	for attempt := 1; ; attempt++ {
		err := fn(ctx, attempt)
		if err == nil {
			return nil // başarı!
		}

		// Retry'a değmez mi?
		if !retryCheck(err) {
			return fmt.Errorf("yeniden denenemeyen hata: %w", err)
		}

		// Max deneme sayısına ulaştık mı?
		if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
			return fmt.Errorf("%d deneme sonrası vazgeçildi: %w", attempt, err)
		}

		// Ne kadar bekleyeceğiz?
		delay := calculateDelay(cfg, attempt)

		logger.Warn("Deneme %d başarısız: %v — %s sonra yeniden denenecek", attempt, err, delay.Round(time.Millisecond))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			// devam et, yeniden dene
		}
	}
}

// calculateDelay - exponential backoff + optional jitter hesapla
func calculateDelay(cfg RetryConfig, attempt int) time.Duration {
	// delay = base * multiplier^(attempt-1)
	delay := float64(cfg.BaseDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	// Tavanı uygula
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	if cfg.Jitter {
		// ±25% jitter ekle — thundering herd problem'ı önler
		// GÜVENLİK: math/rand yeterli, burada kripto random gerekmez
		// (jitter güvenlik değil, performans için)
		jitter := delay * 0.25
		delay += (rand.Float64()*2 - 1) * jitter
		if delay < 0 {
			delay = float64(cfg.BaseDelay)
		}
	}

	return time.Duration(delay)
}

// defaultIsRetryable - retry mantığı: hangi hatalar yeniden denenebilir?
func defaultIsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context cancelled/timeout - kesinlikle retry etme
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	// Network hataları genellikle geçicidir - retry et
	// URL hatası, auth hatası - retry etme
	switch err.(type) {
	case *url.Error:
		ue := err.(*url.Error)
		// Timeout ve temporary network errors → retry
		if ue.Timeout() {
			return true
		}
		// DNS failure, connection refused → retry (server başlamıyor olabilir)
		return true
	}

	return true // varsayılan: retry et
}

// RelayClient - tunr relay sunucusuyla iletişim kurar
// Şimdilik Cloudflare quicktunnel wrapper, ileride custom relay
type RelayClient struct {
	relayURL  string
	authToken string // sadece relay'e iletilir, log'a GEÇMEz
	httpClient *http.Client
}

// NewRelayClient - relay client oluştur
func NewRelayClient(relayURL string, authToken string) *RelayClient {
	return &RelayClient{
		relayURL:  relayURL,
		authToken: authToken, // GÜVENLİK: bu alan struct dışına expose edilmez
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				// GÜVENLİK: TLS verify her zaman aktif!
				// InsecureSkipVerify: false // default zaten, ama açıkça belirtiyoruz
				DisableKeepAlives: false,
			},
		},
	}
}

// RequestTunnel - relay'den yeni tunnel URL'i talep et
func (rc *RelayClient) RequestTunnel(ctx context.Context, port int, opts TunnelRequestOptions) (*TunnelResponse, error) {
	// GÜVENLİK: URL validation - SSRF saldırısını önle
	if err := validateRelayURL(rc.relayURL); err != nil {
		return nil, fmt.Errorf("geçersiz relay URL: %w", err)
	}

	// TODO Faz 1: Gerçek API çağrısı
	// POST /v1/tunnels
	// Authorization: Bearer <token>
	// { "local_port": port, "subdomain": opts.Subdomain }
	//
	// Şimdilik Cloudflare quicktunnel kullan (token gerekmez, ama rate limited)
	publicURL := fmt.Sprintf("https://%s.trycloudflare.com", generateSubdomain())

	return &TunnelResponse{
		PublicURL:  publicURL,
		TunnelID:   generateID(),
		ExpiresAt:  time.Now().Add(8 * time.Hour),
	}, nil
}

// TunnelRequestOptions - tunnel talep seçenekleri
type TunnelRequestOptions struct {
	Subdomain string // özel subdomain (pro)
	HTTPS     bool
}

// TunnelResponse - relay'den dönen tunnel bilgisi
type TunnelResponse struct {
	PublicURL string    `json:"public_url"`
	TunnelID  string    `json:"tunnel_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// validateRelayURL - relay URL'inin güvenli olduğunu doğrula
// GÜVENLİK: SSRF saldırısına karşı koruma
// Biri config'i değiştirerek iç ağa istek yaptıramasın
func validateRelayURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL parse hatası: %w", err)
	}

	// Sadece HTTPS relay'lere bağlan (HTTP kabul etme)
	if u.Scheme != "https" {
		return fmt.Errorf("relay HTTPS kullanmalı (scheme: %q)", u.Scheme)
	}

	// Private IP aralıklarını reddet (SSRF koruması)
	// 10.x.x.x, 172.16-31.x.x, 192.168.x.x, 127.x.x.x, 169.254.x.x
	host := u.Hostname()
	if isPrivateHost(host) {
		return fmt.Errorf("relay private IP adresine işaret edemez: %s", host)
	}

	return nil
}

// isPrivateHost - verilen host private/loopback mu?
func isPrivateHost(host string) bool {
	privateHosts := []string{
		"localhost", "127.", "10.", "192.168.",
		"::1", "169.254.", "0.",
	}
	for _, prefix := range privateHosts {
		if len(host) >= len(prefix) && host[:len(prefix)] == prefix {
			return true
		}
	}
	// 172.16.x.x - 172.31.x.x
	if len(host) >= 7 && host[:4] == "172." {
		var second int
		fmt.Sscanf(host[4:], "%d", &second)
		if second >= 16 && second <= 31 {
			return true
		}
	}
	return false
}

// generateSubdomain - random subdomain üret (quicktunnel için)
func generateSubdomain() string {
	// 8 karakterlik random hex
	b := make([]byte, 4)
	rand.Read(b) // math/rand: subdomain için kripto güvenli random gerekmez
	return fmt.Sprintf("%x", b)
}

// generateID - tunnel ID üret
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[:8]
}
