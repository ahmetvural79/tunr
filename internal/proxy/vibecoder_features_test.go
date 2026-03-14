package proxy

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func mustPortFromURL(t *testing.T, rawURL string) int {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return port
}

func TestDemoMiddleware_BlocksMutations(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := DemoMiddleware(next)
	req := httptest.NewRequest(http.MethodPost, "/orders", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for POST in demo mode, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-Tunr-Demo-Mode"); got != "blocked-mutation" {
		t.Fatalf("expected demo mode header, got %q", got)
	}
	if !strings.Contains(rec.Body.String(), "demo_success") {
		t.Fatalf("expected demo success payload, got %q", rec.Body.String())
	}
}

func TestDemoMiddleware_AllowsReadMethods(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	})

	handler := DemoMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("expected GET request to pass through demo middleware")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected status from next handler, got %d", rec.Code)
	}
}

func TestFreezeCache_CachesAndServesSnapshot(t *testing.T) {
	cache := NewFreezeCache(true)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("snapshot-body"))
	})

	req := httptest.NewRequest(http.MethodGet, "/health?full=1", nil)
	rec := httptest.NewRecorder()
	cache.Middleware(next).ServeHTTP(rec, req)

	fallbackReq := httptest.NewRequest(http.MethodGet, "/health?full=1", nil)
	fallbackRec := httptest.NewRecorder()
	served := cache.ServeFromCache(fallbackRec, fallbackReq)
	if !served {
		t.Fatal("expected cache hit from freeze mode")
	}
	if fallbackRec.Code != http.StatusOK {
		t.Fatalf("expected cached status 200, got %d", fallbackRec.Code)
	}
	if fallbackRec.Body.String() != "snapshot-body" {
		t.Fatalf("expected cached body, got %q", fallbackRec.Body.String())
	}
	if fallbackRec.Header().Get("X-Tunr-Freeze-Cache") != "HIT" {
		t.Fatalf("expected freeze cache hit header, got %q", fallbackRec.Header().Get("X-Tunr-Freeze-Cache"))
	}
}

func TestInjectMiddleware_InjectsWidgetIntoHTML(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body><h1>Hello</h1></body></html>"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	InjectMiddleware(next).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "tunr Vibecoder Feedback Widget") {
		t.Fatalf("expected widget injection marker in html body, got %q", body)
	}
	if !strings.Contains(body, "window.addEventListener('error'") {
		t.Fatalf("expected remote error catcher script, got %q", body)
	}
}

func TestLocalProxy_AutoLoginCookieInjection(t *testing.T) {
	var cookieSeen string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieSeen = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	proxy, err := NewLocalProxy(mustPortFromURL(t, upstream.URL), nil)
	if err != nil {
		t.Fatalf("new local proxy: %v", err)
	}
	proxy.AutoLogin = "session=demo-token"
	proxy.BuildMiddlewareChain()

	req := httptest.NewRequest(http.MethodGet, "/profile", nil)
	req.Header.Set("Cookie", "foo=bar")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from upstream, got %d", rec.Code)
	}
	if !strings.Contains(cookieSeen, "foo=bar; session=demo-token") {
		t.Fatalf("expected merged cookie with auto-login token, got %q", cookieSeen)
	}
}

func TestLocalProxy_PathRouting(t *testing.T) {
	uiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ui"))
	}))
	defer uiSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("api"))
	}))
	defer apiSrv.Close()

	routes := map[string]int{"/api": mustPortFromURL(t, apiSrv.URL)}
	proxy, err := NewLocalProxy(mustPortFromURL(t, uiSrv.URL), routes)
	if err != nil {
		t.Fatalf("new local proxy: %v", err)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	apiRec := httptest.NewRecorder()
	proxy.ServeHTTP(apiRec, apiReq)
	if got := strings.TrimSpace(apiRec.Body.String()); got != "api" {
		t.Fatalf("expected /api route to hit api server, got %q", got)
	}

	uiReq := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	uiRec := httptest.NewRecorder()
	proxy.ServeHTTP(uiRec, uiReq)
	if got := strings.TrimSpace(uiRec.Body.String()); got != "ui" {
		t.Fatalf("expected non-routed path to hit default server, got %q", got)
	}
}

func TestBasicAuthMiddleware_DefaultAdminUser(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := BasicAuthMiddleware("secret", next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	cred := base64.StdEncoding.EncodeToString([]byte("admin:secret"))
	req.Header.Set("Authorization", "Basic "+cred)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected authorized request to pass, got %d", rec.Code)
	}
}
