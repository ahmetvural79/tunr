package proxy

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BasicAuthMiddleware protects the tunnel with HTTP Basic Authentication.
func BasicAuthMiddleware(expectedCreds string, next http.Handler) http.Handler {
	// Accepts "username:password" or just "password" (defaults to admin user)
	var expectedUsername, expectedPassword string

	if strings.Contains(expectedCreds, ":") {
		parts := strings.SplitN(expectedCreds, ":", 2)
		expectedUsername = parts[0]
		expectedPassword = parts[1]
	} else {
		expectedUsername = "admin"
		expectedPassword = expectedCreds
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()

		// SECURITY: Constant-time comparison to prevent timing attacks
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(expectedUsername)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted Tunnel"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
