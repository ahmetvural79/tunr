package relay

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Registry — aktif tunnel kayıt defteri.
// Kim hangi subdomain'i tutuyor? Kim bağlı, kim değil?
// Thread-safe olması şart — aynı anda yüzlerce tunnel olabilir.

// TunnelEntry — kayıtlı bir tunnel'ın tam kaydı
type TunnelEntry struct {
	ID        string
	UserID    string
	Subdomain string
	LocalPort int

	Requests chan *TunnelRequest
	Done     chan struct{}

	ConnectedAt time.Time
	LastPingAt  time.Time

	mu              sync.Mutex
	pendingRequests map[string]*TunnelRequest // requestID → waiting request
}

// TunnelRequest — relay'in tunnel client'ına ilettiği HTTP isteği
type TunnelRequest struct {
	ID       string
	Method   string
	Path     string
	Headers  http.Header
	Body     []byte
	Response chan *TunnelResponse // cevap buraya yazılır
}

// TunnelResponse — tunnel client'ının relay'e geri gönderdiği cevap
type TunnelResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	Err        error
}

// Registry — thread-safe tunnel kayıt defteri
type Registry struct {
	mu          sync.RWMutex
	tunnels     map[string]*TunnelEntry // key: tunnelID
	bySubdomain map[string]*TunnelEntry // key: subdomain (quick lookup)
}

// NewRegistry — boş kayıt defteri oluştur
func NewRegistry() *Registry {
	r := &Registry{
		tunnels:     make(map[string]*TunnelEntry),
		bySubdomain: make(map[string]*TunnelEntry),
	}

	// Ölü tunnel'ları temizlemek için background goroutine
	go r.cleanupLoop()
	return r
}

// Register — yeni tunnel kayıt et ve bir ID/subdomain ver
func (r *Registry) Register(userID string, preferredSubdomain string) (*TunnelEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Subdomain belirle
	subdomain := preferredSubdomain
	if subdomain == "" {
		// Rastgele 8 karakterlik subdomain üret
		// Kısa ama çakışma ihtimali düşük
		subdomain = generateSubdomain()
	}

	// Subdomain zaten alınmış mı?
	if _, exists := r.bySubdomain[subdomain]; exists {
		if preferredSubdomain != "" {
			return nil, fmt.Errorf("subdomain '%s' zaten kullanımda", subdomain)
		}
		// Rastgele oluşturduysak tekrar dene
		subdomain = generateSubdomain()
	}

	id := uuid.New().String()[:8]
	entry := &TunnelEntry{
		ID:              id,
		UserID:          userID,
		Subdomain:       subdomain,
		Requests:        make(chan *TunnelRequest, 16),
		Done:            make(chan struct{}),
		ConnectedAt:     time.Now(),
		LastPingAt:      time.Now(),
		pendingRequests: make(map[string]*TunnelRequest),
	}

	r.tunnels[id] = entry
	r.bySubdomain[subdomain] = entry

	return entry, nil
}

// Lookup — subdomain'e göre tunnel bul
// HTTP isteği gelince relay buna bakıyor
func (r *Registry) Lookup(subdomain string) (*TunnelEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.bySubdomain[subdomain]
	return t, ok
}

// LookupByID — ID'ye göre tunnel bul
func (r *Registry) LookupByID(id string) (*TunnelEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tunnels[id]
	return t, ok
}

// Unregister — tunnel'ı kayıt defterinden sil
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.tunnels[id]
	if !ok {
		return
	}

	// Done kanalını kapat — bekleyen goroutineler haberdar olsun
	select {
	case <-entry.Done:
		// Zaten kapalı
	default:
		close(entry.Done)
	}

	delete(r.tunnels, id)
	delete(r.bySubdomain, entry.Subdomain)
}

// UpdatePing — tunnel'ın son ping zamanını güncelle (heartbeat)
func (r *Registry) UpdatePing(id string) {
	r.mu.RLock()
	entry, ok := r.tunnels[id]
	r.mu.RUnlock()
	if !ok {
		return
	}
	entry.mu.Lock()
	entry.LastPingAt = time.Now()
	entry.mu.Unlock()
}

// Stats — kayıt defteri istatistikleri
func (r *Registry) Stats() map[string]interface{} {
	r.mu.RLock()
	count := len(r.tunnels)
	r.mu.RUnlock()
	return map[string]interface{}{
		"active_tunnels": count,
	}
}

func (r *Registry) ListByUser(userID string) []*TunnelEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*TunnelEntry, 0)
	for _, tunnelEntry := range r.tunnels {
		if tunnelEntry.UserID != userID {
			continue
		}
		result = append(result, tunnelEntry)
	}
	return result
}

// cleanupLoop — 30 saniyedir ping almayan tunnel'ları temizle
// GÜVENLİK: Zombie tunnel'lar subdomain'i bloklamamalı
func (r *Registry) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		r.mu.Lock()
		deadline := time.Now().Add(-2 * time.Minute) // 2 dakika timeout
		var stale []string
		for id, entry := range r.tunnels {
			entry.mu.Lock()
			if entry.LastPingAt.Before(deadline) {
				stale = append(stale, id)
			}
			entry.mu.Unlock()
		}
		for _, id := range stale {
			entry := r.tunnels[id]
			select {
			case <-entry.Done:
			default:
				close(entry.Done)
			}
			delete(r.tunnels, id)
			delete(r.bySubdomain, entry.Subdomain)
		}
		r.mu.Unlock()
	}
}

// generateSubdomain — rastgele 8 karakterlik subdomain üret
// "abc1x2y3" gibi bir şey — okunabilir ama tahmin edilemez
func generateSubdomain() string {
	id := uuid.New().String()
	// UUID'den sadece alfanümerik karakterleri al, ilk 8'i kullan
	var result []byte
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result = append(result, byte(c))
		}
		if len(result) == 8 {
			break
		}
	}
	return string(result)
}

// IsAlive — tunnel hala aktif mi?
func (e *TunnelEntry) IsAlive() bool {
	select {
	case <-e.Done:
		return false
	default:
		return true
	}
}

// PublicURL — tunnel'ın public URL'si
func (e *TunnelEntry) PublicURL(domain string) string {
	return fmt.Sprintf("https://%s.%s", e.Subdomain, domain)
}

// ForwardRequest queues the request for the CLI, tracks it in pendingRequests,
// then waits for the CLI to respond (or timeout).
func (e *TunnelEntry) ForwardRequest(req *TunnelRequest) (*TunnelResponse, error) {
	e.mu.Lock()
	e.pendingRequests[req.ID] = req
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.pendingRequests, req.ID)
		e.mu.Unlock()
	}()

	select {
	case <-e.Done:
		return nil, fmt.Errorf("tunnel kapalı")
	case e.Requests <- req:
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("tunnel meşgul (istek kuyruğu dolu)")
	}

	select {
	case resp := <-req.Response:
		return resp, resp.Err
	case <-e.Done:
		return nil, fmt.Errorf("tunnel bağlantı kesildi")
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("upstream timeout (30s)")
	}
}

// ResolveResponse routes a CLI response back to the waiting HTTP handler.
func (e *TunnelEntry) ResolveResponse(requestID string, resp *TunnelResponse) {
	e.mu.Lock()
	req, ok := e.pendingRequests[requestID]
	e.mu.Unlock()
	if !ok {
		return
	}
	select {
	case req.Response <- resp:
	default:
	}
}

// dummyNetListener — interface doyumu için (kullanılmıyor ama ileride lazım olabilir)
var _ net.Listener = nil
