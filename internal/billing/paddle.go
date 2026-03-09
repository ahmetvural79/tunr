package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tunr-dev/tunr/internal/logger"
)

// Paddle — subscription fiyatlandırma.
// Neden Paddle? VAT hesaplamasını onlar hallediyor,
// GDPR uyumluluğu var, Türkiye dâhil 200+ ülkede ödeme alınabiliyor.
// Stripe kadar yaygın değil ama developer araçları için daha adil.

// Plan - kullanıcının plan seviyesi
type Plan string

const (
	PlanFree Plan = "free"
	PlanPro  Plan = "pro"
	PlanTeam Plan = "team"
)

// PlanLimits - her plan için kota sınırları
type PlanLimits struct {
	MaxTunnels     int
	MaxRequestsDay int  // günlük request limiti
	CustomDomain   bool
	LogsRetention  int  // gün
	TeamMembers    int
}

// Planların kota sınırları
var planLimits = map[Plan]PlanLimits{
	PlanFree: {
		MaxTunnels:     1,
		MaxRequestsDay: 1000,
		CustomDomain:   false,
		LogsRetention:  1,
		TeamMembers:    0,
	},
	PlanPro: {
		MaxTunnels:     10,
		MaxRequestsDay: 100_000,
		CustomDomain:   true,
		LogsRetention:  30,
		TeamMembers:    0,
	},
	PlanTeam: {
		MaxTunnels:     100,
		MaxRequestsDay: 1_000_000,
		CustomDomain:   true,
		LogsRetention:  90,
		TeamMembers:    10,
	},
}

// PaddleClient - Paddle API ile konuşur
type PaddleClient struct {
	apiKey    string // GÜVENLİK: asla log'a geçmez
	webhookSecret string // GÜVENLİK: webhook imza doğrulaması için
	sandbox   bool   // test ortamı mı?
	httpClient *http.Client
}

// baseURL - sandbox veya production
func (c *PaddleClient) baseURL() string {
	if c.sandbox {
		return "https://sandbox-api.paddle.com"
	}
	return "https://api.paddle.com"
}

// NewPaddleClient - Paddle client oluştur
// apiKey: Paddle dashboard'dan alınan API key
// webhookSecret: webhook imza doğrulaması için
func NewPaddleClient(apiKey, webhookSecret string, sandbox bool) *PaddleClient {
	return &PaddleClient{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		sandbox:       sandbox,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			// GÜVENLİK: TLS verify kapatılmaz, default olarak aktif
		},
	}
}

// SubscriptionStatus - Paddle abonelik durumu
type SubscriptionStatus string

const (
	SubActive   SubscriptionStatus = "active"
	SubTrialing SubscriptionStatus = "trialing"
	SubCanceled SubscriptionStatus = "canceled"
	SubPaused   SubscriptionStatus = "paused"
	SubPastDue  SubscriptionStatus = "past_due"
)

// Subscription - Paddle abonelik bilgisi
type Subscription struct {
	ID           string             `json:"id"`
	Status       SubscriptionStatus `json:"status"`
	Plan         Plan               `json:"plan"`
	CustomerID   string             `json:"customer_id"`
	CurrentPeriodEnd time.Time      `json:"current_period_end"`
}

