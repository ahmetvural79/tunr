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

	"github.com/google/uuid"
	"github.com/tunr-dev/tunr/internal/logger"
)

// HTTP Inspector — gelen ve giden isteklerin tam kopyasını tutar.
// ngrok'ta "inspector" ekranı gördünüz mü? İşte o.
// Prod sunucuya çarpmadan önce ne geldiğini görmek: priceless.

// CapturedRequest — yakalanmış bir HTTP isteğinin tam kaydı
type CapturedRequest struct {
	// Kimlik ve zaman
	ID        string    `json:"id"`
	TunnelID  string    `json:"tunnel_id"`
	Timestamp time.Time `json:"timestamp"`

	// İstek bilgileri
	Method     string            `json:"method"`
	URL        string            `json:"url"`
	Path       string            `json:"path"`
	RemoteAddr string            `json:"remote_addr"`
	ReqHeaders map[string]string `json:"req_headers"`
	ReqBody    string            `json:"req_body"`   // max 64KB
	ReqBodyLen int64             `json:"req_body_len"`
	
	// Yanıt bilgileri
	StatusCode  int               `json:"status_code"`
	RespHeaders map[string]string `json:"resp_headers"`
	RespBody    string            `json:"resp_body"`   // max 64KB
	RespBodyLen int64             `json:"resp_body_len"`
	
	// Performans
	DurationMs int64  `json:"duration_ms"`
	
	// İçerik tipi — dashboard için
	ContentType string `json:"content_type"`
	IsJSON      bool   `json:"is_json"`
}

// Inspector — istek/yanıt middleware + ring buffer
type Inspector struct {
	mu       sync.RWMutex
	requests []*CapturedRequest
	maxSize  int

	// Abone sayacı (kaç WS client canlı log alıyor)
	subscribers atomic.Int32

	// Kanıt: bu middleware'den kaç istek geçti
	totalCount atomic.Int64

	// Log callback — yeni istek gelince webui'ye bildir
	OnNewRequest func(req *CapturedRequest)
}

// New — inspector oluştur
func New(ringSize int) *Inspector {
	if ringSize <= 0 {
		ringSize = 1000 // default: son 1000 isteği sakla
	}
	return &Inspector{
		requests: make([]*CapturedRequest, 0, ringSize),
		maxSize:  ringSize,
	}
}

