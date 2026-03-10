// Package tunr is the official Go SDK for tunr.
//
// Use it to programmatically create tunnels from CI/CD pipelines,
// test automation, or custom tooling.
//
// Usage:
//
//	client, err := tunr.NewClient()
//	tunnel, err := client.Share(ctx, 3000, tunr.Options{})
//	fmt.Println(tunnel.PublicURL) // https://abc123.tunr.sh
//	defer tunnel.Close()
package tunr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"
)

// Client talks to the tunr API. Works via both REST and CLI under the hood.
type Client struct {
	token      string
	apiBase    string
	httpClient *http.Client
}

// Option configures the client
type Option func(*Client)

// WithToken sets the auth token
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithAPIBase sets a custom relay address (for self-hosted deployments)
func WithAPIBase(base string) Option {
	return func(c *Client) { c.apiBase = base }
}

// NewClient creates a tunr client
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		apiBase: "https://relay.tunr.sh",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Tunnel represents an active tunnel
type Tunnel struct {
	ID        string
	PublicURL string
	LocalPort int
	StartedAt time.Time

	done chan struct{}
	cmd  *exec.Cmd
}

// TunnelOptions configures tunnel creation
type TunnelOptions struct {
	Subdomain string
	NoInspect bool
}

// Share exposes a local port as a public HTTPS URL.
//
//	tunnel, err := client.Share(ctx, 3000, TunnelOptions{})
//	// tunnel.PublicURL = "https://abc123.tunr.sh"
//	defer tunnel.Close()
func (c *Client) Share(ctx context.Context, port int, opts TunnelOptions) (*Tunnel, error) {
	if port < 1024 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %d", port)
	}

	args := []string{"share", "--port", fmt.Sprintf("%d", port), "--no-open"}
	if opts.Subdomain != "" {
		args = append(args, "--subdomain", opts.Subdomain)
	}

	cmd := exec.CommandContext(ctx, "tunr", args...)

	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start tunr: %w", err)
	}

	readOutput := func(r io.Reader) {
		buf := make([]byte, 1024)
		full := ""
		for {
			n, err := r.Read(buf)
			if n > 0 {
				full += string(buf[:n])
				for _, line := range splitLines(full) {
					if url := extractURL(line); url != "" {
						select {
						case urlCh <- url:
						default:
						}
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}

	go readOutput(stdout)
	go readOutput(stderr)

	var publicURL string
	select {
	case publicURL = <-urlCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		return nil, fmt.Errorf("tunnel failed to start within 15 seconds")
	case <-ctx.Done():
		cmd.Process.Kill()
		return nil, ctx.Err()
	}

	tunnel := &Tunnel{
		PublicURL: publicURL,
		LocalPort: port,
		StartedAt: time.Now(),
		done:      make(chan struct{}),
		cmd:       cmd,
	}

	go func() {
		cmd.Wait()
		close(tunnel.done)
	}()

	return tunnel, nil
}

// Close shuts down the tunnel
func (t *Tunnel) Close() error {
	if t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Kill()
	}
	return nil
}

// Done returns a channel that closes when the tunnel exits
func (t *Tunnel) Done() <-chan struct{} {
	return t.done
}

// ─── Inspector API ────────────────────────────────────────────────────────────

// Request is a captured HTTP request from the inspector
type Request struct {
	ID         string            `json:"id"`
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	StatusCode int               `json:"status_code"`
	DurationMs int64             `json:"duration_ms"`
	Timestamp  time.Time         `json:"timestamp"`
	ReqBody    string            `json:"req_body"`
	RespBody   string            `json:"resp_body"`
	ReqHeaders map[string]string `json:"req_headers"`
}

// Requests fetches recent HTTP requests from the inspector
func (c *Client) Requests(ctx context.Context, limit int) ([]*Request, error) {
	dashURL := "http://localhost:19842/api/v1/requests"
	if limit > 0 {
		dashURL += fmt.Sprintf("?limit=%d", limit)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, dashURL, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach inspector (is the daemon running?): %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Requests []*Request `json:"requests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Requests, nil
}

// Replay resends a captured request to the local server
func (c *Client) Replay(ctx context.Context, requestID string, port int) error {
	url := fmt.Sprintf("http://localhost:19842/api/v1/requests/%s?action=replay&port=%d",
		requestID, port)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("replay failed (status %d)", resp.StatusCode)
	}
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func extractURL(line string) string {
	for _, word := range splitWords(line) {
		if len(word) > 8 &&
			(startsWith(word, "https://") || startsWith(word, "http://")) &&
			contains(word, "tunr.sh") {
			return word
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	return lines
}

func splitWords(s string) []string {
	var words []string
	start := -1
	for i, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, s[start:])
	}
	return words
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
