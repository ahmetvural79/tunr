package proxy

import (
	"crypto/subtle"
	"net/http"
	"net/textproto"

	"github.com/ahmetvural79/tunr/internal/logger"
)

// BearerTokenMiddleware validates requests carrying a bearer token in the
// Authorization header or a configurable header name.
func BearerTokenMiddleware(token, headerName string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		var requestToken string

		if headerName != "" {
			requestToken = r.Header.Get(headerName)
			if requestToken == "" {
				requestToken = r.URL.Query().Get("token")
			}
		} else {
			auth := r.Header.Get("Authorization")
			if len(auth) > 7 && auth[:7] == "Bearer " {
				requestToken = auth[7:]
			}
			if requestToken == "" {
				requestToken = r.URL.Query().Get("token")
			}
		}

		if requestToken == "" || subtle.ConstantTimeCompare([]byte(requestToken), []byte(token)) != 1 {
			logger.Debug("Denied request: invalid/missing bearer token from %s", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized","message":"valid bearer token required"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HeaderModification holds a single rule for live header modification.
type HeaderModification struct {
	Action string // "add", "replace", "remove"
	Header string // header name
	Value  string // new value (for add/replace)
}

// HeaderModificationMiddleware applies live header modifications before
// forwarding requests to the upstream server.
func HeaderModificationMiddleware(rules []HeaderModification, next http.Handler) http.Handler {
	if len(rules) == 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rule := range rules {
			switch rule.Action {
			case "add":
				r.Header.Add(rule.Header, rule.Value)
			case "replace":
				r.Header.Set(rule.Header, rule.Value)
			case "remove":
				r.Header.Del(rule.Header)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// XForwardedForMiddleware adds X-Forwarded-For with the original client IP.
func XForwardedForMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") == "" {
			r.Header.Set("X-Forwarded-For", r.RemoteAddr)
		}
		next.ServeHTTP(w, r)
	})
}

// OriginalURLMiddleware adds X-Original-URL with the public request URL.
func OriginalURLMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Original-URL") == "" {
			scheme := "https"
			if r.TLS == nil {
				scheme = "http"
			}
			originalURL := scheme + "://" + r.Host + r.URL.RequestURI()
			r.Header.Set("X-Original-URL", originalURL)
		}
		next.ServeHTTP(w, r)
	})
}

// CORSPreflightMiddleware handles CORS preflight requests if enabled.
func CORSPreflightMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			origin := r.Header.Get("Origin")
			for _, allowed := range allowedOrigins {
				if origin == allowed || allowed == "*" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "*")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// force canonical header keys for Go's textproto
func init() {
	_ = textproto.CanonicalMIMEHeaderKey("x-forwarded-for")
}
