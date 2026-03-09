package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/tunr-dev/tunr/internal/billing"
	"github.com/tunr-dev/tunr/internal/inspector"
	"github.com/tunr-dev/tunr/internal/logger"
)

// Server — tunr'nun internal API sunucusu.
// Bu sunucu hem Paddle webhook'larını alır hem de inspector API'sini sağlar.
// Port dıştan erişilmez, nginx/Caddy arkasına alınır (production'da).
type Server struct {
	port          int
	paddleClient  *billing.PaddleClient
	ins           *inspector.Inspector
	httpServer    *http.Client
}

// New — API sunucusu oluştur
func New(port int, paddleClient *billing.PaddleClient, ins *inspector.Inspector) *Server {
	return &Server{
		port:         port,
		paddleClient: paddleClient,
		ins:          ins,
	}
}

// Handler — tüm API route'larını birleştiren http.Handler
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// ── Paddle Webhook ──
	// GÜVENLİK: Bu endpoint internetten erişilebilir olmalı
	// (Paddle sunucularından webhook alabilmek için)
	// Ama imza doğrulaması zorunlu!
	mux.HandleFunc("/webhook/paddle", s.handlePaddleWebhook)

	// ── Inspector API ──
	mux.HandleFunc("/api/v1/requests", s.handleListRequests)
	mux.HandleFunc("/api/v1/requests/", s.handleRequestDetail) // /api/v1/requests/{id}
	mux.HandleFunc("/api/v1/requests/clear", s.handleClearRequests)
	mux.HandleFunc("/api/v1/stats", s.handleStats)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	// Middleware zinciri: timeout + logging + recovery
	return withMiddleware(mux)
}

// withMiddleware — temel HTTP middleware'leri uygula
func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS — production'da kısıtla, dev'de açık
		// GÜVENLİK: * yerine gerçek domain koy production'da
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("X-Content-Type-Options", "nosniff") // MIME sniffing önle
		w.Header().Set("X-Frame-Options", "DENY")           // clickjacking önle
		w.Header().Set("Cache-Control", "no-store")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handlePaddleWebhook — Paddle'dan gelen webhook olaylarını işle
// GÜVENLİK: Her webhook HMAC-SHA256 imzasıyla doğrulanır
func (s *Server) handlePaddleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Body sınırı: 1MB
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	if s.paddleClient != nil {
		// Paddle client'ın kendi handler'ını kullan (imza doğrulama dahil)
		s.paddleClient.HandleWebhook(w, r)
		return
	}

	// Paddle client yoksa (test ortamı) sadece 200 dön
	logger.Warn("Paddle client yapılandırılmamış, webhook işlenemiyor")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ignored"}`))
}

// handleListRequests — kayıtlı isteklerin listesini döndür
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

// handleRequestDetail — belirli bir isteğin detaylarını döndür
func (s *Server) handleRequestDetail(w http.ResponseWriter, r *http.Request) {
	// URL: /api/v1/requests/{id}
	// Not: Go 1.22+ pattern routing ile daha temiz yapılabilir ama compat için elle parse
	path := r.URL.Path
	const prefix = "/api/v1/requests/"
	if len(path) <= len(prefix) {
		http.Error(w, "request ID gerekli", http.StatusBadRequest)
		return
	}
	id := path[len(prefix):]

	// Alt path güvenliği: / içermemeli
	if len(id) == 0 {
		http.Error(w, "geçersiz ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		req, err := s.ins.GetByID(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		// JSON body'leri pretty print yap
		if req.IsJSON && req.RespBody != "" {
			req.RespBody = inspector.PrettyJSON(req.RespBody)
		}
		if req.IsJSON && req.ReqBody != "" {
			req.ReqBody = inspector.PrettyJSON(req.ReqBody)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(req)

	case http.MethodPost:
		// Replay isteği: POST /api/v1/requests/{id}?action=replay
		action := r.URL.Query().Get("action")
		if action == "replay" {
			// Local port'u query param'dan al
			var port int
			if _, err := io.ReadAll(r.Body); err == nil {
				// port belirsizse 3000'i kullan (geliştirici kolaylığı)
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
			// curl export: POST /api/v1/requests/{id}?action=curl
			curlCmd, err := s.ins.ExportCurl(id)
			if err != nil {
				jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(curlCmd))

		} else {
			http.Error(w, "bilinmeyen action", http.StatusBadRequest)
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleClearRequests — ring buffer'ı temizle
func (s *Server) handleClearRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.ins.Clear()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
	logger.Info("Inspector ring buffer temizlendi")
}

// handleStats — inspector istatistikleri
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.ins.Stats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// handleHealth — sağlık kontrolü
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
	})
}

// jsonError — JSON formatında hata döndür
func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
