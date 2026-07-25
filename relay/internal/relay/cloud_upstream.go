package relay

// cloud_upstream.go — the "cloud" upstream for the relay.
//
// Today the relay maps  subdomain -> live tunnel session (WebSocket to CLI).
// This adds a second upstream kind:  subdomain -> CloudUpstream, which
// reverse-proxies to a persistent app container/machine and knows how to WAKE it.
//
// Pattern: "probe, wake, then proxy". We never retry a proxied request
// (no body-replay problems); instead we TCP-probe the target first, call
// Waker.Wake() on failure, and only hand the request to ReverseProxy once
// the target is dialable. httputil.ReverseProxy handles WebSocket upgrades.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// Waker abstracts "make this app dialable". runner.DockerDriver implements it on
// the own-server path; on Fly it's a no-op (Fly Proxy wakes machines itself).
// Defined as a local interface so package relay doesn't hard-depend on runner.
type Waker interface {
	// Wake makes the app dialable and returns its CURRENT IP (may be empty if
	// unknown). The IP can change across a cold stop→start, so callers update
	// their target with it.
	Wake(ctx context.Context, appID string) (ip string, err error)
}

// FreezeCache is implemented by a freeze-mode store: last good response per
// (host, path) — reused here to mask redeploy gaps and wake failures.
// Wired in Faz 1 (relay-side freeze); nil-safe until then.
type FreezeCache interface {
	Serve(w http.ResponseWriter, r *http.Request) bool // true if served from cache
	Store(r *http.Request, status int, header http.Header, body []byte)
}

// SleepState mirrors the lifecycle state the sweeper last drove the app into.
// The relay tracks it because the wake path differs per state — and because a
// paused container still completes a TCP handshake in the kernel, so probing
// alone can never tell us the app is frozen.
type SleepState int32

const (
	// SleepAwake — HOT: serving normally.
	SleepAwake SleepState = iota
	// SleepWarm — paused + pages reclaimed into zram. Resume is a decompress.
	SleepWarm
	// SleepStopped — container exited. Waking is a full cold boot, and the
	// container's IP changes, so the target has to be re-pointed.
	SleepStopped
)

// CloudUpstream reverse-proxies a subdomain to a persistent app, waking it on demand.
type CloudUpstream struct {
	AppID       string
	Target      *url.URL      // http://tunr-app-x:8080 (own-server) or http://tunr-a-x.flycast:80 (fly)
	EdgeSecret  []byte        // HMAC key; tunr-shim in the container verifies X-Tunr-Edge
	WakeTimeout time.Duration // total budget for probe+wake, e.g. 30s
	Waker       Waker
	Freeze      FreezeCache // optional (Faz 1)
	Metrics     *WakeMetrics

	initOnce sync.Once
	proxy    *httputil.ReverseProxy
	targetMu sync.RWMutex // guards Target.Host, which can move across a cold restart
	// lastSeen feeds the idle sweeper (reads it to Sleep/Stop apps). Only
	// ActivityNormal/ActivityPin requests update it — see activity.go.
	lastSeenMu sync.Mutex
	lastSeen   time.Time
	// state is the sweeper's last known lifecycle state for this app.
	state atomic.Int32
	// pins counts open long-lived connections (WebSocket/SSE). While non-zero
	// the sweeper must not put the app to sleep: freezing a container mid-stream
	// hangs every attached client.
	pins atomic.Int64
}

// SetSleepState records the exact state the sweeper drove the app into.
func (u *CloudUpstream) SetSleepState(s SleepState) { u.state.Store(int32(s)) }

// SleepState reports the app's last known lifecycle state.
func (u *CloudUpstream) SleepState() SleepState { return SleepState(u.state.Load()) }

// Pinned reports whether a long-lived connection is currently open. The sweeper
// skips pinned apps entirely.
func (u *CloudUpstream) Pinned() bool { return u.pins.Load() > 0 }

