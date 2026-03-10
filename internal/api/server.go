package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ahmetvural79/tunr/internal/billing"
	"github.com/ahmetvural79/tunr/internal/inspector"
	"github.com/ahmetvural79/tunr/internal/logger"
)

// Server is tunr's internal API server.
// Handles Paddle webhooks and serves the inspector API.
// Not exposed directly — sits behind nginx/Caddy in production.
type Server struct {
	port         int
	paddleClient *billing.PaddleClient
	ins          *inspector.Inspector
}

// New creates an API server
func New(port int, paddleClient *billing.PaddleClient, ins *inspector.Inspector) *Server {
	return &Server{
		port:         port,
		paddleClient: paddleClient,
		ins:          ins,
	}
}

// Handler returns an http.Handler with all API routes wired up
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// SECURITY: this endpoint must be internet-accessible for Paddle webhooks,
	// but signature verification is enforced on every request
	mux.HandleFunc("/webhook/paddle", s.handlePaddleWebhook)

	mux.HandleFunc("/api/v1/requests", s.handleListRequests)
	mux.HandleFunc("/api/v1/requests/", s.handleRequestDetail)
	mux.HandleFunc("/api/v1/requests/clear", s.handleClearRequests)
	mux.HandleFunc("/api/v1/stats", s.handleStats)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	return withMiddleware(mux)
}

// withMiddleware wraps the handler with standard HTTP middleware
func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SECURITY: lock down CORS to real domain in production (this is dev-mode permissive)
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SECURITY: every Paddle webhook is verified via HMAC-SHA256 signature
func (s *Server) handlePaddleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	if s.paddleClient != nil {
		s.paddleClient.HandleWebhook(w, r)
		return
	}

	logger.Warn("Paddle client not configured, webhook ignored")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ignored"}`))
}

func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requests := s.ins.GetAll()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"requests": requests,
		"count":    len(requests),
	})
}

func (s *Server) handleRequestDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	const prefix = "/api/v1/requests/"
	if len(path) <= len(prefix) {
		http.Error(w, "request ID required", http.StatusBadRequest)
		return
	}
	id := path[len(prefix):]

	if len(id) == 0 {
		http.Error(w, "invalid ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		req, err := s.ins.GetByID(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		if req.IsJSON && req.RespBody != "" {
			req.RespBody = inspector.PrettyJSON(req.RespBody)
		}
		if req.IsJSON && req.ReqBody != "" {
			req.ReqBody = inspector.PrettyJSON(req.ReqBody)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(req)

	case http.MethodPost:
		action := r.URL.Query().Get("action")
		if action == "replay" {
			var port int
			if _, err := io.ReadAll(r.Body); err == nil {
				port = 3000
			}

			result, err := s.ins.Replay(r.Context(), id, port)
			if err != nil {
				jsonError(w, err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)

		} else if action == "curl" {
			curlCmd, err := s.ins.ExportCurl(id)
			if err != nil {
				jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(curlCmd))

		} else {
			http.Error(w, "unknown action", http.StatusBadRequest)
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleClearRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.ins.Clear()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
	logger.Info("Inspector ring buffer cleared")
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.ins.Stats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
	})
}

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
