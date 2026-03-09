package proxy

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BasicAuthMiddleware - Tüneli şifreyle korumak için Basic Authentication katmanı
func BasicAuthMiddleware(expectedCreds string, next http.Handler) http.Handler {
	// expectedCreds formatı "username:password" veya sadece "password" olabilir.
	var expectedUsername, expectedPassword string

	if strings.Contains(expectedCreds, ":") {
		parts := strings.SplitN(expectedCreds, ":", 2)
		expectedUsername = parts[0]
		expectedPassword = parts[1]
	} else {
		// Sadece şifre girildiyse kullanıcı adını boş kabul et (veya admin)
		expectedUsername = "admin"
		expectedPassword = expectedCreds
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Tarayıcıdan veya client'tan gelen Basic Auth logini al
		user, pass, ok := r.BasicAuth()

		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(expectedUsername)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPassword)) != 1 {
			// Yanlış login - Authorization header'ına challenge gönder
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted Tunnel"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Şifre doğru, sonraki katmana (veya asıl backend'e) ilet
		next.ServeHTTP(w, r)
	})
}