// NewCloudUpstream builds a CloudUpstream with a 30s default wake budget.
func NewCloudUpstream(appID string, target *url.URL, secret []byte, wake Waker, freeze FreezeCache) *CloudUpstream {
	return &CloudUpstream{
		AppID: appID, Target: target, EdgeSecret: secret,
		WakeTimeout: 30 * time.Second, Waker: wake, Freeze: freeze,
	}
}

func (u *CloudUpstream) init() {
	u.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			u.targetMu.RLock()
			target := *u.Target
			u.targetMu.RUnlock()
			pr.SetURL(&target)
			pr.SetXForwarded()
			// Keep the PUBLIC host so the app sees its real URL
			// (myapp.tunr.sh), not the container IP.
			pr.Out.Host = pr.In.Host
			// Signed edge header — proves the request came through the relay.
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			pr.Out.Header.Set("X-Tunr-Edge",
				"t="+ts+", s="+signEdge(u.EdgeSecret, ts, pr.In.Host, pr.In.URL.Path))
		},
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second, // slow first render on cold app
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if u.Freeze != nil && u.Freeze.Serve(w, r) {
				w.Header().Set("X-Tunr-Frozen", "1")
				return
			}
			http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		},
		// TODO(Faz1): ModifyResponse -> u.Freeze.Store(...) for cacheable GET 200s
		// (mind body buffering limits; reuse existing freeze-mode code).
	}
}

// signEdge = hex(HMAC-SHA256(secret, ts|host|path)). tunr-shim recomputes and
// rejects requests without a valid, recent signature — this is what closes the
// "neighbor container calls my app directly" hole on a shared network.
func signEdge(secret []byte, ts, host, path string) string {
	m := hmac.New(sha256.New, secret)
	fmt.Fprintf(m, "%s|%s|%s", ts, host, path)
	return hex.EncodeToString(m.Sum(nil))
}

// ServeHTTP: ensure target is dialable (waking it if needed), then proxy once.
func (u *CloudUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.initOnce.Do(u.init)

	class := ClassifyActivity(r)

	// A monitor polling a sleeping app is answered here, at the edge. Waking a
	// container so it can say "ok" to a robot every 30s is how scale-to-zero
	// silently stops working (plan §1.2 / E6).
	if class == ActivityProbe && u.SleepState() != SleepAwake {
		u.Metrics.ObserveProbe()
		writeSyntheticHealth(w, u.AppID)
		return
	}

	// Probes never reset the idle clock, even when the app is already awake —
	// otherwise a 30s monitor keeps a 45s idle threshold permanently out of reach.
	if class != ActivityProbe {
		u.touch()
	}

	// A pin forbids sleep for as long as the connection is open. Held across the
	// whole proxy call, which for a WebSocket is the lifetime of the socket.
	if class == ActivityPin {
		u.pins.Add(1)
		u.Metrics.PinDelta(1)
		defer func() {
			u.pins.Add(-1)
			u.Metrics.PinDelta(-1)
		}()
	}

	ctx, cancel := context.WithTimeout(r.Context(), u.WakeTimeout)
	defer cancel()

	if err := u.ensureDialable(ctx); err != nil {
		// Last resort: frozen copy beats an error page.
		if u.Freeze != nil && u.Freeze.Serve(w, r) {
			w.Header().Set("X-Tunr-Frozen", "1")
			return
		}
		// TODO(v0.1): friendly "Waking your app…" HTML with auto-refresh.
		http.Error(w, "app is waking up, try again in a few seconds", http.StatusServiceUnavailable)
		return
	}
	u.proxy.ServeHTTP(w, r)
}

