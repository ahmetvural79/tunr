package proxy

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
)

type cacheEntry struct {
	Headers    http.Header
	StatusCode int
	Body       []byte
	SavedAt    time.Time
}

// FreezeCache is an in-memory snapshot of last-known-good responses.
// When the local server crashes mid-demo, clients keep seeing a working app.
type FreezeCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	enabled bool
}

// NewFreezeCache creates a new freeze mode cache.
func NewFreezeCache(enabled bool) *FreezeCache {
	return &FreezeCache{
		entries: make(map[string]*cacheEntry),
		enabled: enabled,
	}
}

// responseRecorder tees response bytes into a buffer for cache storage.
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
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
}

// Middleware caches successful responses and serves them back
// when the upstream goes belly-up (5xx or unreachable).
func (c *FreezeCache) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Only cache GET/HEAD — don't cache state-mutating requests
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		cacheKey := r.URL.Path + "?" + r.URL.RawQuery

		// Wrap the writer to capture the response
		rec := &responseRecorder{
			ResponseWriter: w,
			statusCode:     0,
			body:           &bytes.Buffer{},
		}

		// Let the real handler do its thing
		next.ServeHTTP(rec, r)

		if rec.statusCode >= 200 && rec.statusCode < 400 {
			c.mu.Lock()
			// Deep-copy headers so we own the data
			headersCopy := make(http.Header)
			for k, vv := range w.Header() {
				for _, v := range vv {
					headersCopy.Add(k, v)
				}
			}

			// Skip caching bodies over 5MB — we're not Redis
			if rec.body.Len() < 5*1024*1024 {
				c.entries[cacheKey] = &cacheEntry{
					Headers:    headersCopy,
					StatusCode: rec.statusCode,
					Body:       rec.body.Bytes(),
					SavedAt:    time.Now(),
				}
			}
			c.mu.Unlock()
			return
		}

		// On 5xx the response is already written to the client — the recorder
		// can't un-send bytes. The real freeze magic happens in the ErrorHandler
		// hook on the reverse proxy (see BuildMiddlewareChain).
	})
}

// ServeFromCache is the fallback when the reverse proxy returns an error (e.g. 502).
// Called directly from the ErrorHandler to serve the last-known-good response.
func (c *FreezeCache) ServeFromCache(w http.ResponseWriter, r *http.Request) bool {
	if !c.enabled || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}

	cacheKey := r.URL.Path + "?" + r.URL.RawQuery

	c.mu.RLock()
	entry, exists := c.entries[cacheKey]
	c.mu.RUnlock()

	if !exists {
		return false
	}

	logger.Warn("FREEZE MODE: Localhost is down, serving %s from cache!", r.URL.Path)

	for k, vv := range entry.Headers {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	// Tag the response so devtools can spot it
	w.Header().Set("X-Tunr-Freeze-Cache", "HIT")
	w.Header().Set("X-Cache-Saved-At", entry.SavedAt.Format(time.RFC3339))

	w.WriteHeader(entry.StatusCode)
	io.Copy(w, bytes.NewReader(entry.Body))
	return true
}
