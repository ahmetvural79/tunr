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
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// Waker abstracts "make this app dialable". runner.DockerDriver implements it on
// the own-server path; on Fly it's a no-op (Fly Proxy wakes machines itself).
// Defined as a local interface so package relay doesn't hard-depend on runner.
type Waker interface {
	Wake(ctx context.Context, appID string) error
}

// FreezeCache is implemented by a freeze-mode store: last good response per
// (host, path) — reused here to mask redeploy gaps and wake failures.
// Wired in Faz 1 (relay-side freeze); nil-safe until then.
type FreezeCache interface {
	Serve(w http.ResponseWriter, r *http.Request) bool // true if served from cache
	Store(r *http.Request, status int, header http.Header, body []byte)
}

// CloudUpstream reverse-proxies a subdomain to a persistent app, waking it on demand.
type CloudUpstream struct {
	AppID       string
	Target      *url.URL      // http://tunr-app-x:8080 (own-server) or http://tunr-a-x.flycast:80 (fly)
	EdgeSecret  []byte        // HMAC key; tunr-shim in the container verifies X-Tunr-Edge
	WakeTimeout time.Duration // total budget for probe+wake, e.g. 30s
	Waker       Waker
	Freeze      FreezeCache // optional (Faz 1)

	initOnce sync.Once
	proxy    *httputil.ReverseProxy
	// lastSeen feeds the idle sweeper (control plane reads it to Sleep/Stop apps).
	lastSeenMu sync.Mutex
	lastSeen   time.Time
}

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
			pr.SetURL(u.Target)
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
	u.touch()

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
func (u *CloudUpstream) ensureDialable(ctx context.Context) error {
	addr := u.Target.Host // host:port
	probe := func() error {
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = c.Close()
		}
		return err
	}

	if probe() == nil {
		return nil
	}
	if u.Waker != nil {
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		if err := u.Waker.Wake(wctx, u.AppID); err != nil {
			// Log but keep probing — the unit may still be coming up.
			logger.Warn("cloud wake %s: %v", u.AppID, err)
		}
		wcancel()
	}
	backoff := 300 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wake budget exceeded for %s", u.AppID)
		case <-time.After(backoff):
		}
		if probe() == nil {
			return nil
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
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
	mu     sync.RWMutex
	clouds map[string]*CloudUpstream // key: subdomain
}

// NewRouteStore returns an empty store.
func NewRouteStore() *RouteStore { return &RouteStore{clouds: map[string]*CloudUpstream{}} }

// SetCloud installs (or replaces) the upstream for a subdomain.
func (s *RouteStore) SetCloud(subdomain string, up *CloudUpstream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clouds[subdomain] = up
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
