// Package tunr — tunr Go SDK
//
// tunr CLI'ı programatik olarak kullanın.
// CI/CD, test automation, custom tooling için ideal.
//
// Kullanım:
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

// Client — tunr API client
// Hem REST API hem CLI üzerinden çalışabilir.
type Client struct {
	token      string
	apiBase    string
	httpClient *http.Client
}

// Option — client konfigürasyon fonksiyonu
type Option func(*Client)

// WithToken — auth token
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithAPIBase — özel relay adresi (self-hosted için)
func WithAPIBase(base string) Option {
	return func(c *Client) { c.apiBase = base }
}

// NewClient — tunr client oluştur
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

// Tunnel — aktif bir tunnel
type Tunnel struct {
	ID        string
	PublicURL string
	LocalPort int
	StartedAt time.Time

	done chan struct{}
	cmd  *exec.Cmd
}

// TunnelOptions — tunnel açma seçenekleri
type TunnelOptions struct {
	Subdomain string
	NoInspect bool
}

// Share — local port'u public URL olarak paylaş
//
//	tunnel, err := client.Share(ctx, 3000, TunnelOptions{})
//	// tunnel.PublicURL = "https://abc123.tunr.sh"
//	defer tunnel.Close()
func (c *Client) Share(ctx context.Context, port int, opts TunnelOptions) (*Tunnel, error) {
	if port < 1024 || port > 65535 {
		return nil, fmt.Errorf("geçersiz port: %d", port)
	}

	// tunr binary'yi çalıştır
	args := []string{"share", "--port", fmt.Sprintf("%d", port), "--no-open"}
	if opts.Subdomain != "" {
		args = append(args, "--subdomain", opts.Subdomain)
	}

	cmd := exec.CommandContext(ctx, "tunr", args...)

	// URL'yi stdout/stderr'den oku
	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tunr başlatılamadı: %w", err)
	}

	// URL'yi oku
	readOutput := func(r io.Reader) {
		buf := make([]byte, 1024)
		full := ""
		for {
			n, err := r.Read(buf)
			if n > 0 {
				full += string(buf[:n])
				// URL bulmaya çalış
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

	// URL gelene kadar bekle (max 15s)
	var publicURL string
	select {
	case publicURL = <-urlCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		return nil, fmt.Errorf("tunnel 15 saniyede başlamadı")
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

	// Tunnel kapanınca done kanal'ı kapat
	go func() {
		cmd.Wait()
		close(tunnel.done)
	}()

	return tunnel, nil
}

// Close — tunnel'ı kapat
func (t *Tunnel) Close() error {
	if t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Kill()
	}
	return nil
}

// Done — tunnel kapandığında bu kanal kapanır
func (t *Tunnel) Done() <-chan struct{} {
	return t.done
}

// ─── Inspector API ────────────────────────────────────────────────────────────

// Request — yakalanmış HTTP isteği
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

// Requests — son HTTP isteklerini getir
func (c *Client) Requests(ctx context.Context, limit int) ([]*Request, error) {
	dashURL := "http://localhost:19842/api/v1/requests"
	if limit > 0 {
		dashURL += fmt.Sprintf("?limit=%d", limit)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, dashURL, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inspector'a bağlanılamadı (daemon çalışıyor mu?): %w", err)
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

// Replay — yakalanmış bir isteği tekrar gönder
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
		return fmt.Errorf("replay başarısız (status %d)", resp.StatusCode)
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
