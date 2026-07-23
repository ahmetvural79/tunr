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
type wakerFunc func(ctx context.Context, appID string) error

func (f wakerFunc) Wake(ctx context.Context, appID string) error { return f(ctx, appID) }

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
		wakerFunc(func(ctx context.Context, appID string) error {
			atomic.AddInt32(&wakeCalls, 1)
			return nil
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
	waker := wakerFunc(func(ctx context.Context, appID string) error {
		atomic.AddInt32(&wakeCalls, 1)
		// Simulate the driver bringing the app up: start serving on addr.
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		srv := &http.Server{Handler: handler}
		go func() { _ = srv.Serve(l) }()
		return nil
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