// ensureDialable probes TCP; on failure asks the Waker once, then keeps
// probing with backoff until the budget runs out.
//
// It also times the whole path and attributes it to the state the app was in,
// because an unattributed "wake p95" is useless: a 20 ms unpause and a 3 s cold
// boot are the same event to a timer but completely different products.
func (u *CloudUpstream) ensureDialable(ctx context.Context) error {
	start := time.Now()
	prior := u.SleepState()

	// Attribute the sample to where the app started, not where it ended up.
	source := WakeFromProbe
	switch prior {
	case SleepWarm:
		source = WakeFromWarm
	case SleepStopped:
		source = WakeFromCold
	}
	recorded := false
	record := func(ok bool) {
		if !recorded {
			u.Metrics.Observe(source, time.Since(start), ok)
			recorded = true
		}
	}

	probe := func() error {
		u.targetMu.RLock()
		addr := u.Target.Host
		u.targetMu.RUnlock()
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = c.Close()
		}
		return err
	}

	// If the sweeper paused/stopped this app, wake it first — a paused container
	// still completes the TCP handshake in the kernel, so probe success alone
	// would never trigger a wake and the request would hang on the frozen process.
	if prior != SleepAwake && u.Waker != nil {
		wctx, wcancel := context.WithTimeout(ctx, 12*time.Second)
		if ip, err := u.Waker.Wake(wctx, u.AppID); err != nil {
			logger.Warn("cloud pre-wake %s: %v", u.AppID, err)
		} else {
			u.updateHost(ip)
		}
		wcancel()
		u.SetSleepState(SleepAwake)
	}

	if probe() == nil {
		record(true)
		return nil
	}
	if u.Waker != nil {
		// The dial failed while we believed the app was up — it died, or was
		// stopped out from under us. Either way this is a cold path now.
		if prior == SleepAwake {
			source = WakeFromCold
		}
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		if ip, err := u.Waker.Wake(wctx, u.AppID); err != nil {
			// Log but keep probing — the unit may still be coming up.
			logger.Warn("cloud wake %s: %v", u.AppID, err)
		} else {
			u.updateHost(ip)
		}
		wcancel()
	}
	backoff := 300 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			record(false)
			return fmt.Errorf("wake budget exceeded for %s", u.AppID)
		case <-time.After(backoff):
		}
		if probe() == nil {
			record(true)
			return nil
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}

// updateHost swaps the target's IP (keeping the port) after a cold restart moved
// it. Safe under concurrent proxying via targetMu.
func (u *CloudUpstream) updateHost(newIP string) {
	if newIP == "" {
		return
	}
	u.targetMu.Lock()
	defer u.targetMu.Unlock()
	_, port, err := net.SplitHostPort(u.Target.Host)
	if err != nil || port == "" {
		port = "8080"
	}
	nh := net.JoinHostPort(newIP, port)
	if u.Target.Host != nh {
		u.Target.Host = nh
		logger.Info("cloud %s: endpoint → %s", u.AppID, nh)
	}
}

func (u *CloudUpstream) touch() {
	u.lastSeenMu.Lock()
	u.lastSeen = time.Now()
	u.lastSeenMu.Unlock()
}

// LastSeen is read by the idle sweeper: if now-LastSeen > 5m -> driver.Sleep,
// > 2h -> driver.Stop. The sweeper lives in the control plane, not here.
func (u *CloudUpstream) LastSeen() time.Time {
	u.lastSeenMu.Lock()
	defer u.lastSeenMu.Unlock()
	return u.lastSeen
}

// ---------- Route store (subdomain -> cloud upstream) ----------

// RouteStore holds the in-process subdomain -> CloudUpstream map. It is fed by
// the route loader (Postgres load + LISTEN/NOTIFY). The tunnel path (in-memory
// Registry) is resolved first; this store is the fallback for kind='cloud'.
type RouteStore struct {
	mu      sync.RWMutex
	clouds  map[string]*CloudUpstream // key: subdomain
	metrics *WakeMetrics              // injected into every upstream the store adopts
}

// NewRouteStore returns an empty store.
func NewRouteStore() *RouteStore { return &RouteStore{clouds: map[string]*CloudUpstream{}} }

