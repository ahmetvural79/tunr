package inspector

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/google/uuid"
)

// HTTP Inspector — full-body capture for every request and response.
// Think ngrok's inspect tab, but ours. Seeing what hits your server before prod does: priceless.

// CapturedRequest is a complete snapshot of an HTTP round-trip
type CapturedRequest struct {
	ID        string    `json:"id"`
	TunnelID  string    `json:"tunnel_id"`
	Timestamp time.Time `json:"timestamp"`

	Method     string            `json:"method"`
	URL        string            `json:"url"`
	Path       string            `json:"path"`
	RemoteAddr string            `json:"remote_addr"`
	ReqHeaders map[string]string `json:"req_headers"`
	ReqBody    string            `json:"req_body"` // max 64KB
	ReqBodyLen int64             `json:"req_body_len"`

	StatusCode  int               `json:"status_code"`
	RespHeaders map[string]string `json:"resp_headers"`
	RespBody    string            `json:"resp_body"` // max 64KB
	RespBodyLen int64             `json:"resp_body_len"`

	DurationMs int64 `json:"duration_ms"`

	ContentType string `json:"content_type"`
	IsJSON      bool   `json:"is_json"`
}

// Inspector is the request/response capture middleware backed by a ring buffer
type Inspector struct {
	mu       sync.RWMutex
	requests []*CapturedRequest
	maxSize  int

	totalCount atomic.Int64 // lifetime request counter

	// fires when a new request is captured — used to push to the web UI
	OnNewRequest func(req *CapturedRequest)
}

// New creates an inspector with the given ring buffer capacity
func New(ringSize int) *Inspector {
	if ringSize <= 0 {
		ringSize = 1000
	}
	return &Inspector{
		requests: make([]*CapturedRequest, 0, ringSize),
		maxSize:  ringSize,
	}
}

// Middleware wraps a handler to capture requests and responses into the ring buffer
func (ins *Inspector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := uuid.New().String()[:8]

		// ── REQUEST CAPTURE ──

		var reqBodyBuf bytes.Buffer
		var reqBodyLen int64
		if r.Body != nil {
			// capture up to 64KB — bigger bodies still get forwarded, just not recorded
			limitedBody := io.LimitReader(r.Body, 64*1024)
			reqBodyLen, _ = io.Copy(&reqBodyBuf, limitedBody)
			if r.Header.Get("Content-Encoding") == "gzip" {
				reqBodyBuf = decodeGzip(reqBodyBuf)
			}
			// reassemble the body so upstream can still read it
			r.Body = io.NopCloser(io.MultiReader(
				bytes.NewReader(reqBodyBuf.Bytes()),
				r.Body, // remainder beyond 64KB, if any
			))
		}

		// SECURITY: mask sensitive headers before we store anything
		reqHeaders := sanitizeHeaders(r.Header)

		// ── RESPONSE CAPTURE ──
		rw := newResponseWriter(w)

		next.ServeHTTP(rw, r)

		// Timing
		duration := time.Since(start)

		// Response body decode
		respBody := rw.bodyBuf.Bytes()
		if rw.Header().Get("Content-Encoding") == "gzip" {
			buf := decodeGzip(*bytes.NewBuffer(respBody))
			respBody = buf.Bytes()
		}

		// only keep response bodies up to 64KB
		respBodyStr := ""
		if len(respBody) <= 64*1024 {
			respBodyStr = string(respBody)
		} else {
			respBodyStr = fmt.Sprintf("[%d bytes — too large, only first 64KB stored]", len(respBody))
		}

		ct := rw.Header().Get("Content-Type")
		isJSON := strings.Contains(ct, "application/json")

		captured := &CapturedRequest{
			ID:          id,
			Timestamp:   start,
			Method:      r.Method,
			URL:         r.URL.String(),
			Path:        r.URL.Path,
			RemoteAddr:  r.RemoteAddr,
			ReqHeaders:  reqHeaders,
			ReqBody:     reqBodyBuf.String(),
			ReqBodyLen:  reqBodyLen,
			StatusCode:  rw.statusCode,
			RespHeaders: sanitizeHeaders(rw.Header()),
			RespBody:    respBodyStr,
			RespBodyLen: int64(len(respBody)),
			DurationMs:  duration.Milliseconds(),
			ContentType: ct,
			IsJSON:      isJSON,
		}

		ins.totalCount.Add(1)
		ins.add(captured)

		// WebUI callback
		if ins.OnNewRequest != nil {
			go ins.OnNewRequest(captured)
		}
	})
}

// add appends to the ring buffer, evicting the oldest entry if full
func (ins *Inspector) add(req *CapturedRequest) {
	ins.mu.Lock()
	defer ins.mu.Unlock()

	if len(ins.requests) >= ins.maxSize {
		ins.requests = ins.requests[1:]
	}
	ins.requests = append(ins.requests, req)
}

// GetAll returns all captured requests (newest first)
func (ins *Inspector) GetAll() []*CapturedRequest {
	ins.mu.RLock()
	defer ins.mu.RUnlock()

	result := make([]*CapturedRequest, len(ins.requests))
	copy(result, ins.requests)
	return result
}

