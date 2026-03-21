package relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestIsBrowserWebSocket(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		h    http.Header
		want bool
	}{
		{
			name: "upgrade websocket",
			h:    http.Header{"Upgrade": []string{"websocket"}, "Connection": []string{"Upgrade"}},
			want: true,
		},
		{name: "no upgrade", h: http.Header{"Connection": []string{"Upgrade"}}, want: false},
		{name: "upgrade not websocket", h: http.Header{"Upgrade": []string{"h2c"}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &http.Request{Header: tc.h}
			if got := isBrowserWebSocket(r); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestBrowserTunnelCheckOrigin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		origin string
		want   bool
	}{
		{"", true},
		{"http://localhost:3000", true},
		{"http://127.0.0.1:3000", true},
		{"https://abc.tunr.sh", true},
		{"https://tunr.sh", true},
		{"https://evil.example", false},
	}
	for _, tc := range cases {
		name := tc.origin
		if name == "" {
			name = "no_origin"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := &http.Request{Header: http.Header{}}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := browserTunnelCheckOrigin(r); got != tc.want {
				t.Fatalf("origin %q: got %v want %v", tc.origin, got, tc.want)
			}
		})
	}
}

// TestBrowserWebSocketRelayEchoRoundTrip exercises the public WS upgrade path and
// Outbound ws_open / ws_frame flow (same shape as Next/Vite HMR over the tunnel).
func TestBrowserWebSocketRelayEchoRoundTrip(t *testing.T) {
	reg := NewRegistry()
	entry, err := reg.Register("e2e-user", "hmre2e01")
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Unregister(entry.ID)

	domain := "tunr.test"
	p := NewProxy(reg, domain)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r2 := r.Clone(r.Context())
		r2.Host = fmt.Sprintf("%s.%s", entry.Subdomain, domain)
		p.ServeHTTP(w, r2)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		relayWSFrameEcho(ctx, entry)
	}()

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/_next/webpack-hmr"
	d := websocket.Dialer{}
	client, _, err := d.Dial(wsURL, http.Header{
		"Origin": []string{"http://127.0.0.1:3000"},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if err := client.WriteMessage(websocket.TextMessage, []byte("hmr-ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, payload, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(payload) != "hmr-ping" {
		t.Fatalf("echo mismatch: %q", payload)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("echo goroutine did not exit")
	}
}

func relayWSFrameEcho(ctx context.Context, entry *TunnelEntry) {
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-entry.Outbound:
			if !ok {
				return
			}
			switch m.Type {
			case MsgTypeWsOpen:
				continue
			case MsgTypeWsFrame:
				var f WsFrameData
				if err := json.Unmarshal(m.Data, &f); err != nil {
					return
				}
				raw, err := base64.StdEncoding.DecodeString(f.PayloadB64)
				if err != nil {
					return
				}
				_ = entry.WriteWSFrameToBrowser(f.StreamID, f.Opcode, raw)
			case MsgTypeWsClose:
				return
			default:
				continue
			}
		}
	}
}
