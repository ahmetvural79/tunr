package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// Audit API — enterprise müşteriler için tam denetim kaydı.
//
// SOC2, ISO 27001, GDPR Art.30 gereksinimlerini karşılar.
// Kim, ne zaman, ne yaptı — her önemli eylem burada.
//
// Endpoint:
//   GET /api/v1/audit?from=2026-01-01&to=2026-03-08&action=tunnel.created&limit=100

// AuditEvent — bir denetim kaydı
type AuditEvent struct {
	ID        int64                  `json:"id"`
	UserID    string                 `json:"user_id"`
	Email     string                 `json:"email,omitempty"`
	Action    string                 `json:"action"`
	Detail    map[string]interface{} `json:"detail,omitempty"`
	IPAddress string                 `json:"ip_address,omitempty"`
	UserAgent string                 `json:"user_agent,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

// Tanımlı eylemler — string sabitler yerine type kullanmak daha güvenli
const (
	// Auth eylemleri
	ActionLogin          = "auth.login"
	ActionLogout         = "auth.logout"
	ActionLoginFailed    = "auth.login_failed"
	ActionMagicTokenUsed = "auth.magic_token_used"

	// Tunnel eylemleri
	ActionTunnelCreated      = "tunnel.created"
	ActionTunnelClosed       = "tunnel.closed"
	ActionTunnelLimitReached = "tunnel.limit_reached"

	// Plan eylemleri
	ActionPlanUpgraded   = "plan.upgraded"
	ActionPlanDowngraded = "plan.downgraded"
	ActionPlanCancelled  = "plan.cancelled"

	// Güvenlik eylemleri
	ActionWebhookVerified  = "security.webhook_verified"
	ActionWebhookRejected  = "security.webhook_rejected"
	ActionSuspiciousRequest = "security.suspicious_request"
	ActionRateLimitHit     = "security.rate_limit_hit"

	// Admin eylemleri
	ActionAdminUserBanned   = "admin.user_banned"
	ActionAdminDataExport   = "admin.data_export"
	ActionAdminConfigChange = "admin.config_changed"
)

// APIHandler — denetim loglarını sunan HTTP handler
type APIHandler struct {
	querier Querier
}

// Querier — DB sorgu arayüzü (test için mock edilebilir)
type Querier interface {
	QueryAuditLog(ctx context.Context, filter AuditFilter) ([]*AuditEvent, int64, error)
}

// AuditFilter — filtreleme parametreleri
type AuditFilter struct {
	UserID    string
	Action    string
	From      time.Time
	To        time.Time
	Limit     int
	Offset    int
}

// NewAPIHandler — oluşturucu
func NewAPIHandler(q Querier) *APIHandler {
	return &APIHandler{querier: q}
}

// ServeHTTP — GET /api/v1/audit endpoint'i
func (h *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// GÜVENLİK: Sadece admin veya hesap sahibi erişebilir
	// Bu middleware tarafından sağlanır (burada JWT claims kontrol edilmiş)
	q := r.URL.Query()

	filter := AuditFilter{
		Action: q.Get("action"),
		Limit:  100, // varsayılan
	}

	// Limit
	if lStr := q.Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil {
			if l > 0 && l <= 1000 {
				filter.Limit = l
			}
		}
	}

	// Offset (sayfalama)
	if oStr := q.Get("offset"); oStr != "" {
		if o, err := strconv.Atoi(oStr); err == nil && o >= 0 {
			filter.Offset = o
		}
	}

	// Zaman aralığı
	if fromStr := q.Get("from"); fromStr != "" {
		if t, err := time.Parse(time.DateOnly, fromStr); err == nil {
			filter.From = t
		}
	}
	if toStr := q.Get("to"); toStr != "" {
		if t, err := time.Parse(time.DateOnly, toStr); err == nil {
			filter.To = t.Add(24 * time.Hour) // bitiş gününü dahil et
		}
	}

	events, total, err := h.querier.QueryAuditLog(r.Context(), filter)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Sayfalama header'ları (GitHub API tarzı)
	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}

// ─── SDK Export (webhook) ─────────────────────────────────────────────────────

// WebhookEvent — enterprise müşterilere gönderilen webhook
// Kendi sistemlerine real-time audit streaming için
type WebhookEvent struct {
	EventType string      `json:"event_type"`
	Event     AuditEvent  `json:"event"`
	SentAt    time.Time   `json:"sent_at"`
}
