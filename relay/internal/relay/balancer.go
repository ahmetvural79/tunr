package relay

import (
	"sort"
	"sync"
	"time"
)

// Balancer — çoklu bölge yük dengeleme.
//
// tunr relay birden fazla Fly.io bölgesinde çalışabilir:
//   ams (Amsterdam) — Avrupa, Türkiye
//   sea (Seattle)   — Kuzey Amerika batı
//   sin (Singapur)  — Asya-Pasifik
//
// Client bağlanırken en az yüklü bölgeye yönlendirilir.
// (Fly.io anycast kendi yapar, bu balancer in-region load distributor)

// RegionMetrics — bir bölgenin anlık metrikleri
type RegionMetrics struct {
	Region        string
	ActiveTunnels int
	ReqPerSecond  float64
	AvgLatencyMs  float64
	Healthy       bool
	LastUpdated   time.Time
}

// Balancer — bölge metriklerini takip eden load balancer
type Balancer struct {
	mu      sync.RWMutex
	regions map[string]*RegionMetrics
}

// NewBalancer — yeni balancer (bölgelerle başlat)
func NewBalancer(regions []string) *Balancer {
	b := &Balancer{
		regions: make(map[string]*RegionMetrics, len(regions)),
	}
	for _, r := range regions {
		b.regions[r] = &RegionMetrics{
			Region:      r,
			Healthy:     true,
			LastUpdated: time.Now(),
		}
	}
	return b
}

// UpdateMetrics — bölge metriklerini güncelle
// (relay sunucu her 10s'de bir kendi metriklerini broadcast eder)
func (b *Balancer) UpdateMetrics(region string, tunnels int, rps float64, latencyMs float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	m, ok := b.regions[region]
	if !ok {
		m = &RegionMetrics{Region: region}
		b.regions[region] = m
	}
	m.ActiveTunnels = tunnels
	m.ReqPerSecond = rps
	m.AvgLatencyMs = latencyMs
	m.Healthy = true
	m.LastUpdated = time.Now()
}

// MarkUnhealthy — sağlık kontrolü başarısız olan bölgeyi işaretle
func (b *Balancer) MarkUnhealthy(region string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m, ok := b.regions[region]; ok {
		m.Healthy = false
	}
}

// BestRegion — en az yüklü, sağlıklı bölgeyi döndür
// Öncelik sırası: sağlıklı → en az tunnel → en düşük RPS → en düşük latency
func (b *Balancer) BestRegion() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var candidates []*RegionMetrics
	for _, m := range b.regions {
		if m.Healthy {
			candidates = append(candidates, m)
		}
	}

	if len(candidates) == 0 {
		// Hepsi sağlıksız — en son güncellenen birini dene
		for _, m := range b.regions {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return "ams" // varsayılan
	}

	// Sırala: en az tunnel, sonra RPS
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ActiveTunnels != candidates[j].ActiveTunnels {
			return candidates[i].ActiveTunnels < candidates[j].ActiveTunnels
		}
		return candidates[i].ReqPerSecond < candidates[j].ReqPerSecond
	})

	return candidates[0].Region
}

// AllMetrics — tüm bölge metriklerini döndür (status endpoint için)
func (b *Balancer) AllMetrics() []RegionMetrics {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]RegionMetrics, 0, len(b.regions))
	for _, m := range b.regions {
		result = append(result, *m)
	}

	// Alfabetik sıra — kararlı çıktı
	sort.Slice(result, func(i, j int) bool {
		return result[i].Region < result[j].Region
	})
	return result
}

// StaleCheck — 2 dakikadır güncellenmemiş bölgeleri sağlıksız say
func (b *Balancer) StaleCheck() {
	b.mu.Lock()
	defer b.mu.Unlock()
	deadline := time.Now().Add(-2 * time.Minute)
	for _, m := range b.regions {
		if m.LastUpdated.Before(deadline) {
			m.Healthy = false
		}
	}
}
