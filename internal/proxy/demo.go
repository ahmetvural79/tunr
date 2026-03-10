package proxy

import (
	"encoding/json"
	"net/http"
)

// DemoMiddleware blocks state-mutating requests (POST, PUT, PATCH, DELETE) and
// returns fake 2xx responses so the app looks functional without actually changing anything.
// Your client clicks "Place Order" and feels good — but no order is placed.
func DemoMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Safe methods pass through untouched
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Always let tunr's own endpoints through
		if r.URL.Path == "/__tunr/feedback" || r.URL.Path == "/__tunr/error" {
			next.ServeHTTP(w, r)
			return
		}

		// Mutation blocked — return a convincing fake success
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Tunr-Demo-Mode", "blocked-mutation")

		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}
		w.WriteHeader(status)

		// Most frontend libs (React Query, SWR) expect JSON back
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "demo_success",
			"message": "Mutations are disabled in Tunr Demo Mode. Request intercepted and faked.",
			"tunr":    true,
			"method":  r.Method,
			"path":    r.URL.Path,
		})
	})
}
