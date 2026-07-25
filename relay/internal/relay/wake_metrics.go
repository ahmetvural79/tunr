package relay

// wake_metrics.go — wake latency + state-transition telemetry (Faz 0).
//
// "Ölçmeden değiştirme": every density lever in the plan is judged by what it
// does to wake latency, and the Faz 2 checkpoint/restore gate is literally a
// number from this file (restore p95 < 600 ms, fallback rate < 2%). Without it
// the whole programme is guesswork.
//
// Implementation note: this is a fixed-bucket histogram, not a reservoir. Wake
// latency spans milliseconds to tens of seconds, we only ever read percentiles
// at bucket granularity, and a bounded array means the metric can never become
// the memory problem it exists to measure.

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// wakeBuckets are upper bounds in milliseconds. Resolution is deliberately
// densest between 100 ms and 1 s — that's the band where WARM resume, the Faz 2
// checkpoint restore target (~200-400 ms) and the "user notices" threshold
// (~800 ms) all live, so it's where a percentile has to be trustworthy.
var wakeBuckets = []float64{
	5, 10, 25, 50, 75, 100, 150, 200, 300, 400, 600, 800,
	1000, 1500, 2000, 3000, 5000, 8000, 15000, 30000,
}

// WakeSource is the state an app woke from — the dimension that matters,
// because "wake p95" means nothing if you can't tell a 20 ms unpause from a
// 3 s cold boot.
type WakeSource string

const (
	// WakeFromWarm is an unpause: pages come back from zram, no disk, no boot.
	WakeFromWarm WakeSource = "warm"
	// WakeFromCold is a container start: full application boot.
	WakeFromCold WakeSource = "cold"
	// WakeFromProbe is a dial that succeeded without any wake — the app was
	// already up. Recorded so the ratio of "already hot" is observable, which
	// is what predictive pre-warm (L8) will be measured against.
	WakeFromProbe WakeSource = "hot"
)

type histogram struct {
	mu       sync.Mutex
	counts   []uint64
	overflow uint64
	sum      float64
	n        uint64
}

func newHistogram() *histogram { return &histogram{counts: make([]uint64, len(wakeBuckets))} }

func (h *histogram) observe(ms float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	h.sum += ms
	i := sort.SearchFloat64s(wakeBuckets, ms)
	if i >= len(wakeBuckets) {
		h.overflow++
		return
	}
	h.counts[i]++
}

// quantile returns the upper bound of the bucket containing the q-th
// percentile. Reported values are therefore rounded up to a bucket edge —
// adequate for an SLO, not for billing.
func (h *histogram) quantile(q float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n == 0 {
		return 0
	}
	target := q * float64(h.n)
	var cum float64
	for i, c := range h.counts {
		cum += float64(c)
		if cum >= target {
			return wakeBuckets[i]
		}
	}
	return wakeBuckets[len(wakeBuckets)-1] // everything above the last bucket
}

func (h *histogram) snapshot() map[string]any {
	h.mu.Lock()
	n, sum := h.n, h.sum
	h.mu.Unlock()
	if n == 0 {
		return map[string]any{"count": 0}
	}
	return map[string]any{
		"count":   n,
		"mean_ms": sum / float64(n),
		"p50_ms":  h.quantile(0.50),
		"p95_ms":  h.quantile(0.95),
		"p99_ms":  h.quantile(0.99),
	}
}

// WakeMetrics aggregates wake outcomes across all cloud apps.
type WakeMetrics struct {
	hist map[WakeSource]*histogram // fixed key set — no locking needed after construction

	successes atomic.Uint64
	failures  atomic.Uint64 // wake budget exceeded — the user saw a 503
	fallbacks atomic.Uint64 // reserved for Faz 2: restore failed → cold start

	probesServed atomic.Uint64 // synthetic health answers for sleeping apps
	pinnedNow    atomic.Int64  // open WebSocket/SSE connections across all apps
}

// NewWakeMetrics returns an initialised collector.
func NewWakeMetrics() *WakeMetrics {
	return &WakeMetrics{hist: map[WakeSource]*histogram{
		WakeFromWarm:  newHistogram(),
		WakeFromCold:  newHistogram(),
		WakeFromProbe: newHistogram(),
	}}
}

// Observe records one wake attempt.
func (m *WakeMetrics) Observe(src WakeSource, d time.Duration, ok bool) {
	if m == nil {
		return
	}
	if h := m.hist[src]; h != nil {
		h.observe(float64(d.Microseconds()) / 1000)
	}
	if ok {
		m.successes.Add(1)
	} else {
		m.failures.Add(1)
	}
}

// ObserveFallback records a restore that had to fall back to a cold start.
// Unused until Faz 2; wired now so the gate metric exists before it's needed.
func (m *WakeMetrics) ObserveFallback() {
	if m != nil {
		m.fallbacks.Add(1)
	}
}

// ObserveProbe records a synthetic health response served for a sleeping app.
func (m *WakeMetrics) ObserveProbe() {
	if m != nil {
		m.probesServed.Add(1)
	}
}

// PinDelta adjusts the count of open long-lived connections.
func (m *WakeMetrics) PinDelta(d int64) {
	if m != nil {
		m.pinnedNow.Add(d)
	}
}

// Snapshot renders the metrics for the stats endpoint.
func (m *WakeMetrics) Snapshot() map[string]any {
	if m == nil {
		return map[string]any{"enabled": false}
	}
	succ, fail := m.successes.Load(), m.failures.Load()
	total := succ + fail
	var failRate float64
	if total > 0 {
		failRate = float64(fail) / float64(total) * 100
	}
	return map[string]any{
		"warm":              m.hist[WakeFromWarm].snapshot(),
		"cold":              m.hist[WakeFromCold].snapshot(),
		"hot":               m.hist[WakeFromProbe].snapshot(),
		"successes":         succ,
		"failures":          fail,
		"fail_rate_percent": failRate,
		"fallbacks":         m.fallbacks.Load(),
		"probes_served":     m.probesServed.Load(),
		"pinned_now":        m.pinnedNow.Load(),
	}
}
