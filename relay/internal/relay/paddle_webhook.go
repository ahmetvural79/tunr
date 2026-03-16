package relay

import (
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

	relaydb "github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
)

// PaddlePlanConfig maps Paddle prices/products to internal plan names.
type PaddlePlanConfig struct {
	ProPriceID     string
	TeamPriceID    string
	ProProductID   string
	TeamProductID  string
	DefaultPaidPlan string
}

// PaddleWebhookHandler receives Paddle events and syncs user plans.
type PaddleWebhookHandler struct {
	db            *relaydb.DB
	webhookSecret string
	planConfig    PaddlePlanConfig
}

func NewPaddleWebhookHandler(db *relaydb.DB, webhookSecret string, planConfig PaddlePlanConfig) *PaddleWebhookHandler {
	if planConfig.DefaultPaidPlan == "" {
		planConfig.DefaultPaidPlan = "pro"
	}
	return &PaddleWebhookHandler{
		db:            db,
		webhookSecret: webhookSecret,
		planConfig:    planConfig,
	}
}

func (h *PaddleWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.db == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if err := verifyPaddleSignature(body, r.Header.Get("Paddle-Signature"), h.webhookSecret); err != nil {
		logger.Warn("Paddle webhook rejected: %v", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var event paddleWebhookEnvelope
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if err := h.handleEvent(r.Context(), event); err != nil {
		logger.Warn("Paddle webhook process failed event=%s id=%s err=%v", event.EventType, event.EventID, err)
		http.Error(w, "processing failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (h *PaddleWebhookHandler) handleEvent(ctx context.Context, event paddleWebhookEnvelope) error {
	switch event.EventType {
	case "subscription.activated", "subscription.updated":
		var data paddleSubscriptionData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return fmt.Errorf("subscription payload parse failed: %w", err)
		}
		if !isSubscriptionActive(data.Status) {
			return h.syncPlan(ctx, subscriptionCustomerID(data), subscriptionEmail(data), "free")
		}
		plan := h.resolvePlan(data.Items, h.planConfig.DefaultPaidPlan)
		return h.syncPlan(ctx, subscriptionCustomerID(data), subscriptionEmail(data), plan)

	case "subscription.canceled", "subscription.paused", "subscription.past_due":
		var data paddleSubscriptionData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return fmt.Errorf("subscription payload parse failed: %w", err)
		}
		return h.syncPlan(ctx, subscriptionCustomerID(data), subscriptionEmail(data), "free")

	case "transaction.completed":
		var data paddleTransactionData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return fmt.Errorf("transaction payload parse failed: %w", err)
		}
		customerID := transactionCustomerID(data)
		email := transactionEmail(data)
		if customerID == "" || email == "" {
			return nil
		}
		_, err := h.db.LinkPaddleCustomerByEmail(ctx, email, customerID)
		return err
	default:
		return nil
	}
}

func (h *PaddleWebhookHandler) resolvePlan(items []paddleItem, fallback string) string {
	for _, item := range items {
		priceID := item.Price.ID
		productID := item.Price.ProductID

		if h.planConfig.TeamPriceID != "" && priceID == h.planConfig.TeamPriceID {
			return "team"
		}
		if h.planConfig.TeamProductID != "" && productID == h.planConfig.TeamProductID {
			return "team"
		}
		if h.planConfig.ProPriceID != "" && priceID == h.planConfig.ProPriceID {
			return "pro"
		}
		if h.planConfig.ProProductID != "" && productID == h.planConfig.ProProductID {
			return "pro"
		}
	}
	if fallback != "" {
		return fallback
	}
	return "pro"
}

func (h *PaddleWebhookHandler) syncPlan(ctx context.Context, customerID, email, newPlan string) error {
	if customerID == "" {
		return fmt.Errorf("missing customer id")
	}

	updated, err := h.db.UpdateUserPlanByCustomerID(ctx, customerID, newPlan)
	if err != nil {
		return err
	}
	if updated {
		logger.Info("Paddle plan synced customer=%s plan=%s", customerID, newPlan)
		return nil
	}

	if email == "" {
		return fmt.Errorf("no user mapped for customer id %s and no email fallback", customerID)
	}
	if _, err := h.db.LinkPaddleCustomerByEmail(ctx, email, customerID); err != nil {
		return err
	}

	updated, err = h.db.UpdateUserPlanByCustomerID(ctx, customerID, newPlan)
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("no user matched customer id %s", customerID)
	}

	logger.Info("Paddle plan synced via email fallback email=%s customer=%s plan=%s", email, customerID, newPlan)
	return nil
}

func isSubscriptionActive(status string) bool {
	switch strings.ToLower(status) {
	case "active", "trialing":
		return true
	default:
		return false
	}
}

func verifyPaddleSignature(payload []byte, signatureHeader, secret string) error {
	if secret == "" {
		return fmt.Errorf("webhook secret is empty")
	}

	parts := strings.Split(signatureHeader, ";")
	if len(parts) < 2 {
		return fmt.Errorf("invalid signature header")
	}

	var ts, h1 string
	for _, part := range parts {
		if strings.HasPrefix(part, "ts=") {
			ts = strings.TrimPrefix(part, "ts=")
		}
		if strings.HasPrefix(part, "h1=") {
			h1 = strings.TrimPrefix(part, "h1=")
		}
	}
	if ts == "" || h1 == "" {
		return fmt.Errorf("signature missing ts or h1")
	}

	tsTime, err := parseUnixTimestamp(ts)
	if err != nil {
		return err
	}
	age := time.Since(tsTime)
	if age > 5*time.Minute || age < -1*time.Minute {
		return fmt.Errorf("timestamp out of range: %v", age)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + ":" + string(payload)))
	expectedHex := hex.EncodeToString(mac.Sum(nil))

	got, err := hex.DecodeString(h1)
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return fmt.Errorf("invalid expected signature hex: %w", err)
	}
	if !hmac.Equal(got, expected) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func parseUnixTimestamp(s string) (time.Time, error) {
	var unix int64
	if _, err := fmt.Sscanf(s, "%d", &unix); err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp")
	}
	return time.Unix(unix, 0), nil
}

type paddleWebhookEnvelope struct {
	EventID   string          `json:"event_id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
}

type paddlePrice struct {
	ID        string `json:"id"`
	ProductID string `json:"product_id"`
}

type paddleItem struct {
	Price paddlePrice `json:"price"`
}

type paddleCustomer struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type paddleSubscriptionData struct {
	Status     string         `json:"status"`
	CustomerID string         `json:"customer_id"`
	Customer   paddleCustomer `json:"customer"`
	Items      []paddleItem   `json:"items"`
}

type paddleTransactionData struct {
	CustomerID string         `json:"customer_id"`
	Customer   paddleCustomer `json:"customer"`
}

func subscriptionCustomerID(data paddleSubscriptionData) string {
	if data.CustomerID != "" {
		return data.CustomerID
	}
	return data.Customer.ID
}

func subscriptionEmail(data paddleSubscriptionData) string {
	return data.Customer.Email
}

func transactionCustomerID(data paddleTransactionData) string {
	if data.CustomerID != "" {
		return data.CustomerID
	}
	return data.Customer.ID
}

func transactionEmail(data paddleTransactionData) string {
	return data.Customer.Email
}
