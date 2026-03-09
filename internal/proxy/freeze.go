package proxy

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/tunr-dev/tunr/internal/logger"
)

// cacheEntry — Freeze mode için bir sayfanın önbelleklenmiş hali
type cacheEntry struct {
	Headers    http.Header
	StatusCode int
	Body       []byte
	SavedAt    time.Time
}

// FreezeCache — Proxy çöktüğünde son çalışan versiyonu gösteren bellek içi önbellek.
// Vibecoder demo sırasında sunucuyu çökertirse müşteri hissetmez.
type FreezeCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	enabled bool
}

// NewFreezeCache — yeni bir freeze mode cache oluştur
func NewFreezeCache(enabled bool) *FreezeCache {
	return &FreezeCache{
		entries: make(map[string]*cacheEntry),
		enabled: enabled,
	}
}

// responseRecorder — Freeze cache için yanıtı yakalar
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}
	r.body.Write(p) // Cache için kopyala
	return r.ResponseWriter.Write(p) // İstemciye gönder
}

// Middleware — Freeze mod devredeyse yanıtları cache'ler,
// sunucu hatası (5xx) veya ulaşılamama durumunda cache'den döner.
func (c *FreezeCache) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Sadece GET ve HEAD isteklerini cache'le (state değiştirenler cache'lenmez)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		cacheKey := r.URL.Path + "?" + r.URL.RawQuery

		// Yanıtı yakalamak için recorder oluştur
		rec := &responseRecorder{
			ResponseWriter: w,
			statusCode:     0,
			body:           &bytes.Buffer{},
		}

		// Orijinal handler'ı çağır
		next.ServeHTTP(rec, r)

		// 2xx veya 3xx ise cache'e kaydet
		if rec.statusCode >= 200 && rec.statusCode < 400 {
			// Başarılı yanıt — cache'i güncelle
			c.mu.Lock()
			// Header'ları derin kopyala
			headersCopy := make(http.Header)
			for k, vv := range w.Header() {
				for _, v := range vv {
					headersCopy.Add(k, v)
				}
			}
			
			// Body 5MB'dan büyükse cache'leme (RAM dolmasın)
			if rec.body.Len() < 5*1024*1024 {
				c.entries[cacheKey] = &cacheEntry{
					Headers:    headersCopy,
					StatusCode: rec.statusCode,
					Body:       rec.body.Bytes(), // kopyala
					SavedAt:    time.Now(),
				}
			}
			c.mu.Unlock()
			return
		}

		// Eğer 5xx hatası döndüyse veya sunucu yanıt vermediyse (proxy Bad Gateway attıysa) 
		// RECORDER MÜDAHALE EDEMEZ ÇÜNKÜ ZATEN İSTEMCİYE YAZILMIŞ OLDU.
		// Bu nedenle Caddy/ReverseProxy hata durumunu önceden anlayıp cache'den dönemiyoruz.
		//
		// GERÇEK FREEZE MODE İMPLEMENTASYONU:
		// Reverse proxy hatasında çalışması için Caddy/ReverseProxy Custom Transport yazılır.
	})
}

// ServeFromCache — Reverse proxy hata verdiğinde (Örn: 502 Bad Gateway)
// Doğrudan bu fonksiyon çağrılarak client'a sağlıklı cache dönüşü sağlanır!
func (c *FreezeCache) ServeFromCache(w http.ResponseWriter, r *http.Request) bool {
	if !c.enabled || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}

	cacheKey := r.URL.Path + "?" + r.URL.RawQuery

	c.mu.RLock()
	entry, exists := c.entries[cacheKey]
	c.mu.RUnlock()

	if !exists {
		return false // Cache'te yok
	}

	// Müşteri "sunucu çöktü" paniği yaşamaz!
	logger.Warn("FREEZE MODE: Localhost çöktü, %s isteği cache'den sunuluyor!", r.URL.Path)

	for k, vv := range entry.Headers {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	// Freeze mode cache isareti
	w.Header().Set("X-Tunr-Freeze-Cache", "HIT")
	w.Header().Set("X-Cache-Saved-At", entry.SavedAt.Format(time.RFC3339))
	
	w.WriteHeader(entry.StatusCode)
	io.Copy(w, bytes.NewReader(entry.Body))
	return true
}
