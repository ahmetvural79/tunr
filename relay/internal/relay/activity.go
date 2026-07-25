package relay

// activity.go — request activity classification (Yoğunluk planı Faz 1, lever L7).
//
// The sweeper decides when to sleep an app from "time since last request". That
// is too blunt, and it fails in both directions:
//
//   - Health checks, uptime robots and crawlers hit an app every 30–60 seconds
//     forever. Under a last-request rule they pin every app awake permanently,
//     which quietly cancels scale-to-zero for the entire population.
//   - WebSocket and SSE connections look like a single old request. Under the
//     same rule the app gets frozen mid-stream while a client is still attached.
//
// So a request carries an activity *class*, not just a timestamp:
//
//	Probe   — wakes nothing, doesn't count as activity. A sleeping app answers
//	          these itself, at the edge, with a synthetic 200 (Koyeb's dummy
//	          server trick, but free because the relay is already in the path).
//	Normal  — real traffic. Wakes the app and keeps it warm.
//	Pin     — long-lived connection. Sleep is forbidden while one is open.
//
// Deliberately conservative: anything unrecognised is Normal. Misclassifying
// real traffic as a probe would let us freeze an app someone is using, which is
// far worse than keeping a bot-polled app awake a while longer.

import (
	"net/http"
	"strings"
)

// ActivityClass is how a request affects an app's sleep state.
type ActivityClass int

const (
	// ActivityNormal is real traffic: wake the app, reset the idle clock.
	ActivityNormal ActivityClass = iota
	// ActivityProbe is monitoring traffic: never wakes, never resets the clock.
	ActivityProbe
	// ActivityPin is a long-lived connection: forbids sleep while it is open.
	ActivityPin
)

func (a ActivityClass) String() string {
	switch a {
	case ActivityProbe:
		return "probe"
	case ActivityPin:
		return "pin"
	default:
		return "normal"
	}
}

// probePaths are the conventional health endpoints. Matched exactly (after
// trimming a trailing slash) so an app's real "/healthcheck-dashboard" page
// isn't silently swallowed.
var probePaths = map[string]bool{
	"/health":             true,
	"/healthz":            true,
	"/_health":            true,
	"/healthcheck":        true,
	"/livez":              true,
	"/readyz":             true,
	"/ping":               true,
	"/up":                 true,
	"/.well-known/health": true,
}

// probeAgents are substrings (lowercased) of User-Agents belonging to monitors
// and crawlers — traffic that proves an app is reachable but means nobody is
// actually using it.
var probeAgents = []string{
	"uptimerobot",
	"pingdom",
	"statuscake",
	"betteruptime",
	"better-uptime",
	"uptime-kuma",
	"hetrixtool",
	"site24x7",
	"datadog",
	"newrelic",
	"prometheus",
	"blackbox_exporter",
	"kube-probe",
	"googlebot",
	"bingbot",
	"ahrefsbot",
	"semrushbot",
	"yandexbot",
	"censys",
	"shodan",
	"zgrab",
	"masscan",
}

// ClassifyActivity assigns a request its activity class.
func ClassifyActivity(r *http.Request) ActivityClass {
	// Long-lived connections first — a WebSocket handshake is a GET, and an SSE
	// request could otherwise be mistaken for an ordinary one.
	if isWebSocketUpgrade(r) || isEventStream(r) {
		return ActivityPin
	}

	// Bare liveness pokes at the root: HEAD / and OPTIONS / carry no user intent.
	if (r.Method == http.MethodHead || r.Method == http.MethodOptions) && normalizePath(r.URL.Path) == "/" {
		return ActivityProbe
	}

	// Conventional health endpoints, on read-only methods only — a POST to
	// /health is somebody's API, not a monitor.
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if probePaths[normalizePath(r.URL.Path)] {
			return ActivityProbe
		}
	}

	if isProbeAgent(r.Header.Get("User-Agent")) {
		return ActivityProbe
	}

	// Cloudflare's bot score: 1–30 is "almost certainly automated". Present only
	// when the zone has Bot Management, so its absence means nothing.
	if score := r.Header.Get("Cf-Bot-Score"); score != "" {
		if n := atoiSafe(score); n > 0 && n <= 30 {
			return ActivityProbe
		}
	}

	return ActivityNormal
}

// normalizePath lowercases and strips a trailing slash so "/Health/" matches.
func normalizePath(p string) string {
	p = strings.ToLower(p)
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	if p == "" {
		return "/"
	}
	return p
}

func isProbeAgent(ua string) bool {
	if ua == "" {
		return false
	}
	ua = strings.ToLower(ua)
	for _, a := range probeAgents {
		if strings.Contains(ua, a) {
			return true
		}
	}
	return false
}

// isWebSocketUpgrade reports a WebSocket handshake. Connection is a
// comma-separated list and both header values are case-insensitive.
func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, tok := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
			return true
		}
	}
	return false
}

// isEventStream reports an SSE request (Accept: text/event-stream).
func isEventStream(r *http.Request) bool {
	for _, v := range strings.Split(r.Header.Get("Accept"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(v, ";", 2)[0]), "text/event-stream") {
			return true
		}
	}
	return false
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range strings.TrimSpace(s) {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
		if n > 1_000_000 {
			return -1
		}
	}
	return n
}

// writeSyntheticHealth answers a probe on behalf of a sleeping app.
//
// This closes the loop that otherwise makes scale-to-zero unusable: an external
// monitor polls a sleeping app, the poll wakes it, the app is never idle long
// enough to sleep again. Answering at the edge keeps the monitor green and the
// app asleep. The header makes the substitution auditable rather than a lie.
func writeSyntheticHealth(w http.ResponseWriter, appID string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Tunr-Sleeping", "1")       // app is scaled to zero
	h.Set("X-Tunr-Answered-By", "edge") // this response did not come from the app
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
	_ = appID
}
