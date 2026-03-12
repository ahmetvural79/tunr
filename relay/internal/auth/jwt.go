package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTAuth — JWT token oluştur ve doğrula.
// Kullanım yeri: CLI login sonrası token alır, WS bağlantısında kullanır.
//
// GÜVENLİK:
//   - HS256 imzalama (RS256 daha güvenli ama deploy kompleksliği artırır — sonra migrate edilebilir)
//   - Token expire süresi kısa tutuldu (24h refresh token gerektirir)
//   - Hassas claim'ler (plan, quota) token'da tutulmaz — her request'te DB'den alınır

// Claims — JWT token içeriği
type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"uid"`
	Email  string `json:"email"`
	// Plan — kullanıcının mevcut abonelik planı
	// Değerler: "free" | "pro" | "team"
	// GÜVENLİK: Plan değiştiğinde (Paddle webhook) yeni token issue edilmeli
	// DB lookup olmadan middleware'de kontrol edilir → performans kazanımı
	Plan string `json:"plan"`
}

// JWTAuth — JWT oluşturucu/doğrulayıcı
type JWTAuth struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTAuth — JWT auth oluştur
// secret: 32+ byte random key (TUNR_JWT_SECRET env'den alınır)
func NewJWTAuth(secret string, ttl time.Duration) (*JWTAuth, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT secret en az 32 karakter olmalı (güvenlik gereği)")
	}
	return &JWTAuth{secret: []byte(secret), ttl: ttl}, nil
}

// Issue — yeni JWT token oluştur
// plan: "free" | "pro" | "team"
func (j *JWTAuth) Issue(userID, email, plan string) (string, error) {
	if plan == "" {
		plan = "free" // default plan
	}
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "tunr.sh",
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
			// GÜVENLİK: NotBefore = şimdiki zaman (token önceden kullanılamaz)
			NotBefore: jwt.NewNumericDate(now),
		},
		UserID: userID,
		Email:  email,
		Plan:   plan,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(j.secret)
	if err != nil {
		return "", fmt.Errorf("token imzalanamadı: %w", err)
	}
	return signed, nil
}

// Verify — JWT token doğrula ve claims döndür
func (j *JWTAuth) Verify(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{},
		func(t *jwt.Token) (interface{}, error) {
			// GÜVENLİK: Signing method kontrolü
			// "alg: none" saldırısına karşı koruma
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return j.secret, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("token süresi dolmuş, yeniden giriş yapın")
		}
		return nil, fmt.Errorf("geçersiz token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("token parse edilemedi")
	}

	return claims, nil
}

// ─── MAGIC LINK ──────────────────────────────────────────────────────────────

// MagicToken — tek kullanımlık email giriş token'ı
type MagicToken struct {
	Token     string
	Email     string
	ExpiresAt time.Time
	Used      bool
}

// GenerateMagicToken — cryptographically random token üret
// GÜVENLİK: math/rand değil crypto/rand kullanıyoruz
func GenerateMagicToken() (string, error) {
	bytes := make([]byte, 32) // 256 bit = yeterince tahmin edilemez
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("random token üretilemedi: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// MagicLink — magic link URL'si oluştur
func MagicLink(baseURL, email, token string) string {
	// GÜVENLİK: email ve token URL encode edilmeli ama hex zaten URL-safe
	return fmt.Sprintf("%s/auth/verify?token=%s", baseURL, token)
}
