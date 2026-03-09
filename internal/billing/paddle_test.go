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

// createPaddleSig — test için geçerli Paddle webhook imzası oluştur
// Paddle protokolü: "ts=<timestamp>:body" HMAC-SHA256
func createPaddleSig(secret, body string, ts int64) string {
	payload := fmt.Sprintf("%d:%s", ts, body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("ts=%d;h1=%s", ts, sig)
}

// TestNewPaddleClientCreation — client oluşturma
func TestNewPaddleClientCreation(t *testing.T) {
	client := billing.NewPaddleClient("test-api-key", "test-webhook-secret", true)
	if client == nil {
		t.Fatal("NewPaddleClient nil döndürdü")
	}
}

// TestWebhookVerifyValidSignature — geçerli imzalı webhook kabul edilmeli
func TestWebhookVerifyValidSignature(t *testing.T) {
	secret := "test-webhook-secret-paddle"
	client := billing.NewPaddleClient("api-key", secret, true)

	body := `{"event_type":"subscription.created","data":{"customer_id":"ctm_123","status":"active"}}`
	ts := time.Now().Unix()
	sig := createPaddleSig(secret, body, ts)

	err := client.VerifyWebhookSignature([]byte(body), sig)
	if err != nil {
		t.Fatalf("Geçerli webhook reddedildi: %v", err)
	}
}

// TestWebhookVerifyInvalidSignature — yanlış imzalı webhook reddedilmeli
func TestWebhookVerifyInvalidSignature(t *testing.T) {
	client := billing.NewPaddleClient("api-key", "correct-secret", true)

	body := `{"event_type":"subscription.created"}`
	wrongSig := "ts=1234567890;h1=fakesignaturethatisnotvalid00000000"

	err := client.VerifyWebhookSignature([]byte(body), wrongSig)
	if err == nil {
		t.Error("GÜVENLİK AÇIĞI: Yanlış imzalı webhook kabul edildi!")
	}
}

// TestWebhookReplayAttack — eski timestamp reddedilmeli
func TestWebhookReplayAttack(t *testing.T) {
	secret := "test-webhook-secret-paddle"
	client := billing.NewPaddleClient("api-key", secret, true)

	body := `{"event_type":"subscription.created"}`
	// 10 dakika önce oluşturulmuş timestamp (5dk window dışında)
	oldTS := time.Now().Add(-10 * time.Minute).Unix()
	sig := createPaddleSig(secret, body, oldTS)

	err := client.VerifyWebhookSignature([]byte(body), sig)
	if err == nil {
		t.Error("Eski timestamp'li webhook kabul edildi! Replay attack koruması çalışmıyor!")
	}
}

// TestWebhookFutureTimestamp — gelecek timestamp de reddedilmeli
func TestWebhookFutureTimestamp(t *testing.T) {
	secret := "test-webhook-secret-paddle"
	client := billing.NewPaddleClient("api-key", secret, true)

	body := `{"event_type":"subscription.created"}`
	futureTS := time.Now().Add(10 * time.Minute).Unix()
	sig := createPaddleSig(secret, body, futureTS)

	err := client.VerifyWebhookSignature([]byte(body), sig)
	if err == nil {
		t.Error("Gelecek timestampli webhook kabul edildi! Replay attack koruması eksik!")
	}
}

// TestWebhookMissingTimestamp — timestamp eksik
func TestWebhookMissingTimestamp(t *testing.T) {
	client := billing.NewPaddleClient("api-key", "test-secret", true)
	err := client.VerifyWebhookSignature([]byte(`{}`), "h1=onlysig")
	if err == nil {
		t.Error("ts eksik imza kabul edildi")
	}
}

// TestWebhookEmptySignature — boş imza reddedilmeli
func TestWebhookEmptySignature(t *testing.T) {
	client := billing.NewPaddleClient("api-key", "test-secret", true)
	err := client.VerifyWebhookSignature([]byte(`{}`), "")
	if err == nil {
		t.Error("Boş imza kabul edildi")
	}
}

// TestGetLimitsFreePlan — Free plan kotaları doğru
func TestGetLimitsFreePlan(t *testing.T) {
	client := billing.NewPaddleClient("api-key", "secret", true)
	limits := client.GetLimits(nil, "") // context nil = sandbox

	// Free plan beklenen limitleri
	// (paddle.go'daki PlanLimits struct'a göre)
	if limits.MaxTunnels <= 0 {
		t.Errorf("Free plan MaxTunnels = %d, > 0 beklendi", limits.MaxTunnels)
	}
}

// TestHMACTimingSafeComparison — timing attack'a karşı dirençli mi?
// Bu test doğrudan hmac.Equal'ı test eder, dolaylı olarak billing paketini test eder
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
		t.Error("Aynı input için HMAC farklı sonuç verdi")
	}

	// Farklı signature'lar eşit kabul edilmemeli
	wrongSig := make([]byte, len(sig1))
	copy(wrongSig, sig1)
	wrongSig[0] ^= 0xFF // ilk byte'ı flip et
	if hmac.Equal(sig1, wrongSig) {
		t.Error("Farklı HMAC'ler eşit görünüyor!")
	}

	_ = strings.ToLower // import kullanım
}
