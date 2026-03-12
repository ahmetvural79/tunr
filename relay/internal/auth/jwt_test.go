package auth_test

import (
	"testing"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/auth"
)

// TestJWTIssueAndVerify — normal happy path
func TestJWTIssueAndVerify(t *testing.T) {
	j, err := auth.NewJWTAuth("supersecret-32-characters-minimum!!", 1*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTAuth hata: %v", err)
	}

	token, err := j.Issue("user-123", "test@tunr.sh", "free")
	if err != nil {
		t.Fatalf("Issue hata: %v", err)
	}
	if token == "" {
		t.Fatal("token boş")
	}

	claims, err := j.Verify(token)
	if err != nil {
		t.Fatalf("Verify hata: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("UserID = %q, user-123 beklendi", claims.UserID)
	}
	if claims.Email != "test@tunr.sh" {
		t.Errorf("Email = %q, test@tunr.sh beklendi", claims.Email)
	}
}

// TestJWTShortSecret — kısa secret reddetmeli
func TestJWTShortSecret(t *testing.T) {
	_, err := auth.NewJWTAuth("tooshort", 1*time.Hour)
	if err == nil {
		t.Error("Kısa secret için error beklendi, nil geldi")
	}
}

// TestJWTExpired — süresi dolmuş token reddedilmeli
func TestJWTExpired(t *testing.T) {
	j, _ := auth.NewJWTAuth("supersecret-32-characters-minimum!!", -1*time.Second)

	token, err := j.Issue("user-456", "expired@tunr.sh", "free")
	if err != nil {
		// Bazı JWT kütüphaneleri negatif TTL'de issue anında hata verebilir
		t.Skipf("negatif TTL Issue hatası (expected): %v", err)
	}

	// Biraz bekle
	time.Sleep(10 * time.Millisecond)

	_, err = j.Verify(token)
	if err == nil {
		t.Error("Süresi geçmiş token kabul edildi, reject beklendi")
	}
}

// TestJWTInvalidSignature — başka secret ile imzalanmış token reddedilmeli
func TestJWTInvalidSignature(t *testing.T) {
	j1, _ := auth.NewJWTAuth("supersecret-32-characters-minimum!!", 1*time.Hour)
	j2, _ := auth.NewJWTAuth("different-secret-32-characters-x!!", 1*time.Hour)

	token, _ := j1.Issue("user-789", "user@tunr.sh", "free")

	_, err := j2.Verify(token)
	if err == nil {
		t.Error("Yanlış secret ile token kabul edildi, reject beklendi")
	}
}

// TestJWTTamperedToken — değiştirilmiş token reddedilmeli
func TestJWTTamperedToken(t *testing.T) {
	j, _ := auth.NewJWTAuth("supersecret-32-characters-minimum!!", 1*time.Hour)

	_, err := j.Verify("eyJhbGciOiJIUzI1NiJ9.eyJ1aWQiOiJoYWNrZXIifQ.tampered_signature")
	if err == nil {
		t.Error("Manipüle edilmiş token kabul edildi")
	}
}

// TestJWTAlgNone — "alg: none" saldırısı engellenmeli
// Bu kritik bir güvenlik testi — JWT kütüphaneleri bu saldırıya karşı
// savunmasız olabilir
func TestJWTAlgNone(t *testing.T) {
	j, _ := auth.NewJWTAuth("supersecret-32-characters-minimum!!", 1*time.Hour)

	// alg:none ile manuel token oluştur
	// Header: {"alg":"none","typ":"JWT"}
	// Payload: {"uid":"admin","email":"admin@tunr.sh","exp":9999999999}
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1aWQiOiJhZG1pbiIsImVtYWlsIjoiYWRtaW5AcHJldm8uZGV2IiwiZXhwIjo5OTk5OTk5OTk5fQ."

	_, err := j.Verify(noneToken)
	if err == nil {
		t.Error("GÜVENLIK AÇIĞI: alg:none token kabul edildi!")
	}
}

// TestGenerateMagicToken — her çağrıda farklı, 64 char token üretilmeli
func TestGenerateMagicToken(t *testing.T) {
	token1, err := auth.GenerateMagicToken()
	if err != nil {
		t.Fatalf("GenerateMagicToken hata: %v", err)
	}
	token2, err := auth.GenerateMagicToken()
	if err != nil {
		t.Fatalf("GenerateMagicToken hata: %v", err)
	}

	if len(token1) != 64 {
		t.Errorf("token1 uzunluğu = %d, 64 beklendi (32 byte hex)", len(token1))
	}
	if token1 == token2 {
		t.Error("İki magic token aynı! Entropi sorunu var")
	}
}

// TestMagicLink — magic link URL formatı doğru mu?
func TestMagicLink(t *testing.T) {
	link := auth.MagicLink("https://tunr.sh", "user@test.com", "abc123token")
	expected := "https://tunr.sh/auth/verify?token=abc123token"
	if link != expected {
		t.Errorf("MagicLink = %q, %q beklendi", link, expected)
	}
}
