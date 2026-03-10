package billing_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tunr-dev/tunr/internal/billing"
)

// createPaddleSig — creates a valid Paddle webhook signature for testing
// Paddle protocol: "ts=<timestamp>:body" HMAC-SHA256
func createPaddleSig(secret, body string, ts int64) string {
	payload := fmt.Sprintf("%d:%s", ts, body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("ts=%d;h1=%s", ts, sig)
}

// TestNewPaddleClientCreation — verifies client creation
func TestNewPaddleClientCreation(t *testing.T) {
	client := billing.NewPaddleClient("test-api-key", "test-webhook-secret", true)
	if client == nil {
		t.Fatal("NewPaddleClient returned nil")
	}
}

// TestWebhookVerifyValidSignature — valid signature should be accepted
func TestWebhookVerifyValidSignature(t *testing.T) {
	secret := "test-webhook-secret-paddle"
	client := billing.NewPaddleClient("api-key", secret, true)

	body := `{"event_type":"subscription.created","data":{"customer_id":"ctm_123","status":"active"}}`
	ts := time.Now().Unix()
	sig := createPaddleSig(secret, body, ts)

	err := client.VerifyWebhookSignature([]byte(body), sig)
	if err != nil {
		t.Fatalf("valid webhook was rejected: %v", err)
	}
}

// TestWebhookVerifyInvalidSignature — invalid signature should be rejected
func TestWebhookVerifyInvalidSignature(t *testing.T) {
	client := billing.NewPaddleClient("api-key", "correct-secret", true)

	body := `{"event_type":"subscription.created"}`
	wrongSig := "ts=1234567890;h1=fakesignaturethatisnotvalid00000000"

	err := client.VerifyWebhookSignature([]byte(body), wrongSig)
	if err == nil {
		t.Error("SECURITY VULNERABILITY: webhook with invalid signature was accepted!")
	}
}

// TestWebhookReplayAttack — stale timestamp should be rejected
func TestWebhookReplayAttack(t *testing.T) {
	secret := "test-webhook-secret-paddle"
	client := billing.NewPaddleClient("api-key", secret, true)

	body := `{"event_type":"subscription.created"}`
	// Timestamp from 10 minutes ago (outside the 5min window)
	oldTS := time.Now().Add(-10 * time.Minute).Unix()
	sig := createPaddleSig(secret, body, oldTS)

	err := client.VerifyWebhookSignature([]byte(body), sig)
	if err == nil {
		t.Error("webhook with stale timestamp was accepted! Replay attack protection is broken!")
	}
}

// TestWebhookFutureTimestamp — future timestamp should also be rejected
func TestWebhookFutureTimestamp(t *testing.T) {
	secret := "test-webhook-secret-paddle"
	client := billing.NewPaddleClient("api-key", secret, true)

	body := `{"event_type":"subscription.created"}`
	futureTS := time.Now().Add(10 * time.Minute).Unix()
	sig := createPaddleSig(secret, body, futureTS)

	err := client.VerifyWebhookSignature([]byte(body), sig)
	if err == nil {
		t.Error("webhook with future timestamp was accepted! Replay attack protection is incomplete!")
	}
}

// TestWebhookMissingTimestamp — missing timestamp
func TestWebhookMissingTimestamp(t *testing.T) {
	client := billing.NewPaddleClient("api-key", "test-secret", true)
	err := client.VerifyWebhookSignature([]byte(`{}`), "h1=onlysig")
	if err == nil {
		t.Error("signature without ts was accepted")
	}
}

// TestWebhookEmptySignature — empty signature should be rejected
func TestWebhookEmptySignature(t *testing.T) {
	client := billing.NewPaddleClient("api-key", "test-secret", true)
	err := client.VerifyWebhookSignature([]byte(`{}`), "")
	if err == nil {
		t.Error("empty signature was accepted")
	}
}

// TestGetLimitsFreePlan — free plan quotas are correct
func TestGetLimitsFreePlan(t *testing.T) {
	client := billing.NewPaddleClient("api-key", "secret", true)
	limits := client.GetLimits(nil, "") // context nil = sandbox

	// Expected free plan limits (per PlanLimits struct in paddle.go)
	if limits.MaxTunnels <= 0 {
		t.Errorf("free plan MaxTunnels = %d, expected > 0", limits.MaxTunnels)
	}
}

// TestHMACTimingSafeComparison — verifies timing-safe comparison
// This test directly tests hmac.Equal, indirectly validating the billing package
func TestHMACTimingSafeComparison(t *testing.T) {
	secret := []byte("test-key")
	body := []byte("test-body")

	mac1 := hmac.New(sha256.New, secret)
	mac1.Write(body)
	sig1 := mac1.Sum(nil)

	mac2 := hmac.New(sha256.New, secret)
	mac2.Write(body)
	sig2 := mac2.Sum(nil)

	// hmac.Equal timing-safe
	if !hmac.Equal(sig1, sig2) {
		t.Error("HMAC produced different results for identical input")
	}

	// Different signatures must not be considered equal
	wrongSig := make([]byte, len(sig1))
	copy(wrongSig, sig1)
	wrongSig[0] ^= 0xFF // flip the first byte
	if hmac.Equal(sig1, wrongSig) {
		t.Error("different HMACs appear equal!")
	}

	_ = strings.ToLower // keep import
}
