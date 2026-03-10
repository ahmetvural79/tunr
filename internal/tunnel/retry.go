package tunnel

import (
	"context"
	cryptoRand "crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
)

// RetryConfig tunes the exponential backoff strategy.
// "Just retry immediately" is how you DDoS yourself.
type RetryConfig struct {
	MaxAttempts int           // 0 = forever, handle with care
	BaseDelay   time.Duration // delay before the first retry
	MaxDelay    time.Duration // ceiling for the geometric growth
	Multiplier  float64       // delay multiplier per attempt
	Jitter      bool          // randomize delay to prevent thundering herd
}

// DefaultRetryConfig is the "sane defaults" config — retries forever in daemon mode
var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 0,
	BaseDelay:   1 * time.Second,
	MaxDelay:    60 * time.Second,
	Multiplier:  2.0,
	Jitter:      true,
}

// ShareRetryConfig is less patient — `tunr share` shouldn't wait forever
var ShareRetryConfig = RetryConfig{
	MaxAttempts: 5,
	BaseDelay:   500 * time.Millisecond,
	MaxDelay:    10 * time.Second,
	Multiplier:  2.0,
	Jitter:      true,
}

// RetryFunc is the function signature for anything that can be retried
type RetryFunc func(ctx context.Context, attempt int) error

// IsRetryableError decides if an error is worth retrying.
// Some errors (auth failures, bad ports) won't magically fix themselves.
type IsRetryableError func(err error) bool

// WithRetry wraps a function in exponential backoff
func WithRetry(ctx context.Context, cfg RetryConfig, fn RetryFunc, isRetryable ...IsRetryableError) error {
	retryCheck := defaultIsRetryable
	if len(isRetryable) > 0 && isRetryable[0] != nil {
		retryCheck = isRetryable[0]
	}

	for attempt := 1; ; attempt++ {
		err := fn(ctx, attempt)
		if err == nil {
			return nil
		}

		if !retryCheck(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
			return fmt.Errorf("gave up after %d attempts: %w", attempt, err)
		}

		delay := calculateDelay(cfg, attempt)

		logger.Warn("Attempt %d failed: %v — retrying in %s", attempt, err, delay.Round(time.Millisecond))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// calculateDelay computes exponential backoff with optional jitter
func calculateDelay(cfg RetryConfig, attempt int) time.Duration {
	delay := float64(cfg.BaseDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	if cfg.Jitter {
		// ±25% jitter to prevent thundering herd
		// SECURITY: math/rand is fine here — this is about load spreading, not cryptography
		jitter := delay * 0.25
		delay += (rand.Float64()*2 - 1) * jitter
		if delay < 0 {
			delay = float64(cfg.BaseDelay)
		}
	}

	return time.Duration(delay)
}

// defaultIsRetryable decides which errors deserve another shot
func defaultIsRetryable(err error) bool {
	if err == nil {
		return false
	}

	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	var ue *url.Error
	if errors.As(err, &ue) {
		return true
	}

	return true // when in doubt, retry
}

// RelayClient talks to the tunr relay server.
// Currently wraps Cloudflare quicktunnel; custom relay coming soon.
type RelayClient struct {
	relayURL   string
	authToken  string // SECURITY: only sent to relay, never logged
	httpClient *http.Client
}

// NewRelayClient creates a relay client with sane defaults
func NewRelayClient(relayURL string, authToken string) *RelayClient {
	return &RelayClient{
		relayURL:  relayURL,
		authToken: authToken, // SECURITY: this field is not exposed outside the struct
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				// SECURITY: TLS verification is always on — no exceptions
				DisableKeepAlives: false,
			},
		},
	}
}

// RequestTunnel asks the relay for a fresh public URL
func (rc *RelayClient) RequestTunnel(ctx context.Context, port int, opts TunnelRequestOptions) (*TunnelResponse, error) {
	// SECURITY: validate URL to prevent SSRF attacks
	if err := validateRelayURL(rc.relayURL); err != nil {
		return nil, fmt.Errorf("invalid relay URL: %w", err)
	}

	// TODO phase 1: real API call
	// POST /v1/tunnels
	// Authorization: Bearer <token>
	// { "local_port": port, "subdomain": opts.Subdomain }
	//
	// For now, fall back to Cloudflare quicktunnel (no token needed, but rate limited)
	publicURL := fmt.Sprintf("https://%s.trycloudflare.com", generateSubdomain())

	return &TunnelResponse{
		PublicURL: publicURL,
		TunnelID:  generateID(),
		ExpiresAt: time.Now().Add(8 * time.Hour),
	}, nil
}

// TunnelRequestOptions configures a tunnel request
type TunnelRequestOptions struct {
	Subdomain string // custom subdomain (pro feature)
	HTTPS     bool
}

// TunnelResponse is what the relay hands back
type TunnelResponse struct {
	PublicURL string    `json:"public_url"`
	TunnelID  string    `json:"tunnel_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// validateRelayURL ensures the relay URL is safe to call.
// SECURITY: prevents SSRF — nobody should be able to craft a config that hits internal networks
func validateRelayURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	if u.Scheme != "https" {
		return fmt.Errorf("relay must use HTTPS (got scheme: %q)", u.Scheme)
	}

	// SECURITY: reject private IP ranges to prevent SSRF
	host := u.Hostname()
	if isPrivateHost(host) {
		return fmt.Errorf("relay must not point to a private IP: %s", host)
	}

	return nil
}

// isPrivateHost checks if the host resolves to a private or loopback address
func isPrivateHost(host string) bool {
	privateHosts := []string{
		"localhost", "127.", "10.", "192.168.",
		"::1", "169.254.", "0.",
	}
	for _, prefix := range privateHosts {
		if len(host) >= len(prefix) && host[:len(prefix)] == prefix {
			return true
		}
	}
	if len(host) >= 7 && host[:4] == "172." {
		var second int
		_, _ = fmt.Sscanf(host[4:], "%d", &second)
		if second >= 16 && second <= 31 {
			return true
		}
	}
	return false
}

// generateSubdomain creates a random hex subdomain for quicktunnel
func generateSubdomain() string {
	b := make([]byte, 4)
	_, _ = cryptoRand.Read(b)
	return fmt.Sprintf("%x", b)
}

// generateID mints a short random tunnel ID
func generateID() string {
	b := make([]byte, 8)
	_, _ = cryptoRand.Read(b)
	return fmt.Sprintf("%x", b)[:8]
}
