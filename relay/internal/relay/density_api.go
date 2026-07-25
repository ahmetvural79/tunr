package relay

// density_api.go — the capacity dashboard endpoint (Yoğunluk planı Faz 0).
//
// Faz 0's exit criterion is being able to answer, from one place: "how many apps
// are HOT right now, what do they actually cost, and how fast are they waking?"
// Until that question has an answer, every later decision — dropping the idle
// threshold, oversubscribing memory, gating checkpoint/restore — is a guess.
//
// It joins the two halves of the picture:
//
//	relay  → what state it believes each app is in, wake latency, activity mix
//	runner → what each app's cgroup actually costs, host PSI, build queue
//
// Auth: disabled unless TUNR_METRICS_TOKEN is set. This endpoint reveals the
// app inventory and the box's health, so it must never be open by default; an
// unset token means the route returns 404 as if it didn't exist.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// clusterSampler is the capacity source. Satisfied by a single RunnerClient
// today and by the Scheduler (which aggregates every node) once there is more
// than one — so this endpoint doesn't change shape when the cluster grows.
type clusterSampler interface {
	HostSample(ctx context.Context) (HostSample, error)
	Enabled() bool
}

// DensityAPI serves capacity telemetry.
type DensityAPI struct {
	store   *RouteStore
	cluster clusterSampler
	token   string // empty → endpoint disabled
}

// NewDensityAPI wires the endpoint. token empty disables it entirely.
func NewDensityAPI(store *RouteStore, cluster clusterSampler, token string) *DensityAPI {
	return &DensityAPI{store: store, cluster: cluster, token: token}
}

// Enabled reports whether a token was configured.
func (d *DensityAPI) Enabled() bool { return d.token != "" }

// RegisterRoutes mounts the endpoint when enabled.
func (d *DensityAPI) RegisterRoutes(mux *http.ServeMux) {
	if !d.Enabled() {
		return
	}
	mux.HandleFunc("/api/v1/density", d.handle)
}

func (d *DensityAPI) handle(w http.ResponseWriter, r *http.Request) {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(d.token)) != 1 {
		// 404, not 401: don't confirm the endpoint exists to an unauthenticated caller.
		http.NotFound(w, r)
		return
	}

	out := map[string]any{
		"routes": d.store.Snapshot(),
		"wake":   d.store.Metrics().Snapshot(),
	}

	// Node telemetry is best-effort: the relay must stay useful when the runner
	// is down, since the tunnel path doesn't depend on it at all.
	if d.cluster != nil && d.cluster.Enabled() {
		hctx, hcancel := context.WithTimeout(r.Context(), 5*time.Second)
		if h, err := d.cluster.HostSample(hctx); err == nil {
			out["host"] = h
			out["host_mem_utilization"] = h.MemUtilization()
		} else {
			out["host_error"] = err.Error()
		}
		hcancel()
	} else {
		out["host_error"] = "no runner configured"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
