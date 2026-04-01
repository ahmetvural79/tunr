package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/auth"
	"github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
	"github.com/ahmetvural79/tunr/relay/internal/relay"
)

// tunr relay sunucusu.
// Bunu bir sunucuya deploy et, CLI bu sunucuya bağlanır.
// DNS: *.tunr.sh → bu sunucu
//
// Ortam değişkenleri (zorunlu):
//   TUNR_DOMAIN      = tunr.sh
//   TUNR_JWT_SECRET  = (32+ karakter random string)
//   DATABASE_URL      = postgres://...
//   PORT              = 8080 (opsiyonel, default 8080)
//   TUNR_LOG_LEVEL   = debug|info|warn (opsiyonel)

func main() {
	// Config al
	cfg := loadConfig()

	logger.Info("tunr relay başlatılıyor (domain: %s, port: %s)", cfg.Domain, cfg.Port)

	// DB bağlantısı
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var database *db.DB
	if cfg.DatabaseURL != "" {
		var err error
		database, err = db.New(ctx, cfg.DatabaseURL)
		if err != nil {
			// DB olmadan da çalışabiliriz (in-memory mod)
			logger.Warn("DB bağlantısı kurulamadı: %v (in-memory modda devam)", err)
		} else {
			defer database.Close()
			logger.Info("PostgreSQL bağlantısı kuruldu")
		}
	} else {
		logger.Warn("DATABASE_URL ayarlanmamış — in-memory mod (persistent değil)")
	}

	// JWT auth
	jwtAuth, err := auth.NewJWTAuth(cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		logger.Fatal("JWT auth oluşturulamadı: %v", err)
	}

	// Bileşenler
	registry := relay.NewRegistry()
	handler := relay.NewHandler(registry, jwtAuth, database, cfg.Domain)
	proxy := relay.NewProxy(registry, cfg.Domain)
	rateLimiter := relay.NewRateLimiter()
	userAPI := relay.NewUserAPI(jwtAuth, database, rateLimiter, registry, cfg.Domain)

	// HTTP sunucusu
	mux := http.NewServeMux()

	// ── Tunnel bağlantı noktası ──
	// tunr CLI bu endpoint'e WebSocket bağlantısı açar
	mux.HandleFunc("/tunnel/connect", handler.ServeTunnel)

	// ── Browser TCP WebSocket endpoint ──
	// Browsers connect to TCP tunnels via WebSocket
	mux.HandleFunc("/tunnel/tcp", handler.ServeBrowserTCP)

	// ── Auth endpoints ──
	mux.HandleFunc("/auth/magic", handleMagicRequest(database, jwtAuth, cfg.Domain))
	mux.HandleFunc("/auth/verify", handleMagicVerify(database, jwtAuth))

	// ── API endpoints ──
	mux.HandleFunc("/api/v1/status", handleStatus(registry))
	mux.HandleFunc("/api/v1/health", handleHealth())
	userAPI.RegisterRoutes(mux)
	if cfg.PaddleWebhookSecret != "" {
		paddleWebhook := relay.NewPaddleWebhookHandler(database, cfg.PaddleWebhookSecret, relay.PaddlePlanConfig{
			ProPriceID:      cfg.PaddleProPriceID,
			TeamPriceID:     cfg.PaddleTeamPriceID,
			ProProductID:    cfg.PaddleProProductID,
			TeamProductID:   cfg.PaddleTeamProductID,
			DefaultPaidPlan: cfg.PaddleDefaultPaidPlan,
		})
		mux.Handle("/webhook/paddle", paddleWebhook)
		logger.Info("Paddle webhook endpoint enabled: /webhook/paddle")
	} else {
		logger.Warn("PADDLE_WEBHOOK_SECRET not set — /webhook/paddle is disabled")
	}

	// ── Tüm diğer istekler → tunnel proxy ──
	// Subdomain bazlı routing buradan geçer
	mux.HandleFunc("/", proxy.ServeHTTP)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: withBaseMiddleware(mux),

		// Timeout'lar — yavaş client'ları sonsonlandır
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout yok — tunnel proxy uzun sürebilir
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		logger.Info("Kapatıl iyor... (aktif tunnellar tamamlanıyor)")
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			logger.Warn("Shutdown hatası: %v", err)
		}
	}()

	logger.Info("Relay hazır — :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Fatal("Sunucu hatası: %v", err)
	}
	logger.Info("Relay kapatıldı. Görüşmek üzere! 👋")
}

