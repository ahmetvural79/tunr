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

	"github.com/ahmetvural79/tunr/internal/logger"
)

// Paddle handles subscription billing.
// We picked Paddle over Stripe because they deal with VAT, GDPR,
// and accept payments in 200+ countries without us doing the tax math.

// Plan represents a user's subscription tier
type Plan string

const (
	PlanFree Plan = "free"
	PlanPro  Plan = "pro"
	PlanTeam Plan = "team"
)

// PlanLimits defines the quota boundaries for each plan
type PlanLimits struct {
	MaxTunnels     int
	MaxRequestsDay int  // daily request cap
	CustomDomain   bool
	LogsRetention  int  // days
	TeamMembers    int
}

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

// PaddleClient talks to the Paddle Billing API
type PaddleClient struct {
	apiKey        string // SECURITY: never written to logs
	webhookSecret string // SECURITY: used for webhook signature verification
	sandbox       bool
	httpClient    *http.Client
}

func (c *PaddleClient) baseURL() string {
	if c.sandbox {
		return "https://sandbox-api.paddle.com"
	}
	return "https://api.paddle.com"
}

// NewPaddleClient creates a Paddle client.
// webhookSecret is required for verifying inbound webhook signatures.
func NewPaddleClient(apiKey, webhookSecret string, sandbox bool) *PaddleClient {
	return &PaddleClient{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		sandbox:       sandbox,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			// SECURITY: TLS verification stays on — never disable it
		},
	}
}

// SubscriptionStatus mirrors Paddle's subscription state machine
type SubscriptionStatus string

const (
	SubActive   SubscriptionStatus = "active"
	SubTrialing SubscriptionStatus = "trialing"
	SubCanceled SubscriptionStatus = "canceled"
	SubPaused   SubscriptionStatus = "paused"
	SubPastDue  SubscriptionStatus = "past_due"
)

// Subscription holds Paddle subscription details
type Subscription struct {
	ID               string             `json:"id"`
	Status           SubscriptionStatus `json:"status"`
	Plan             Plan               `json:"plan"`
	CustomerID       string             `json:"customer_id"`
	CurrentPeriodEnd time.Time          `json:"current_period_end"`
}

// GetSubscription fetches the customer's active subscription from Paddle
func (c *PaddleClient) GetSubscription(ctx context.Context, customerID string) (*Subscription, error) {
	// SECURITY: customerID goes into the URL path — sanitize it
	if strings.ContainsAny(customerID, "/?&=#") {
		return nil, fmt.Errorf("invalid customer ID format")
	}

	url := fmt.Sprintf("%s/subscriptions?customer_id=%s&status=active", c.baseURL(), customerID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// SECURITY: API key sent as Bearer token — do not log the Authorization header
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Paddle-Version", "1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tunr-cli/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Paddle API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("Paddle API rate limit exceeded, try again shortly")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		// SECURITY: do not leak the API key in error messages — just say it failed
		return nil, fmt.Errorf("Paddle API authentication failed (check your API key)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Paddle API error (status %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
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
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, nil // no active subscription
	}

	sub := result.Data[0]
	endTime, _ := time.Parse(time.RFC3339, sub.CurrentBillingPeriod.EndsAt)

	return &Subscription{
		ID:               sub.ID,
		Status:           SubscriptionStatus(sub.Status),
		CustomerID:       sub.CustomerID,
		CurrentPeriodEnd: endTime,
		// Plan mapping via product ID — will read from config in Phase 3
		Plan: PlanPro,
	}, nil
}

// IsPro checks whether the customer is on a Pro or Team plan
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

// GetLimits returns the quota limits for a customer's current plan.
// Falls back to free-tier limits on any error — better safe than sorry.
func (c *PaddleClient) GetLimits(ctx context.Context, customerID string) PlanLimits {
	sub, err := c.GetSubscription(ctx, customerID)
	if err != nil {
		logger.Warn("Failed to fetch plan limits, falling back to free tier: %v", err)
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

// WebhookEvent represents an inbound Paddle webhook event
type WebhookEvent struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	OccurredAt string          `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}

// VerifyWebhookSignature validates the Paddle webhook HMAC signature.
// SECURITY: This is critical — unsigned webhooks would let anyone bypass billing.
// Every inbound webhook must be proven to originate from Paddle.
func (c *PaddleClient) VerifyWebhookSignature(payload []byte, signatureHeader string) error {
	// Paddle-Signature header format: ts=1234567890;h1=HMAC_HASH
	parts := strings.Split(signatureHeader, ";")
	if len(parts) < 2 {
		return fmt.Errorf("invalid webhook signature format")
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
		return fmt.Errorf("webhook signature incomplete (missing ts or h1)")
	}

	// SECURITY: reject webhooks older than 5 minutes to prevent replay attacks
	ts, err := parseTimestamp(timestamp)
	if err != nil {
		return fmt.Errorf("failed to parse webhook timestamp: %w", err)
	}
	age := time.Since(ts)
	if age > 5*time.Minute || age < -1*time.Minute {
		return fmt.Errorf("webhook timestamp out of range (%v)", age)
	}

	// HMAC-SHA256 verification: signed_payload = timestamp + ":" + body
	signedPayload := timestamp + ":" + string(payload)

	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write([]byte(signedPayload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	// SECURITY: hmac.Equal is timing-safe — using == would leak info via timing side-channels
	gotSigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("signature hex decode failed: %w", err)
	}
	expectedSigBytes, _ := hex.DecodeString(expectedSig)

	if !hmac.Equal(gotSigBytes, expectedSigBytes) {
		return fmt.Errorf("webhook signature verification failed (forged or tampered request?)")
	}

	return nil
}

// HandleWebhook processes inbound Paddle webhook events as an HTTP handler
func (c *PaddleClient) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Paddle webhooks shouldn't be huge — cap at 1MB
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("Paddle-Signature")
	if err := c.VerifyWebhookSignature(body, sig); err != nil {
		// SECURITY: don't expose verification details to the client, just log it
		logger.Warn("Webhook signature verification failed: %v", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	switch event.EventType {
	case "subscription.activated", "subscription.updated":
		logger.Info("Paddle webhook: subscription updated (%s)", event.EventID)
		// TODO: update user plan in DB
	case "subscription.canceled":
		logger.Info("Paddle webhook: subscription canceled (%s)", event.EventID)
		// TODO: downgrade user to free plan
	case "subscription.past_due":
		logger.Warn("Paddle webhook: payment past due (%s)", event.EventID)
		// TODO: notify user about overdue payment
	case "transaction.completed":
		logger.Info("Paddle webhook: payment completed (%s)", event.EventID)
	default:
		logger.Debug("Unknown Paddle webhook event: %s", event.EventType)
	}

	// Always return 200 — Paddle treats any 2xx as success.
	// Return an error and you'll trigger a retry storm.
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, `{"status":"ok"}`)
}

func parseTimestamp(s string) (time.Time, error) {
	var unix int64
	_, err := fmt.Sscanf(s, "%d", &unix)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp: %s", s)
	}
	return time.Unix(unix, 0), nil
}

// CreateCheckoutSession starts a Paddle checkout flow (called when "Buy" is clicked)
func (c *PaddleClient) CreateCheckoutSession(ctx context.Context, priceID, customerEmail string) (string, error) {
	// SECURITY: validate email format before sending to Paddle
	if !isValidEmail(customerEmail) {
		return "", fmt.Errorf("invalid email format")
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
		return "", fmt.Errorf("failed to create checkout session (status %d)", resp.StatusCode)
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

// isValidEmail does a quick-and-dirty email sanity check — not RFC 5322, but good enough
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
