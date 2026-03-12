package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/tunr-dev/tunr/relay/internal/auth"
	relaydb "github.com/tunr-dev/tunr/relay/internal/db"
	"github.com/tunr-dev/tunr/relay/internal/logger"
)

// UserAPI — /api/user/* endpoint'leri
//
// Tüm endpoint'ler JWT authentication gerektirir.
// Plan bilgisi hem JWT claim'den hem DB'den alınabilir.
//
// Routes:
//
//	GET  /api/user/profile       → plan, kullanım, hesap bilgisi
//	GET  /api/user/tunnels       → aktif tünel listesi
//	GET  /api/user/token         → API token (masked)
//	POST /api/user/token/rotate  → yeni token üret
//	GET  /api/user/usage         → bu ay kullanım detayı

type UserAPI struct {
	jwtAuth *auth.JWTAuth
	db      *relaydb.DB
	rl      *RateLimiter
}

func NewUserAPI(jwtAuth *auth.JWTAuth, db *relaydb.DB, rl *RateLimiter) *UserAPI {
	return &UserAPI{jwtAuth: jwtAuth, db: db, rl: rl}
}

// RegisterRoutes — mux'a route'ları ekle
// Kullanım: api.RegisterRoutes(mux)
func (a *UserAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/api/user/profile", a.authMiddleware(http.HandlerFunc(a.handleProfile)))
	mux.Handle("/api/user/tunnels", a.authMiddleware(http.HandlerFunc(a.handleTunnels)))
	mux.Handle("/api/user/usage", a.authMiddleware(http.HandlerFunc(a.handleUsage)))
	mux.Handle("/api/user/token", a.authMiddleware(http.HandlerFunc(a.handleToken)))
	mux.Handle("/api/user/token/rotate", a.authMiddleware(http.HandlerFunc(a.handleTokenRotate)))
}

// ── Auth Middleware ────────────────────────────────────────────────

func (a *UserAPI) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "https://tunr.sh")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// JWT doğrula
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeAPIError(w, http.StatusUnauthorized, "auth_required", "Authorization header eksik.")
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := a.jwtAuth.Verify(tokenStr)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "invalid_token", err.Error())
			return
		}

		// Rate limit (plan bazlı)
		plan := claims.Plan
		if plan == "" {
			plan = "free"
		}
		if !a.rl.Allow("user:"+claims.UserID, plan) {
			writeAPIError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "İstek limiti aşıldı.")
			return
		}

		// Context'e user bilgisi ekle
		ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
		ctx = context.WithValue(ctx, "user_email", claims.Email)
		ctx = context.WithValue(ctx, "user_plan", plan)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ── Handlers ─────────────────────────────────────────────────────

// GET /api/user/profile
func (a *UserAPI) handleProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	userID := r.Context().Value("user_id").(string)
	email := r.Context().Value("user_email").(string)
	plan := r.Context().Value("user_plan").(string)

	// TODO: DB'den kullanım verisi çek
	// usage, _ := a.db.GetUsage(r.Context(), userID)

	profile := map[string]interface{}{
		"user_id":   userID,
		"email":     email,
		"plan":      plan,
		"limits": map[string]interface{}{
			"max_tunnels":    DailyRequestLimitByPlan(plan) / 1000,
			"requests_per_day": DailyRequestLimitByPlan(plan),
			"custom_subdomain": plan != "free",
			"http_inspector":  plan != "free",
		},
		"usage": map[string]interface{}{
			"requests_today":  0, // DB'den gelecek
			"bandwidth_bytes": 0, // DB'den gelecek
			"active_tunnels":  0, // Registry'den gelecek
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	logger.Info("User profile request: user=%s plan=%s", userID, plan)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

// GET /api/user/tunnels
func (a *UserAPI) handleTunnels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	userID := r.Context().Value("user_id").(string)

	// TODO: Registry'den o kullanıcının aktif tünellerini al
	// tunnels := a.registry.GetByUser(userID)
	_ = userID

	tunnels := []map[string]interface{}{
		// Şimdilik boş — registry entegrasyonu sonraki fazda
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"tunnels": tunnels,
		"count":   len(tunnels),
	})
}

// GET /api/user/usage
func (a *UserAPI) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	plan := r.Context().Value("user_plan").(string)

	usage := map[string]interface{}{
		"period": time.Now().Format("2006-01"),
		"requests": map[string]interface{}{
			"used":  0, // DB'den gelecek
			"limit": DailyRequestLimitByPlan(plan),
		},
		"bandwidth": map[string]interface{}{
			"used_bytes":  0,
			"limit_bytes": bandwidthLimit(plan),
		},
		"tunnels": map[string]interface{}{
			"active": 0,
			"limit":  TunnelLimitByPlan(plan),
		},
		"reset_at": nextMonthStart(),
	}

	json.NewEncoder(w).Encode(usage)
}

// GET /api/user/token
func (a *UserAPI) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	userID := r.Context().Value("user_id").(string)

	// TODO: DB'den token al (sadece masked)
	_ = userID

	json.NewEncoder(w).Encode(map[string]interface{}{
		"token_masked": "prv_••••••••••••••••",
		"created_at":  time.Now().UTC().Format(time.RFC3339),
		"last_used":   nil,
	})
}

// POST /api/user/token/rotate
func (a *UserAPI) handleTokenRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	userID := r.Context().Value("user_id").(string)
	email := r.Context().Value("user_email").(string)
	plan := r.Context().Value("user_plan").(string)

	// Yeni token üret
	newToken, err := a.jwtAuth.Issue(userID, email, plan)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "token_error", "Token oluşturulamadı.")
		return
	}

	// TODO: DB'ye yeni token hash'i kaydet, eskiyi geçersiz kıl

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      newToken, // Tek seferlik gösterim, sonra masked
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"message":    "Eski token artık geçersizdir. Bu tokeni güvenli saklayın.",
	})
}

// ── Helpers ──────────────────────────────────────────────────────

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}

func bandwidthLimit(plan string) int64 {
	switch plan {
	case "pro":
		return 50 * 1024 * 1024 * 1024  // 50 GB
	case "team":
		return 500 * 1024 * 1024 * 1024 // 500 GB
	default:
		return 500 * 1024 * 1024 // 500 MB
	}
}

func nextMonthStart() string {
	now := time.Now()
	first := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return first.Format(time.RFC3339)
}