// ─── Middleware ───────────────────────────────────────────────────────────────

func withBaseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GÜVENLİK: Temel güvenlik header'ları
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// HSTS (Caddy/nginx bunu da koyacak ama çift katman zarar vermez)
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		// Request ID — tracing için
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r)
	})
}

// ─── Auth Handlers ────────────────────────────────────────────────────────────

func handleMagicRequest(database *db.DB, _ *auth.JWTAuth, domain string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
			http.Error(w, "email gerekli", http.StatusBadRequest)
			return
		}

		// Magic token üret
		token, err := auth.GenerateMagicToken()
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		// DB'ye kaydet (15 dakika geçerli)
		if database != nil {
			if err := database.SaveMagicToken(r.Context(), token, req.Email,
				time.Now().Add(15*time.Minute)); err != nil {
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
		}

		// Magic link göster (production'da e-posta gönderilir)
		magicLink := auth.MagicLink("https://"+domain, req.Email, token)

		// Production'da: sendEmail(req.Email, magicLink)
		// Dev'de: log'a bas
		logger.Info("Magic link: %s", magicLink)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "Magic link e-posta adresinize gönderildi",
			"email":   req.Email,
			// GÜVENLİK: production'da link'i response'a ekleme!
			// Sadece dev ortamında döndür:
			"_dev_link": magicLink,
		})
	}
}

func handleMagicVerify(database *db.DB, jwtAuth *auth.JWTAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "token gerekli", http.StatusBadRequest)
			return
		}

		var email string
		var err error
		if database != nil {
			// Token'ı tüket (tek kullanımlık)
			email, err = database.ConsumeMagicToken(r.Context(), token)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
		} else {
			// DB yoksa token'ı doğrulayamıyoruz — dev mode
			email = "dev@tunr.sh"
		}

		// Kullanıcı oluştur/bul
		var userID string
		if database != nil {
			user, _, err := database.GetOrCreateUser(r.Context(), email)
			if err != nil {
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			userID = user.ID
		} else {
			userID = "dev-user-id"
		}

		// JWT token oluştur
		jwt, err := jwtAuth.Issue(userID, email, "free")
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":   jwt,
			"user_id": userID,
			"email":   email,
		})
	}
}

// ─── Status & Health ──────────────────────────────────────────────────────────

func handleStatus(registry *relay.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(registry.Stats())
	}
}

func handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
		})
	}
}

// ─── Config ──────────────────────────────────────────────────────────────────

type Config struct {
	Domain               string
	Port                 string
	JWTSecret            string
	DatabaseURL          string
	PaddleWebhookSecret  string
	PaddleProPriceID     string
	PaddleTeamPriceID    string
	PaddleProProductID   string
	PaddleTeamProductID  string
	PaddleDefaultPaidPlan string
}

func loadConfig() Config {
	cfg := Config{
		Domain:                getEnv("TUNR_DOMAIN", "tunr.sh"),
		Port:                  getEnv("PORT", "8080"),
		JWTSecret:             getEnv("TUNR_JWT_SECRET", ""),
		DatabaseURL:           getEnv("DATABASE_URL", ""),
		PaddleWebhookSecret:   getEnv("PADDLE_WEBHOOK_SECRET", ""),
		PaddleProPriceID:      getEnv("PADDLE_PRO_PRICE_ID", ""),
		PaddleTeamPriceID:     getEnv("PADDLE_TEAM_PRICE_ID", ""),
		PaddleProProductID:    getEnv("PADDLE_PRO_PRODUCT_ID", ""),
		PaddleTeamProductID:   getEnv("PADDLE_TEAM_PRODUCT_ID", ""),
		PaddleDefaultPaidPlan: getEnv("PADDLE_DEFAULT_PAID_PLAN", "pro"),
	}

	// GÜVENLİK: JWT secret zorunlu
	if cfg.JWTSecret == "" {
		logger.Fatal("TUNR_JWT_SECRET env değişkeni ayarlanmamış. En az 32 karakter random string girin.")
	}
	if len(cfg.JWTSecret) < 32 {
		logger.Fatal("TUNR_JWT_SECRET çok kısa (%d karakter). En az 32 karakter olmalı.", len(cfg.JWTSecret))
	}

	// Log seviyesi
	if os.Getenv("TUNR_LOG_LEVEL") == "debug" {
		logger.SetLevel(logger.DEBUG)
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
