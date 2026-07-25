package relay

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// wakerFunc adapts a func to the Waker interface.
type wakerFunc func(ctx context.Context, appID string) (string, error)

func (f wakerFunc) Wake(ctx context.Context, appID string) (string, error) { return f(ctx, appID) }

// verifyEdge recomputes the X-Tunr-Edge HMAC the way tunr-shim will, and reports
// whether the request carried a valid signature. host/path are what the upstream sees.
func verifyEdge(secret []byte, header, host, path string) bool {
	// format: "t=<unix>, s=<hex hmac>"
	parts := strings.SplitN(header, ", s=", 2)
	if len(parts) != 2 {
		return false
	}
	ts := strings.TrimPrefix(parts[0], "t=")
	return signEdge(secret, ts, host, path) == parts[1]
}

func TestCloudUpstream_HappyPath(t *testing.T) {
	secret := []byte("edge-secret-123")
	var sawEdge int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if verifyEdge(secret, r.Header.Get("X-Tunr-Edge"), r.Host, r.URL.Path) {
			atomic.StoreInt32(&sawEdge, 1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "hello from app")
	}))
	defer upstream.Close()

	target, _ := url.Parse(upstream.URL)

	var wakeCalls int32
	up := NewCloudUpstream("a_test", target, secret,
		wakerFunc(func(ctx context.Context, appID string) (string, error) {
			atomic.AddInt32(&wakeCalls, 1)
			return "", nil
		}), nil)
	up.WakeTimeout = 5 * time.Second

	req := httptest.NewRequest(http.MethodGet, "http://sprint.tunr.sh/api/x", nil)
	rec := httptest.NewRecorder()
	up.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "hello from app" {
		t.Fatalf("body = %q, want %q", body, "hello from app")
	}
	if n := atomic.LoadInt32(&wakeCalls); n != 0 {
		t.Fatalf("wake called %d times on an already-up target, want 0", n)
	}
	if atomic.LoadInt32(&sawEdge) != 1 {
		t.Fatal("upstream did not receive a valid X-Tunr-Edge signature")
	}
}

func TestCloudUpstream_WakeOnRequest(t *testing.T) {
	secret := []byte("edge-secret-xyz")

	// Reserve a loopback address, then close it so nothing is listening yet.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "woke up")
	})

	var wakeCalls int32
	waker := wakerFunc(func(ctx context.Context, appID string) (string, error) {
		atomic.AddInt32(&wakeCalls, 1)
		// Simulate the driver bringing the app up: start serving on addr.
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return "", err
		}
		srv := &http.Server{Handler: handler}
		go func() { _ = srv.Serve(l) }()
		return "", nil
	})

	target, _ := url.Parse("http://" + addr)
	up := NewCloudUpstream("a_sleep", target, secret, waker, nil)
	up.WakeTimeout = 5 * time.Second

	req := httptest.NewRequest(http.MethodGet, "http://sleepy.tunr.sh/", nil)
	rec := httptest.NewRecorder()
	up.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "woke up" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "woke up")
	}
	if n := atomic.LoadInt32(&wakeCalls); n != 1 {
		t.Fatalf("wake called %d times, want exactly 1", n)
	}
}

func TestRouteStore_SetLookupDelete(t *testing.T) {
	s := NewRouteStore()
	if _, ok := s.LookupCloud("nope"); ok {
		t.Fatal("empty store returned a route")
	}
	target, _ := url.Parse("http://tunr-app-a1:8080")
	up := NewCloudUpstream("a1", target, []byte("k"), nil, nil)
	s.SetCloud("app1", up)

	got, ok := s.LookupCloud("app1")
	if !ok || got != up {
		t.Fatal("SetCloud/LookupCloud mismatch")
	}

	count := 0
	s.Each(func(sub string, _ *CloudUpstream) { count++ })
	if count != 1 {
		t.Fatalf("Each visited %d, want 1", count)
	}

	s.Delete("app1")
	if _, ok := s.LookupCloud("app1"); ok {
		t.Fatal("route still present after Delete")
	}
}

// A route reload must not discard an upstream's live state. The route loader
// rebuilds a CloudUpstream from the DB every 60s; if SetCloud swapped the object
// in, the app would reset to "awake" while its container is actually paused —
// and the next request would be proxied into a frozen process and hang.
func TestSetCloudPreservesLiveStateAcrossReload(t *testing.T) {
	store := NewRouteStore()
	target, _ := url.Parse("http://172.20.0.5:8080")

	first := NewCloudUpstream("a_1", target, []byte("secret"), nil, nil)
	store.SetCloud("app", first)

	// The app goes to sleep and picks up an open streaming connection.
	first.SetSleepState(SleepWarm)
	first.pins.Add(1)
	first.touch()

	// A route reload builds a brand-new object from the same DB row.
	reloaded := NewCloudUpstream("a_1", mustURL(t, "http://172.20.0.5:8080"), []byte("secret"), nil, nil)
	store.SetCloud("app", reloaded)

	got, ok := store.LookupCloud("app")
	if !ok {
		t.Fatal("route disappeared after reload")
	}
	if got != first {
		t.Fatal("upstream object was replaced instead of updated in place")
	}
	if got.SleepState() != SleepWarm {
		t.Errorf("state = %v after reload, want SleepWarm", got.SleepState())
	}
	if !got.Pinned() {
		t.Error("pin lost across reload — the sweeper could freeze a streaming app")
	}
	if got.LastSeen().IsZero() {
		t.Error("lastSeen reset across reload — idle clock restarted")
	}
}

// A changed address in the routes table must still be applied.
func TestSetCloudAppliesNewTarget(t *testing.T) {
	store := NewRouteStore()
	first := NewCloudUpstream("a_1", mustURL(t, "http://172.20.0.5:8080"), []byte("s"), nil, nil)
	store.SetCloud("app", first)
	first.SetSleepState(SleepWarm)

	store.SetCloud("app", NewCloudUpstream("a_1", mustURL(t, "http://172.20.0.9:8080"), []byte("s2"), nil, nil))

	got, _ := store.LookupCloud("app")
	got.targetMu.RLock()
	host := got.Target.Host
	got.targetMu.RUnlock()
	if host != "172.20.0.9:8080" {
		t.Fatalf("target = %s, want the reloaded address", host)
	}
	if got.SleepState() != SleepWarm {
		t.Error("state should survive an address change — the app is still asleep")
	}
}

// A different app on the same subdomain is a genuine replacement.
func TestSetCloudReplacesOnDifferentApp(t *testing.T) {
	store := NewRouteStore()
	first := NewCloudUpstream("a_old", mustURL(t, "http://172.20.0.5:8080"), []byte("s"), nil, nil)
	store.SetCloud("app", first)
	first.SetSleepState(SleepWarm)

	next := NewCloudUpstream("a_new", mustURL(t, "http://172.20.0.6:8080"), []byte("s"), nil, nil)
	store.SetCloud("app", next)

	got, _ := store.LookupCloud("app")
	if got != next {
		t.Fatal("a different app should replace the upstream outright")
	}
	if got.SleepState() != SleepAwake {
		t.Errorf("new app state = %v, want SleepAwake", got.SleepState())
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