// GetSubscription - müşterinin aktif aboneliğini getir
func (c *PaddleClient) GetSubscription(ctx context.Context, customerID string) (*Subscription, error) {
	// GÜVENLİK: customerID'yi URL'e gömer, sanitize et
	if strings.ContainsAny(customerID, "/?&=#") {
		return nil, fmt.Errorf("geçersiz customer ID formatı")
	}

	url := fmt.Sprintf("%s/subscriptions?customer_id=%s&status=active", c.baseURL(), customerID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("request oluşturulamadı: %w", err)
	}

	// GÜVENLİK: API key Bearer token olarak gönderilir
	// Authorization header'ı log'a yazma!
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Paddle-Version", "1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tunr-cli/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Paddle API isteği başarısız: %w", err)
	}
	defer resp.Body.Close()

	// Rate limit kontrolü
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("Paddle API rate limit aşıldı, biraz bekleyin")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		// API key'i log'a yazma! Sadece hata söyle.
		return nil, fmt.Errorf("Paddle API kimlik doğrulama hatası (API key kontrol edin)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Paddle API hatası (status %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // max 64KB oku
	if err != nil {
		return nil, fmt.Errorf("response okunamadı: %w", err)
	}

	var result struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Items  []struct {
				Price struct {
					ProductID string `json:"product_id"`
				} `json:"price"`
			} `json:"items"`
			CurrentBillingPeriod struct {
				EndsAt string `json:"ends_at"`
			} `json:"current_billing_period"`
			CustomerID string `json:"customer_id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("response parse hatası: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, nil // aktif abonelik yok
	}

	sub := result.Data[0]
	endTime, _ := time.Parse(time.RFC3339, sub.CurrentBillingPeriod.EndsAt)

	return &Subscription{
		ID:               sub.ID,
		Status:           SubscriptionStatus(sub.Status),
		CustomerID:       sub.CustomerID,
		CurrentPeriodEnd: endTime,
		// Plan mapping product ID'ye göre yapılır (Faz 3'te config'den okunacak)
		Plan: PlanPro,
	}, nil
}

// IsPro - bu müşteri Pro veya Team planında mı?
func (c *PaddleClient) IsPro(ctx context.Context, customerID string) (bool, error) {
	sub, err := c.GetSubscription(ctx, customerID)
	if err != nil {
		return false, err
	}
	if sub == nil {
		return false, nil
	}

	active := sub.Status == SubActive || sub.Status == SubTrialing
	isPro := sub.Plan == PlanPro || sub.Plan == PlanTeam

	return active && isPro, nil
}

// GetLimits - müşterinin plan kotalarını getir
func (c *PaddleClient) GetLimits(ctx context.Context, customerID string) PlanLimits {
	sub, err := c.GetSubscription(ctx, customerID)
	if err != nil {
		logger.Warn("Plan limiti alınamadı, free plan limitleri uygulanıyor: %v", err)
		return planLimits[PlanFree]
	}

	if sub == nil {
		return planLimits[PlanFree]
	}

	limits, ok := planLimits[sub.Plan]
	if !ok {
		return planLimits[PlanFree]
	}
	return limits
}

// ─── WEBHOOK HANDLER ────────────────────────────────────────────────────────

// WebhookEvent - Paddle'dan gelen webhook olayı
type WebhookEvent struct {
	EventID   string          `json:"event_id"`
	EventType string          `json:"event_type"`
	OccurredAt string         `json:"occurred_at"`
	Data      json.RawMessage `json:"data"`
}

// VerifyWebhookSignature - Paddle webhook imzasını doğrula
// GÜVENLİK: Bu kritik! İmzasız webhook → billing bypass riski.
// Her gelen webhook'un gerçekten Paddle'dan geldiğini doğrularız.
func (c *PaddleClient) VerifyWebhookSignature(payload []byte, signatureHeader string) error {
	// Paddle-Signature header formatı: ts=1234567890;h1=HMAC_HASH
	parts := strings.Split(signatureHeader, ";")
	if len(parts) < 2 {
		return fmt.Errorf("geçersiz webhook imza formatı")
	}

	var timestamp, signature string
	for _, part := range parts {
		if strings.HasPrefix(part, "ts=") {
			timestamp = strings.TrimPrefix(part, "ts=")
		}
		if strings.HasPrefix(part, "h1=") {
			signature = strings.TrimPrefix(part, "h1=")
		}
	}

	if timestamp == "" || signature == "" {
		return fmt.Errorf("webhook imzası eksik (ts veya h1 yok)")
	}

	// Replay attack koruması: timestamp 5 dakikadan eskiyse reddet
	// Biri eski bir webhook'u tekrar göndermeye çalışıyor olabilir
	ts, err := parseTimestamp(timestamp)
	if err != nil {
		return fmt.Errorf("webhook timestamp parse hatası: %w", err)
	}
	age := time.Since(ts)
	if age > 5*time.Minute || age < -1*time.Minute {
		return fmt.Errorf("webhook timestamp çok eski veya gelecekte (%v)", age)
	}

	// HMAC-SHA256 doğrulaması
	// signed_payload = timestamp + ":" + body
	signedPayload := timestamp + ":" + string(payload)

	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write([]byte(signedPayload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	// GÜVENLİK: hmac.Equal timing-safe karşılaştırma
	// String == operatörü timing attack'a açık, bunu kullanmıyoruz
	gotSigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("imza hex decode hatası: %w", err)
	}
	expectedSigBytes, _ := hex.DecodeString(expectedSig)

	if !hmac.Equal(gotSigBytes, expectedSigBytes) {
		return fmt.Errorf("webhook imzası doğrulanamadı (sahte veya değiştirilmiş istek?)")
	}

	return nil
}

// HandleWebhook - gelen webhook olayını işle
// HTTP handler olarak kullanılabilir
func (c *PaddleClient) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Sadece POST kabul et
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Body'yi oku (max 1MB, Paddle webhook'ları çok büyük olmamalı)
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "body okunamadı", http.StatusBadRequest)
		return
	}

	// İmzayı doğrula
	sig := r.Header.Get("Paddle-Signature")
	if err := c.VerifyWebhookSignature(body, sig); err != nil {
		// GÜVENLİK: Hata detayını client'a verme, sadece logla
		logger.Warn("Webhook imza doğrulaması başarısız: %v", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Olayı parse et
	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "geçersiz payload", http.StatusBadRequest)
		return
	}

	// Olay tipine göre işle
	switch event.EventType {
	case "subscription.activated", "subscription.updated":
		logger.Info("Paddle webhook: abonelik güncellendi (%s)", event.EventID)
		// TODO: kullanıcı planını DB'de güncelle
	case "subscription.canceled":
		logger.Info("Paddle webhook: abonelik iptal edildi (%s)", event.EventID)
		// TODO: kullanıcıyı free plana düşür
	case "subscription.past_due":
		logger.Warn("Paddle webhook: ödeme gecikmesi (%s)", event.EventID)
		// TODO: kullanıcıya bildirim gönder
	case "transaction.completed":
		logger.Info("Paddle webhook: ödeme tamamlandı (%s)", event.EventID)
	default:
		logger.Debug("Bilinmeyen Paddle webhook eventi: %s", event.EventType)
	}

	// Her zaman 200 dön - Paddle 2xx görürse başarılı sayar
	// Hata dönersen tekrar gönderir (retry fırtınası başlar)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, `{"status":"ok"}`)
}

// parseTimestamp - "1234567890" formatındaki timestamp'i parse et
func parseTimestamp(s string) (time.Time, error) {
	var unix int64
	_, err := fmt.Sscanf(s, "%d", &unix)
	if err != nil {
		return time.Time{}, fmt.Errorf("geçersiz timestamp: %s", s)
	}
	return time.Unix(unix, 0), nil
}

// CreateCheckoutSession - Paddle checkout session oluştur
// (Landing page'deki "Satın Al" butonuna tıklanınca çağrılır)
func (c *PaddleClient) CreateCheckoutSession(ctx context.Context, priceID, customerEmail string) (string, error) {
	// GÜVENLİK: email formatını doğrula
	if !isValidEmail(customerEmail) {
		return "", fmt.Errorf("geçersiz e-posta formatı")
	}

	payload := map[string]interface{}{
		"items": []map[string]interface{}{
			{"price_id": priceID, "quantity": 1},
		},
		"customer": map[string]string{
			"email": customerEmail,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := c.baseURL() + "/transactions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Paddle-Version", "1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checkout session oluşturulamadı (status %d)", resp.StatusCode)
	}

	var result struct {
		Data struct {
			ID      string `json:"id"`
			Details struct {
				LineItems []interface{} `json:"line_items"`
			} `json:"details"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Data.ID, nil
}

// isValidEmail - basit email validasyonu
func isValidEmail(email string) bool {
	if len(email) > 254 || len(email) < 3 {
		return false
	}
	atIdx := strings.Index(email, "@")
	if atIdx < 1 || atIdx >= len(email)-2 {
		return false
	}
	domain := email[atIdx+1:]
	return strings.Contains(domain, ".")
}