// GetByID looks up a single captured request
func (ins *Inspector) GetByID(id string) (*CapturedRequest, error) {
	ins.mu.RLock()
	defer ins.mu.RUnlock()

	for _, req := range ins.requests {
		if req.ID == id {
			return req, nil
		}
	}
	return nil, fmt.Errorf("request not found: %s", id)
}

// Clear wipes the ring buffer clean
func (ins *Inspector) Clear() {
	ins.mu.Lock()
	ins.requests = ins.requests[:0]
	ins.mu.Unlock()
}

// Stats returns a snapshot of inspector metrics
func (ins *Inspector) Stats() map[string]interface{} {
	ins.mu.RLock()
	count := len(ins.requests)
	ins.mu.RUnlock()

	return map[string]interface{}{
		"total_captured": ins.totalCount.Load(),
		"in_buffer":      count,
		"buffer_size":    ins.maxSize,
	}
}

// ─── REQUEST REPLAY ────────────────────────────────────────────────────────

// Replay re-sends a captured request to the local port — great for debugging
func (ins *Inspector) Replay(ctx context.Context, id string, localPort int) (*ReplayResult, error) {
	captured, err := ins.GetByID(id)
	if err != nil {
		return nil, err
	}

	// SECURITY: replay only targets localhost — no risk of leaking to the outside
	localURL := fmt.Sprintf("http://localhost:%d%s", localPort, captured.Path)
	if captured.URL != "" && strings.Contains(captured.URL, "?") {
		localURL += "?" + strings.SplitN(captured.URL, "?", 2)[1]
	}

	var bodyReader io.Reader
	if captured.ReqBody != "" {
		bodyReader = strings.NewReader(captured.ReqBody)
	}

	req, err := http.NewRequestWithContext(ctx, captured.Method, localURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to build replay request: %w", err)
	}

	// re-attach headers (sensitive ones were already redacted)
	for key, val := range captured.ReqHeaders {
		req.Header.Set(key, val)
	}

	start := time.Now()
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("replay failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	return &ReplayResult{
		OriginalID: id,
		StatusCode: resp.StatusCode,
		DurationMs: time.Since(start).Milliseconds(),
		Body:       string(body),
	}, nil
}

// ReplayResult holds what came back from a replayed request
type ReplayResult struct {
	OriginalID string `json:"original_id"`
	StatusCode int    `json:"status_code"`
	DurationMs int64  `json:"duration_ms"`
	Body       string `json:"body"`
}

// ExportCurl converts a captured request into a copy-pasteable curl command
func (ins *Inspector) ExportCurl(id string) (string, error) {
	captured, err := ins.GetByID(id)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("curl -X %s", captured.Method))
	sb.WriteString(fmt.Sprintf(" \\\n  '%s'", captured.URL))

	for key, val := range captured.ReqHeaders {
		// SECURITY: redact auth headers so users don't accidentally share tokens
		if strings.EqualFold(key, "authorization") || strings.EqualFold(key, "cookie") {
			sb.WriteString(fmt.Sprintf(" \\\n  -H '%s: [REDACTED]'", key))
			continue
		}
		sb.WriteString(fmt.Sprintf(" \\\n  -H '%s: %s'", key, val))
	}

	if captured.ReqBody != "" {
		body := strings.ReplaceAll(captured.ReqBody, "'", `'\''`) // escape single quotes
		sb.WriteString(fmt.Sprintf(" \\\n  -d '%s'", body))
	}

	return sb.String(), nil
}

// ─── HELPERS ────────────────────────────────────────────────────────────────

// sanitizeHeaders redacts sensitive headers.
// SECURITY: this is critical — auth tokens must never end up in logs or captures.
func sanitizeHeaders(headers http.Header) map[string]string {
	sensitiveHeaders := map[string]bool{
		"authorization":       true,
		"cookie":              true,
		"set-cookie":          true,
		"x-api-key":           true,
		"x-auth-token":        true,
		"x-access-token":      true,
		"x-secret":            true,
		"x-password":          true,
		"proxy-authorization": true,
	}

	result := make(map[string]string, len(headers))
	for key, vals := range headers {
		lower := strings.ToLower(key)
		if sensitiveHeaders[lower] {
			result[key] = "[REDACTED]"
		} else {
			result[key] = strings.Join(vals, ", ")
		}
	}
	return result
}

// decodeGzip decompresses gzip-encoded data, falling back to raw on failure
func decodeGzip(buf bytes.Buffer) bytes.Buffer {
	reader, err := gzip.NewReader(&buf)
	if err != nil {
		return buf // can't decode, return as-is
	}
	defer reader.Close()

	var decoded bytes.Buffer
	_, _ = io.Copy(&decoded, io.LimitReader(reader, 64*1024))
	return decoded
}

// responseWriter wraps http.ResponseWriter to capture status code and body
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	bodyBuf    bytes.Buffer
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.bodyBuf.Write(b)
	return rw.ResponseWriter.Write(b)
}

// PrettyJSON formats a JSON string with indentation for the dashboard
func PrettyJSON(raw string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw // not valid JSON, return as-is
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return string(pretty)
}

var _ = logger.Info // keep the import alive for linter