// SetMetrics attaches a collector to the store. Upstreams are created by the
// route loader, which has no reason to know about telemetry, so the store wires
// it in on the way past — both for routes already present and for later ones.
func (s *RouteStore) SetMetrics(m *WakeMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics = m
	for _, up := range s.clouds {
		up.Metrics = m
	}
}

// Metrics returns the store's collector (nil if telemetry is off).
func (s *RouteStore) Metrics() *WakeMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics
}

// SetCloud installs the upstream for a subdomain.
//
// If an upstream for the same app is already present it is updated IN PLACE
// rather than replaced. This matters more than it looks: the route loader
// re-reads the routes table every 60 seconds and builds a fresh CloudUpstream
// each time. Swapping the object in would silently discard all of its live
// state —
//
//	state    → the app resets to "awake" while its container is actually
//	           paused. The next request is then proxied into a frozen process
//	           and hangs until the response timeout instead of waking it, and
//	           the sweeper keeps re-issuing pause on an already-paused container.
//	pins     → an app with an open WebSocket/SSE connection looks unpinned, so
//	           the sweeper is free to freeze it mid-stream.
//	lastSeen → the idle clock resets, hiding genuinely idle apps from the sweeper.
//
// None of that state belongs to the route row; it belongs to the running app.
// So the row updates the object's configuration and leaves its liveness alone.
func (s *RouteStore) SetCloud(subdomain string, up *CloudUpstream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if up.Metrics == nil {
		up.Metrics = s.metrics
	}
	if prev, ok := s.clouds[subdomain]; ok && prev != up && prev.AppID == up.AppID {
		prev.updateConfig(up)
		return
	}
	s.clouds[subdomain] = up
}

// updateConfig copies configuration from a freshly-loaded route row onto a live
// upstream, preserving everything runtime (state, pins, lastSeen, proxy).
func (u *CloudUpstream) updateConfig(next *CloudUpstream) {
	u.targetMu.Lock()
	// Only re-point at the row's address if it actually changed. A wake may
	// have already corrected the target to the container's real IP, and the
	// DB row can lag behind that.
	if u.Target.String() != next.Target.String() {
		*u.Target = *next.Target
	}
	u.targetMu.Unlock()

	u.EdgeSecret = next.EdgeSecret
	if next.WakeTimeout > 0 {
		u.WakeTimeout = next.WakeTimeout
	}
	if next.Freeze != nil {
		u.Freeze = next.Freeze
	}
	if next.Waker != nil {
		u.Waker = next.Waker
	}
}

// Snapshot renders per-app relay-side state for the stats endpoint. This is the
// relay's half of the density picture (what it believes each app's state is);
// the runner's /v1/stats supplies the other half (what each app actually costs).
func (s *RouteStore) Snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byState := map[string]int{"hot": 0, "warm": 0, "stopped": 0}
	apps := make(map[string]any, len(s.clouds))
	for sub, up := range s.clouds {
		var state string
		switch up.SleepState() {
		case SleepWarm:
			state = "warm"
		case SleepStopped:
			state = "stopped"
		default:
			state = "hot"
		}
		byState[state]++
		var idleSec float64
		if last := up.LastSeen(); !last.IsZero() {
			idleSec = time.Since(last).Seconds()
		}
		apps[sub] = map[string]any{
			"app_id":   up.AppID,
			"state":    state,
			"pinned":   up.Pinned(),
			"idle_sec": idleSec,
		}
	}
	return map[string]any{"total": len(s.clouds), "by_state": byState, "apps": apps}
}

// Delete removes a subdomain's route.
func (s *RouteStore) Delete(subdomain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clouds, subdomain)
}

// LookupCloud returns the upstream for a subdomain, if any.
func (s *RouteStore) LookupCloud(subdomain string) (*CloudUpstream, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	up, ok := s.clouds[subdomain]
	return up, ok
}

// Each returns a snapshot of all upstreams (used by the idle sweeper).
func (s *RouteStore) Each(fn func(subdomain string, up *CloudUpstream)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for sub, up := range s.clouds {
		fn(sub, up)
	}
}
