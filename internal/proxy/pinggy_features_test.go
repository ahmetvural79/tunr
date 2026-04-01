package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBearerTokenMiddleware validates that the bearer token middleware
// correctly accepts valid tokens and rejects invalid/missing ones.
func TestBearerTokenMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		token        string
		header       string // Authorization header
		queryToken   string // ?token= value
		expectStatus int
	}{
		{
			name:         "valid bearer token",
			token:        "secret123",
			header:       "Bearer secret123",
			expectStatus: http.StatusOK,
		},
		{
			name:         "missing token",
			token:        "secret123",
			expectStatus: http.StatusUnauthorized,
		},
		{
			name:         "wrong token",
			token:        "secret123",
			header:       "Bearer wrong",
			expectStatus: http.StatusUnauthorized,
		},
		{
			name:         "query token valid",
			token:        "secret123",
			queryToken:   "secret123",
			expectStatus: http.StatusOK,
		},
		{
			name:         "query token wrong",
			token:        "secret123",
			queryToken:   "wrong",
			expectStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrapped := BearerTokenMiddleware(tc.token, "", handler)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if tc.queryToken != "" {
				req.URL.RawQuery = "token=" + tc.queryToken
			}

			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			if rr.Code != tc.expectStatus {
				t.Errorf("expected status %d, got %d", tc.expectStatus, rr.Code)
			}
		})
	}
}

// TestHeaderModificationMiddleware validates that header modification
// rules are applied correctly (add, replace, remove).
func TestHeaderModificationMiddleware(t *testing.T) {
	rules := []HeaderModification{
		{Action: "add", Header: "X-Custom", Value: "added"},
		{Action: "add", Header: "X-Custom", Value: "also-added"},
		{Action: "replace", Header: "X-Existing", Value: "replaced"},
		{Action: "remove", Header: "X-Remove-Me"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check the headers
		if got := r.Header.Get("X-Existing"); got != "replaced" {
			t.Errorf("X-Existing = %q, want %q", got, "replaced")
		}

		vals := r.Header.Values("X-Custom")
		if len(vals) != 2 {
			t.Errorf("X-Custom values count = %d, want 2", len(vals))
		}

		if r.Header.Get("X-Remove-Me") != "" {
			t.Errorf("X-Remove-Me should have been removed")
		}

		w.WriteHeader(http.StatusOK)
	})

	wrapped := HeaderModificationMiddleware(rules, handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Existing", "original")
	req.Header.Set("X-Remove-Me", "should-be-gone")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

// TestXForwardedForMiddleware verifies that X-Forwarded-For is injected
// only if not already present.
func TestXForwardedForMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Errorf("X-Forwarded-For should be set")
		}
		w.WriteHeader(http.StatusOK)
	})
	wrapped := XForwardedForMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
}

// TestOriginalURLMiddleware verifies that X-Original-URL is constructed
// from the request scheme, host, and URI.
func TestOriginalURLMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "https://example.com/path?query=1"
		if got := r.Header.Get("X-Original-URL"); got != want {
			t.Errorf("X-Original-URL = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusOK)
	})
	wrapped := OriginalURLMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "https://example.com/path?query=1", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
}

// TestIPWhitelist_Parsing ensures valid CIDRs are parsed and invalid ones
// are gracefully ignored.
func TestIPWhitelist_Parsing(t *testing.T) {
	// All valid
	wl := NewIPWhitelist([]string{"10.0.0.0/8", "192.168.1.0/24"})
	if wl.IsEmpty() {
		t.Errorf("expected non-empty whitelist")
	}

	// Mixed valid/invalid
	wl2 := NewIPWhitelist([]string{"10.0.0.0/8", "not-a-cidr"})
	if wl2.IsEmpty() {
		t.Errorf("expected non-empty whitelist after dropping invalid CIDR")
	}

	// All invalid
	wl3 := NewIPWhitelist([]string{"invalid", "also-invalid"})
	if !wl3.IsEmpty() {
		t.Errorf("expected empty whitelist when all CIDRs invalid")
	}
}

// TestIPWhitelist_Allowed verifies that the Allow method correctly
// accepts/denies IPs based on CIDR ranges.
func TestIPWhitelist_Allowed(t *testing.T) {
	wl := NewIPWhitelist([]string{
		"10.0.0.0/8",
		"203.0.113.5/32",
	})

	tests := []struct {
		ip    string
		allow bool
	}{
		{"10.1.2.3", true},
		{"10.255.255.255", true},
		{"203.0.113.5", true},
		{"203.0.113.6", false},
		{"192.168.1.1", false},
		{"invalid", false},
	}

	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			if got := wl.Allowed(tc.ip); got != tc.allow {
				t.Errorf("Allowed(%q) = %v, want %v", tc.ip, got, tc.allow)
			}
		})
	}
}

// TestIPWhitelist_Middleware verifies that the middleware rejects requests
// from non-whitelisted IPs with a 403 Forbidden.
func TestIPWhitelist_Middleware(t *testing.T) {
	wl := NewIPWhitelist([]string{"10.0.0.0/8"})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := wl.Middleware(handler)

	// Request in whitelist — should pass
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "10.1.2.3:12345"
	rr1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Errorf("whitelisted IP should get 200, got %d", rr1.Code)
	}

	// Request NOT in whitelist — should be forbidden
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "1.2.3.4:12345"
	rr2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusForbidden {
		t.Errorf("non-whitelisted IP should get 403, got %d", rr2.Code)
	}
}

// TestIPWhitelist_XForwardedFor ensures the middleware looks at
// X-Forwarded-For first for the real client IP.
func TestIPWhitelist_XForwardedFor(t *testing.T) {
	wl := NewIPWhitelist([]string{"10.0.0.0/8"})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := wl.Middleware(handler)

	// Real client is in XFF — should use it
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:12345" // spoofed remote addr
	req.Header.Set("X-Forwarded-For", "10.5.5.5") // real client
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("XFF whitelisted IP should get 200, got %d", rr.Code)
	}
}

// TestCORSPreflightMiddleware verifies that OPTIONS requests from allowed
// origins receive proper CORS headers.
func TestCORSPreflightMiddleware(t *testing.T) {
	origins := []string{"https://myapp.com"}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := CORSPreflightMiddleware(origins, handler)

	// Valid preflight
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://myapp.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 on valid preflight, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://myapp.com" {
		t.Errorf("unexpected ACAO: %s", got)
	}
}

// TestExtractClientIP verifies the helper extracts IPs from headers.
func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		name   string
		xff    string
		xri    string
		addr   string
		expect string
	}{
		{
			name:   "XFF takes priority",
			xff:    "10.0.0.1, 172.16.0.1",
			addr:   "1.2.3.4:1234",
			expect: "10.0.0.1",
		},
		{
			name:   "X-Real-IP fallback",
			xri:    "10.0.0.2",
			addr:   "1.2.3.4:1234",
			expect: "10.0.0.2",
		},
		{
			name:   "RemoteAddr fallback",
			addr:   "192.168.1.1:8080",
			expect: "192.168.1.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.addr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				req.Header.Set("X-Real-IP", tc.xri)
			}

			got := extractClientIP(req)
			if got != tc.expect {
				t.Errorf("extractClientIP = %q, want %q", got, tc.expect)
			}
		})
	}
}
