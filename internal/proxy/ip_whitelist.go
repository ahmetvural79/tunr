package proxy

import (
	"net"
	"net/http"
	"strings"

	"github.com/ahmetvural79/tunr/internal/logger"
)

// IPWhitelist holds parsed CIDR networks for access control.
type IPWhitelist struct {
	networks []*net.IPNet
	rawCIDRs []string
}

// NewIPWhitelist parses a comma-separated list of CIDR strings.
// Invalid CIDRs are silently skipped (logged at debug level).
func NewIPWhitelist(cidrs []string) IPWhitelist {
	whitelist := IPWhitelist{rawCIDRs: cidrs}

	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			logger.Debug("Skipping invalid CIDR %q: %v", cidr, err)
			continue
		}
		whitelist.networks = append(whitelist.networks, ipNet)
	}

	return whitelist
}

// IsEmpty returns true when no valid CIDRs were parsed.
func (w IPWhitelist) IsEmpty() bool {
	return len(w.networks) == 0
}

// Allowed checks whether the given IP falls within any whitelisted network.
func (w IPWhitelist) Allowed(ip string) bool {
	if w.IsEmpty() {
		return true
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Strip IPv6-to-IPv4 prefix if present
	if parsedIP4 := parsedIP.To4(); parsedIP4 != nil {
		parsedIP = parsedIP4
	}

	for _, network := range w.networks {
		if network.Contains(parsedIP) {
			return true
		}
	}
	return false
}

// Middleware returns an http.Handler that rejects requests from IPs
// not in the whitelist with a 403 Forbidden response.
func (w IPWhitelist) Middleware(next http.Handler) http.Handler {
	if w.IsEmpty() {
		return next
	}

	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		clientIP := extractClientIP(req)
		if !w.Allowed(clientIP) {
			logger.Debug("Blocked request from %s (not in whitelist)", clientIP)
			resp.Header().Set("Content-Type", "application/json")
			resp.WriteHeader(http.StatusForbidden)
			resp.Write([]byte(`{"error":"forbidden","message":"IP not whitelisted"}`))
			return
		}
		next.ServeHTTP(resp, req)
	})
}

// extractClientIP pulls the client IP from X-Forwarded-For, X-Real-IP,
// or falls back to RemoteAddr.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For first (leftmost = original client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr (strip port if present)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
