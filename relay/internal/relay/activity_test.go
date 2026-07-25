package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(method, path string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestClassifyActivity(t *testing.T) {
	cases := []struct {
		name string
		req  *http.Request
		want ActivityClass
	}{
		// Real traffic must never be downgraded — a misclassified user request
		// gets the app frozen underneath someone who is actively using it.
		{"plain GET", req("GET", "/", nil), ActivityNormal},
		{"GET a page", req("GET", "/dashboard", nil), ActivityNormal},
		{"POST", req("POST", "/api/items", nil), ActivityNormal},
		{"browser UA", req("GET", "/", map[string]string{
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
		}), ActivityNormal},

		// Probes: wake nothing, reset nothing.
		{"HEAD root", req("HEAD", "/", nil), ActivityProbe},
		{"OPTIONS root", req("OPTIONS", "/", nil), ActivityProbe},
		{"GET /health", req("GET", "/health", nil), ActivityProbe},
		{"GET /healthz", req("GET", "/healthz", nil), ActivityProbe},
		{"GET /health/ trailing slash", req("GET", "/health/", nil), ActivityProbe},
		{"GET /HEALTH uppercase", req("GET", "/HEALTH", nil), ActivityProbe},
		{"uptimerobot UA", req("GET", "/", map[string]string{
			"User-Agent": "Mozilla/5.0+(compatible; UptimeRobot/2.0; http://uptimerobot.com/)",
		}), ActivityProbe},
		{"googlebot UA", req("GET", "/some/page", map[string]string{
			"User-Agent": "Googlebot/2.1 (+http://www.google.com/bot.html)",
		}), ActivityProbe},
		{"low cloudflare bot score", req("GET", "/", map[string]string{"Cf-Bot-Score": "3"}), ActivityProbe},

		// Pins: a live connection forbids sleep.
		{"websocket upgrade", req("GET", "/socket", map[string]string{
			"Upgrade": "websocket", "Connection": "Upgrade",
		}), ActivityPin},
		{"websocket with multi-token Connection", req("GET", "/socket", map[string]string{
			"Upgrade": "WebSocket", "Connection": "keep-alive, Upgrade",
		}), ActivityPin},
		{"SSE", req("GET", "/events", map[string]string{"Accept": "text/event-stream"}), ActivityPin},
		{"SSE with params", req("GET", "/events", map[string]string{
			"Accept": "text/event-stream; charset=utf-8",
		}), ActivityPin},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyActivity(c.req); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// A POST to /health is somebody's API, not a monitor — treating it as a probe
// would mean a sleeping app silently swallows a real write.
func TestClassifyActivityPostToHealthIsNormal(t *testing.T) {
	if got := ClassifyActivity(req("POST", "/health", nil)); got != ActivityNormal {
		t.Fatalf("POST /health classified as %v, want normal", got)
	}
}

// A path that merely starts with a health-ish prefix is a real page.
func TestClassifyActivityPrefixIsNotProbe(t *testing.T) {
	for _, p := range []string{"/healthcheck-dashboard", "/health-report", "/pingpong"} {
		if got := ClassifyActivity(req("GET", p, nil)); got != ActivityNormal {
			t.Fatalf("%s classified as %v, want normal", p, got)
		}
	}
}

// A high Cloudflare bot score means "probably human" and must not be a probe.
func TestClassifyActivityHighBotScoreIsNormal(t *testing.T) {
	r := req("GET", "/", map[string]string{"Cf-Bot-Score": "95"})
	if got := ClassifyActivity(r); got != ActivityNormal {
		t.Fatalf("high bot score classified as %v, want normal", got)
	}
}

// A bare "Upgrade: websocket" without the matching Connection token is not a
// valid handshake and must not pin the app awake indefinitely.
func TestClassifyActivityIncompleteUpgradeIsNotPin(t *testing.T) {
	r := req("GET", "/socket", map[string]string{"Upgrade": "websocket"})
	if got := ClassifyActivity(r); got == ActivityPin {
		t.Fatal("incomplete upgrade handshake should not pin")
	}
}

func TestWriteSyntheticHealth(t *testing.T) {
	w := httptest.NewRecorder()
	writeSyntheticHealth(w, "a_test")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// The substitution must be auditable: a monitor seeing 200 should be able to
	// tell the app itself never answered.
	if w.Header().Get("X-Tunr-Sleeping") != "1" {
		t.Error("missing X-Tunr-Sleeping header")
	}
	if w.Header().Get("X-Tunr-Answered-By") != "edge" {
		t.Error("missing X-Tunr-Answered-By header")
	}
}
