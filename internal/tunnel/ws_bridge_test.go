package tunnel

import (
	"testing"
)

func TestPathForRouteMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"/api/foo?x=1", "/api/foo"},
		{"api/foo", "/api/foo"},
		{"/", "/"},
		{"/ws#frag", "/ws"},
	}
	for _, tc := range cases {
		if got := pathForRouteMatch(tc.in); got != tc.want {
			t.Fatalf("pathForRouteMatch(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractSecWebSocketProtocol(t *testing.T) {
	t.Parallel()
	open := &wsOpenPayload{
		HeadersV2: map[string][]string{
			"Sec-WebSocket-Protocol": {"vite-hmr, other"},
		},
	}
	got := extractSecWebSocketProtocol(open)
	if len(got) != 2 || got[0] != "vite-hmr" || got[1] != "other" {
		t.Fatalf("got %#v", got)
	}
	open2 := &wsOpenPayload{Headers: map[string]string{"Sec-WebSocket-Protocol": "abc"}}
	if g := extractSecWebSocketProtocol(open2); len(g) != 1 || g[0] != "abc" {
		t.Fatalf("got %#v", g)
	}
}

func TestBuildUpstreamWSHeadersStripsHandshakeFields(t *testing.T) {
	t.Parallel()
	open := &wsOpenPayload{
		HeadersV2: map[string][]string{
			"Upgrade":                {"websocket"},
			"Connection":             {"Upgrade"},
			"Sec-WebSocket-Key":      {"secret"},
			"Sec-WebSocket-Protocol": {"hmr"},
			"X-Dev":                  {"1"},
		},
	}
	h := buildUpstreamWSHeaders(open, 3000)
	if h.Get("Upgrade") != "" || h.Get("Sec-WebSocket-Key") != "" {
		t.Fatalf("handshake headers should be stripped: %v", h)
	}
	if h.Get("X-Dev") != "1" {
		t.Fatal("custom header missing")
	}
	if h.Get("Host") != "localhost:3000" {
		t.Fatalf("host: %q", h.Get("Host"))
	}
}