// Middleware — bu fonksiyonu proxy handler'a sar
// İsteği ve yanıtı yakalar, ring buffer'a yazar
func (ins *Inspector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := uuid.New().String()[:8]

		// ── İSTEK YAKALAMA ──
		
		// Request body'yi oku (ama upstream'e de iletmeye devam et)
		var reqBodyBuf bytes.Buffer
		var reqBodyLen int64
		if r.Body != nil {
			// Max 64KB body oku — daha büyük body'leri upstream'e ileteriz ama kaydetmeyiz
			limitedBody := io.LimitReader(r.Body, 64*1024)
			reqBodyLen, _ = io.Copy(&reqBodyBuf, limitedBody)
			// Gzip decode?
			if r.Header.Get("Content-Encoding") == "gzip" {
				reqBodyBuf = decodeGzip(reqBodyBuf)
			}
			// Body'yi geri koy (upstream handler okuyabilsin)
			r.Body = io.NopCloser(io.MultiReader(
				bytes.NewReader(reqBodyBuf.Bytes()),
				r.Body, // varsa kalan kısım (64KB üstü)
			))
		}

		// GÜVENLİK: Request header'larını maskele
		// Authorization, Cookie, X-API-Key gibi hassas header'ları loglamıyoruz
		reqHeaders := sanitizeHeaders(r.Header)

		// ── YANIT YAKALAMA ──
		rw := newResponseWriter(w)

		// Downstream handler'a ilet
		next.ServeHTTP(rw, r)

		// Timing
		duration := time.Since(start)

		// Response body decode
		respBody := rw.bodyBuf.Bytes()
		if rw.Header().Get("Content-Encoding") == "gzip" {
			buf := decodeGzip(*bytes.NewBuffer(respBody))
			respBody = buf.Bytes()
		}

		// Max 64KB response body sakla
		respBodyStr := ""
		if len(respBody) <= 64*1024 {
			respBodyStr = string(respBody)
		} else {
			respBodyStr = fmt.Sprintf("[%d bytes — çok büyük, sadece ilk 64KB]", len(respBody))
		}

		// İçerik tipi belirle
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

// add — ring buffer'a yeni kayıt ekle
func (ins *Inspector) add(req *CapturedRequest) {
	ins.mu.Lock()
	defer ins.mu.Unlock()

	// Ring: eski en baştan sil
	if len(ins.requests) >= ins.maxSize {
		ins.requests = ins.requests[1:]
	}
	ins.requests = append(ins.requests, req)
}

// GetAll — tüm kayıtları getir (en yeniden eski)
func (ins *Inspector) GetAll() []*CapturedRequest {
	ins.mu.RLock()
	defer ins.mu.RUnlock()

	result := make([]*CapturedRequest, len(ins.requests))
	copy(result, ins.requests)
	return result
}

// GetByID — ID ile belirli bir kaydı getir
func (ins *Inspector) GetByID(id string) (*CapturedRequest, error) {
	ins.mu.RLock()
	defer ins.mu.RUnlock()

	for _, req := range ins.requests {
		if req.ID == id {
			return req, nil
		}
	}
	return nil, fmt.Errorf("istek bulunamadı: %s", id)
}

// Clear — ring buffer'ı temizle
func (ins *Inspector) Clear() {
	ins.mu.Lock()
	ins.requests = ins.requests[:0]
	ins.mu.Unlock()
}

// Stats — inspector istatistikleri
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

// Replay — kaydedilen bir isteği tekrar local port'a gönder
func (ins *Inspector) Replay(ctx context.Context, id string, localPort int) (*ReplayResult, error) {
	captured, err := ins.GetByID(id)
	if err != nil {
		return nil, err
	}

	// GÜVENLİK: replay sadece localhost'a gönderilir
	// Dışarıya istek gönderme riski yok
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
		return nil, fmt.Errorf("replay isteği oluşturulamadı: %w", err)
	}

	// Header'ları geri koy (hassas olanlar zaten sanitize edilmişti)
	for key, val := range captured.ReqHeaders {
		req.Header.Set(key, val)
	}

	start := time.Now()
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("replay gönderilirken hata: %w", err)
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

// ReplayResult — replay sonucu
type ReplayResult struct {
	OriginalID string `json:"original_id"`
	StatusCode int    `json:"status_code"`
	DurationMs int64  `json:"duration_ms"`
	Body       string `json:"body"`
}

// ExportCurl — kaydedilen isteği curl komutu olarak export et
func (ins *Inspector) ExportCurl(id string) (string, error) {
	captured, err := ins.GetByID(id)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("curl -X %s", captured.Method))
	sb.WriteString(fmt.Sprintf(" \\\n  '%s'", captured.URL))

	for key, val := range captured.ReqHeaders {
		// GÜVENLİK: Authorization header'ını curl export'una dahil etme
		// Kullanıcı token'ını yanlışlıkla paylaşmasın
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

// sanitizeHeaders — hassas header'ları maskele
// GÜVENLİK: Bu cidden önemli. Auth token'ları log'a geçmesin.
func sanitizeHeaders(headers http.Header) map[string]string {
	// Bu header'lar tamamen gizlenir
	sensitiveHeaders := map[string]bool{
		"authorization": true,
		"cookie":        true,
		"set-cookie":    true,
		"x-api-key":     true,
		"x-auth-token":  true,
		"x-access-token": true,
		"x-secret":      true,
		"x-password":    true,
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

// decodeGzip — gzip sıkıştırılmış veriyi aç
func decodeGzip(buf bytes.Buffer) bytes.Buffer {
	reader, err := gzip.NewReader(&buf)
	if err != nil {
		return buf // decode edilemedi, orijinali döndür
	}
	defer reader.Close()

	var decoded bytes.Buffer
	_, _ = io.Copy(&decoded, io.LimitReader(reader, 64*1024))
	return decoded
}

// responseWriter — http.ResponseWriter wrapper, body ve status code'u yakalar
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
	rw.bodyBuf.Write(b) // kopyasını tut
	return rw.ResponseWriter.Write(b) // ileriye ilet
}

// PrettyJSON — JSON body'sini güzel formatta döndür (dashboard için)
func PrettyJSON(raw string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw // JSON değil, olduğu gibi döndür
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return string(pretty)
}

// Warn - bağlantı log'u (inspector kullanımını bildir)
var _ = logger.Info // logger'ı referans et, lint hata vermesin
