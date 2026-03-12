package relay

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	relaydb "github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// RateLimiter — IP başına sliding window rate limiter
//
// GÜVENLİK:
//   - Free kullanıcılar: 200 req/dk
//   - Pro kullanıcılar: 2.000 req/dk
//   - Anonim: 50 req/dk (IP başına)
//
// Production'da Redis kullanın (multi-instance için)
// Bu implementasyon tek sunucu için in-memory çalışır.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	cleanup *time.Ticker
}

type bucket struct {
	tokens    int
	maxTokens int
	refillAt  time.Time
}

// Plan bazlı rate limit değerleri (istek/dakika)
var planLimits = map[string]int{
	"free": 200,
	"pro":  2000,
	"team": 10000,
	"anon": 50,
}

// NewRateLimiter — rate limiter oluştur
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		cleanup: time.NewTicker(5 * time.Minute),
	}
	go rl.cleanupLoop()
	return rl
}

// Allow — istek izin verilip verilmeyeceğini kontrol et
// key: "ip:1.2.3.4" veya "user:uid_abc" gibi
// plan: "free" | "pro" | "team" | "anon"
func (rl *RateLimiter) Allow(key, plan string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit, ok := planLimits[plan]
	if !ok {
		limit = planLimits["anon"]
	}

	b, exists := rl.buckets[key]
	now := time.Now()

	if !exists || now.After(b.refillAt) {
		// Yeni bucket veya pencere doldu — sıfırla
		rl.buckets[key] = &bucket{
			tokens:    limit - 1,
			maxTokens: limit,
			refillAt:  now.Add(time.Minute),
		}
		return true
	}

	if b.tokens <= 0 {
		return false // limit aşıldı
	}

	b.tokens--
	return true
}

// cleanupLoop — süresi dolmuş bucket'ları temizle
func (rl *RateLimiter) cleanupLoop() {
	for range rl.cleanup.C {
		rl.mu.Lock()
		now := time.Now()
		for key, b := range rl.buckets {
			if now.After(b.refillAt.Add(time.Minute)) {
				delete(rl.buckets, key)
			}
		}
		rl.mu.Unlock()
	}
}

// RateLimitMiddleware — HTTP middleware: rate limit uygula
func RateLimitMiddleware(rl *RateLimiter, plan string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := realIP(r)
			key := "ip:" + ip

			if !rl.Allow(key, plan) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":       "rate_limit_exceeded",
					"message":     "İstek limitinizi aştınız. 1 dakika bekleyin veya planınızı yükseltin.",
					"upgrade_url": "https://tunr.sh/#pricing",
				})
				logger.Warn("Rate limit: ip=%s plan=%s", ip, plan)
				return
			}

			w.Header().Set("X-RateLimit-Limit", "200")
			w.Header().Set("X-RateLimit-Remaining", "unknown") // Gerçek değer için Redis
			next.ServeHTTP(w, r)
		})
	}
}

// ── Plan Enforcement Middleware ──────────────────────────────────

// PlanClaims — JWT token'daki plan bilgisi
// jwt.go'daki Claims struct'a eklenmeli:
//
//	type Claims struct {
//	    jwt.RegisteredClaims
//	    UserID string `json:"uid"`
//	    Email  string `json:"email"`
//	    Plan   string `json:"plan"` // ← yeni
//	}

// PlanEnforcer — plan bazlı özellik erişim kontrolü
type PlanEnforcer struct {
	db *relaydb.DB
}

func NewPlanEnforcer(db *relaydb.DB) *PlanEnforcer {
	return &PlanEnforcer{db: db}
}

// TunnelLimitByPlan — plan bazlı maksimum eşzamanlı tünel sayısı
func TunnelLimitByPlan(plan string) int {
	switch plan {
	case "pro":
		return 10
	case "team":
		return 100
	default: // free, anon
		return 1
	}
}

// DailyRequestLimitByPlan — plan bazlı günlük istek limiti
func DailyRequestLimitByPlan(plan string) int {
	switch plan {
	case "pro":
		return 100_000
	case "team":
		return 1_000_000
	default:
		return 1_000
	}
}

// RequireProFeature — Pro+ özellik middleware
// Kullanım: router.Handle("/api/custom-subdomain", RequireProFeature(handler))
func RequireProFeature(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		planVal := r.Context().Value(ctxKeyUserPlan)
		plan, _ := planVal.(string)
		if plan == "" || plan == "free" || plan == "anon" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":       "pro_required",
				"message":     "Bu özellik Pro veya Team planı gerektirir.",
				"upgrade_url": "https://tunr.sh/#pricing",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
