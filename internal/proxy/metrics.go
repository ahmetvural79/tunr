package proxy

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics tracks global tunnel metrics for the Prometheus endpoint.
type Metrics struct {
	RequestsTotal   atomic.Int64
	ActiveTunnels   atomic.Int64
	BytesSent       atomic.Int64
	BytesReceived   atomic.Int64
	ErrorsTotal     atomic.Int64
	TunnelStartTime time.Time
}

// GlobalMetrics is a singleton used by the proxy and inspector.
var GlobalMetrics = &Metrics{
	TunnelStartTime: time.Now(),
}

// PrometheusHandler returns an HTTP handler that exposes /metrics in Prometheus text format.
func PrometheusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := GlobalMetrics
		uptime := time.Since(m.TunnelStartTime).Seconds()

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# HELP tunr_requests_total Total number of requests proxied\n")
		fmt.Fprintf(w, "# TYPE tunr_requests_total counter\n")
		fmt.Fprintf(w, "tunr_requests_total %d\n\n", m.RequestsTotal.Load())

		fmt.Fprintf(w, "# HELP tunr_active_tunnels Number of currently active tunnels\n")
		fmt.Fprintf(w, "# TYPE tunr_active_tunnels gauge\n")
		fmt.Fprintf(w, "tunr_active_tunnels %d\n\n", m.ActiveTunnels.Load())

		fmt.Fprintf(w, "# HELP tunr_bytes_sent_total Total bytes sent to clients\n")
		fmt.Fprintf(w, "# TYPE tunr_bytes_sent_total counter\n")
		fmt.Fprintf(w, "tunr_bytes_sent_total %d\n\n", m.BytesSent.Load())

		fmt.Fprintf(w, "# HELP tunr_bytes_received_total Total bytes received from clients\n")
		fmt.Fprintf(w, "# TYPE tunr_bytes_received_total counter\n")
		fmt.Fprintf(w, "tunr_bytes_received_total %d\n\n", m.BytesReceived.Load())

		fmt.Fprintf(w, "# HELP tunr_errors_total Total number of proxy errors\n")
		fmt.Fprintf(w, "# TYPE tunr_errors_total counter\n")
		fmt.Fprintf(w, "tunr_errors_total %d\n\n", m.ErrorsTotal.Load())

		fmt.Fprintf(w, "# HELP tunr_uptime_seconds Time since tunr started\n")
		fmt.Fprintf(w, "# TYPE tunr_uptime_seconds gauge\n")
		fmt.Fprintf(w, "tunr_uptime_seconds %.2f\n\n", uptime)
	}
}

// HealthHandler returns a simple /healthz handler for K8s-style probes.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","uptime_seconds":%.0f}`, time.Since(GlobalMetrics.TunnelStartTime).Seconds())
	}
}

// ReadyHandler returns a /readyz handler that checks if at least one tunnel is active.
func ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if GlobalMetrics.ActiveTunnels.Load() > 0 {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"ready","active_tunnels":%d}`, GlobalMetrics.ActiveTunnels.Load())
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"not_ready","active_tunnels":0}`)
		}
	}
}
