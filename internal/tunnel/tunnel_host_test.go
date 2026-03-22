package tunnel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/ahmetvural79/tunr/internal/proxy"
)

func mustPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return port
}

func TestForwardViaProxyStripsAcceptEncoding(t *testing.T) {
	t.Parallel()

	var gotAcceptEnc string
	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAcceptEnc = r.Header.Get("Accept-Encoding")
		gotHost = r.Host
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer upstream.Close()

	targetPort := mustPort(t, upstream.URL)
	lp, err := proxy.NewLocalProxy(targetPort, nil)
	if err != nil {
		t.Fatalf("new local proxy: %v", err)
	}

	resp := forwardViaProxy(lp, targetPort, &requestData{
		RequestID: "req-1",
		Method:    http.MethodGet,
		Path:      "/",
		HeadersV2: map[string][]string{
			"Accept-Encoding":  {"gzip, deflate, br"},
			"X-Forwarded-Host": {"abc123.tunr.sh"},
		},
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	// Accept-Encoding must be stripped from the request reaching the upstream;
	// Go's Transport re-adds Accept-Encoding: gzip and handles decompression.
	if gotAcceptEnc == "gzip, deflate, br" {
		t.Fatal("Accept-Encoding was NOT stripped — browser encoding should not reach upstream")
	}

	// Host should remain as localhost (not the tunnel domain) so dev servers accept it.
	if gotHost == "abc123.tunr.sh" {
		t.Fatalf("Host should be localhost, got tunnel domain: %q", gotHost)
	}
}
