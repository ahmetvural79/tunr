package relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ahmetvural79/tunr/relay/internal/auth"
	relaydb "github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
	"github.com/gorilla/websocket"
)

// Handler — relay WebSocket handler.
//
// Bağlantı akışı:
//  1. CLI: tunr share --port 3000
//  2. CLI relay sunucusuna WS bağlantısı açar: wss://relay.tunr.sh/tunnel/connect
//  3. Server tunnel kaydeder, subdomain atar
//  4. Dış dünya https://{subdomain}.tunr.sh/{path} çağırır
//  5. Server bu isteği CLI'ya iletir (WS üzerinden JSON)
//  6. CLI local'e forward eder, cevabı WS üzerinden geri gönderir
//  7. Server cevabı HTTP yanıtı olarak dış dünyaya döner

// Mesaj tipleri — CLI ile relay arasındaki protokol
type MsgType string

const (
	MsgTypeHello    MsgType = "hello"    // CLI → relay: bağlantı kur
	MsgTypeWelcome  MsgType = "welcome"  // relay → CLI: tunnel hazır
	MsgTypeRequest  MsgType = "request"  // relay → CLI: incoming HTTP isteği
	MsgTypeResponse MsgType = "response" // CLI → relay: HTTP isteği yanıtı
	MsgTypePing     MsgType = "ping"     // relay → CLI: heartbeat
	MsgTypePong     MsgType = "pong"     // CLI → relay: heartbeat yanıtı
	MsgTypeError    MsgType = "error"    // iki yönlü: hata bildirimi
	MsgTypeClose    MsgType = "close"    // tunnel kapatma isteği
	MsgTypeWsOpen   MsgType = "ws_open"  // relay → CLI: browser WS tunnel start
	MsgTypeWsFrame  MsgType = "ws_frame" // bidirectional WS payload
	MsgTypeWsClose  MsgType = "ws_close" // bidirectional WS shutdown
)

// Message — WS üzerinden gönderilen JSON mesajı
type Message struct {
	Type MsgType         `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// HelloData — CLI'ın ilk mesajı
type HelloData struct {
	Token     string `json:"token"`      // auth JWT
	LocalPort int    `json:"local_port"` // bilgi amaçlı
	Subdomain string `json:"subdomain"`  // tercih edilen subdomain (opsiyonel)
	Version   string `json:"version"`    // tunr CLI versiyonu
}

// WelcomeData — relay'in ilk yanıtı
type WelcomeData struct {
	TunnelID  string `json:"tunnel_id"`
	Subdomain string `json:"subdomain"`
	PublicURL string `json:"public_url"`
}

// RequestData — relay'in CLI'ya ilettiği HTTP isteği
type RequestData struct {
	RequestID string            `json:"request_id"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	HeadersV2 map[string][]string `json:"headers_v2,omitempty"`
	Body      string              `json:"body,omitempty"`
	BodyB64   string              `json:"body_b64,omitempty"`
}

// ResponseData — CLI'ın relay'e gönderdiği HTTP yanıtı
type ResponseData struct {
	RequestID  string            `json:"request_id"`
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	HeadersV2  map[string][]string `json:"headers_v2,omitempty"`
	Body       string              `json:"body,omitempty"`
	BodyB64    string              `json:"body_b64,omitempty"`
}

// WsOpenData — browser WebSocket'inin yerel proxylenmesi için meta
type WsOpenData struct {
	StreamID  string              `json:"stream_id"`
	Path      string              `json:"path"`
	Headers   map[string]string   `json:"headers,omitempty"`
	HeadersV2 map[string][]string `json:"headers_v2,omitempty"`
}

// WsFrameData — tek WS frame (opcode + base64 payload)
type WsFrameData struct {
	StreamID   string `json:"stream_id"`
	Opcode     int    `json:"opcode"`
	PayloadB64 string `json:"payload_b64"`
}

// WsCloseData — WS stream kapatma
type WsCloseData struct {
	StreamID string `json:"stream_id"`
	Code     int    `json:"code"`
	Reason   string `json:"reason"`
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 64,
	WriteBufferSize: 1024 * 64,
	CheckOrigin: func(r *http.Request) bool {
		// GÜVENLİK: Origin kontrolü — sadece tunr CLI ve tunr.sh kabul et
		// Empty origin = CLI tool (non-browser) → kabul et
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // CLI WebSocket client
		}
		// Browser bağlantısı gerekiyorsa tunr.sh domain'i kontrol et
		return strings.HasSuffix(origin, "tunr.sh")
	},
}

// Handler — WebSocket bağlantılarını relay eden ana handler
type Handler struct {
	registry *Registry
	jwtAuth  *auth.JWTAuth
	db       *relaydb.DB
	domain   string // tunr.sh
}

// NewHandler — relay handler oluştur
func NewHandler(registry *Registry, jwtAuth *auth.JWTAuth, db *relaydb.DB, domain string) *Handler {
	return &Handler{
		registry: registry,
		jwtAuth:  jwtAuth,
		db:       db,
		domain:   domain,
	}
}

// ServeTunnel — WebSocket upgrade + tunnel lifecycle yönetimi
// Route: GET /tunnel/connect
func (h *Handler) ServeTunnel(w http.ResponseWriter, r *http.Request) {
	// HTTP → WebSocket upgrade
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("WS upgrade başarısız: %v", err)
		return
	}
	defer conn.Close()

	// Large HTML responses travel as JSON + base64 on this control socket.
	conn.SetReadLimit(64 << 20) // 64 MiB

	// Bağlantı kuralım ama önce hello mesajını bekle
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	var helloMsg Message
	if err := conn.ReadJSON(&helloMsg); err != nil || helloMsg.Type != MsgTypeHello {
		logger.Warn("Geçersiz hello mesajı: %v", err)
		return
	}

	var hello HelloData
	if err := json.Unmarshal(helloMsg.Data, &hello); err != nil {
		writeErr(conn, "geçersiz hello payload")
		return
	}

	// GÜVENLİK: Auth token doğrula
	// Anonymous kullanım destekleniyor ama rate limit uygulanır
	var userID string
	var userPlan string
	var isAuthenticated bool
	if hello.Token != "" {
		claims, err := h.jwtAuth.Verify(hello.Token)
		if err != nil {
			writeErr(conn, "geçersiz token")
			return
		}
		userID = claims.UserID
		userPlan = claims.Plan
		isAuthenticated = true
	} else {
		userID = "anon:" + r.RemoteAddr
	}

	// Reserved subdomain ownership check.
	if hello.Subdomain != "" {
		if !isAuthenticated {
			writeErr(conn, "Custom subdomain requires login")
			return
		}
		if userPlan != "pro" && userPlan != "team" {
			writeErr(conn, "Custom subdomain requires Pro subscription")
			return
		}
		if h.db != nil {
			ownerUserID, found, err := h.db.GetReservedSubdomainOwner(r.Context(), hello.Subdomain)
			if err != nil {
				logger.Warn("reserved subdomain check failed (%s): %v", hello.Subdomain, err)
				writeErr(conn, "subdomain ownership check failed")
				return
			}
			if found && ownerUserID != userID {
				writeErr(conn, "subdomain is reserved by another user")
				return
			}
		}
	}

	// Tunnel kayıt et
	entry, err := h.registry.Register(userID, hello.Subdomain)
	if err != nil {
		writeErr(conn, err.Error())
		return
	}
	defer h.registry.Unregister(entry.ID)

	// Log kaydı (DB'ye de yazılır)
	logger.Info("Tunnel bağlandı: %s → %s (kullanıcı: %s)",
		entry.ID, entry.PublicURL(h.domain), userID)

	if h.db != nil {
		_ = h.db.RecordTunnelConnect(r.Context(), entry.ID, userID, entry.Subdomain)
	}

	entry.LocalPort = hello.LocalPort

	// Welcome mesajı gönder
	welcomeData, _ := json.Marshal(WelcomeData{
		TunnelID:  entry.ID,
		Subdomain: entry.Subdomain,
		PublicURL: entry.PublicURL(h.domain),
	})
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(Message{Type: MsgTypeWelcome, Data: welcomeData}); err != nil {
		return
	}

	// Read deadline'ı sıfırla — artık ping/request döngüsüne gireceğiz
	_ = conn.SetReadDeadline(time.Time{})

	// Ping döngüsü başlat (30 saniyede bir)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go h.pingLoop(ctx, conn, entry.ID)

	// Mesaj döngüsü — iki goroutine paralel çalışır:
	// 1. Gelen WS mesajlarını (response, pong) işle
	// 2. Gelen HTTP isteklerini (registry.Requests) CLI'ya ilet
	errCh := make(chan error, 2)

	// 1. CLI → relay mesajları
	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				errCh <- err
				return
			}
			if err := h.handleClientMessage(entry, &msg); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// 2. Relay → CLI: HTTP isteklerini ve WS kontrol mesajlarını ilet
	go func() {
		for {
			select {
			case <-entry.Done:
				errCh <- nil
				return
			case msg := <-entry.Outbound:
				_ = conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
				if err := conn.WriteJSON(msg); err != nil {
					errCh <- err
					return
				}
			case req := <-entry.Requests:
				reqData, _ := json.Marshal(RequestData{
					RequestID: req.ID,
					Method:    req.Method,
					Path:      req.Path,
					Headers:   flattenHeaders(req.Headers),
					HeadersV2: cloneHeaders(req.Headers),
					Body:      encodeBodyUTF8(req.Body),
					BodyB64:   base64.StdEncoding.EncodeToString(req.Body),
				})
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteJSON(Message{Type: MsgTypeRequest, Data: reqData}); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	// Bekle: hata veya bağlantı kopması
	<-errCh
	logger.Info("Tunnel kapandı: %s", entry.ID)

	if h.db != nil {
		_ = h.db.RecordTunnelDisconnect(context.Background(), entry.ID)
	}
}

// handleClientMessage — CLI'dan gelen mesajı işle
func (h *Handler) handleClientMessage(entry *TunnelEntry, msg *Message) error {
	switch msg.Type {
	case MsgTypeResponse:
		var resp ResponseData
		if err := json.Unmarshal(msg.Data, &resp); err != nil {
			return nil
		}

		respHeaders := make(http.Header)
		if len(resp.HeadersV2) > 0 {
			for k, vals := range resp.HeadersV2 {
				for _, v := range vals {
					respHeaders.Add(k, v)
				}
			}
		} else {
			for k, v := range resp.Headers {
				respHeaders.Set(k, v)
			}
		}

		body, err := decodeWireBody(resp.BodyB64, resp.Body)
		if err != nil {
			logger.Warn("invalid response body encoding request_id=%s: %v", resp.RequestID, err)
			body = []byte(resp.Body)
		}

		entry.ResolveResponse(resp.RequestID, &TunnelResponse{
			StatusCode: resp.StatusCode,
			Headers:    respHeaders,
			Body:       body,
		})

	case MsgTypePong:
		h.registry.UpdatePing(entry.ID)

	case MsgTypeWsFrame:
		var fr WsFrameData
		if err := json.Unmarshal(msg.Data, &fr); err != nil {
			return nil
		}
		payload, err := base64.StdEncoding.DecodeString(fr.PayloadB64)
		if err != nil {
			logger.Warn("ws_frame bad base64 stream=%s: %v", fr.StreamID, err)
			return nil
		}
		if err := entry.WriteWSFrameToBrowser(fr.StreamID, fr.Opcode, payload); err != nil {
			logger.Debug("ws_frame to browser skipped stream=%s: %v", fr.StreamID, err)
		}

	case MsgTypeWsClose:
		var cl WsCloseData
		if err := json.Unmarshal(msg.Data, &cl); err != nil {
			return nil
		}
		code := cl.Code
		if code == 0 {
			code = websocket.CloseNormalClosure
		}
		entry.CloseBrowserWS(cl.StreamID, code, cl.Reason)

	case MsgTypeClose:
		return io.EOF // bağlantıyı kapat
	}
	return nil
}

// pingLoop — 30 saniyede bir ping gönder
func (h *Handler) pingLoop(ctx context.Context, conn *websocket.Conn, tunnelID string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(Message{Type: MsgTypePing}); err != nil {
				return
			}
		}
	}
}

// writeErr — WS üzerinden hata mesajı gönder
func writeErr(conn *websocket.Conn, msg string) {
	data, _ := json.Marshal(map[string]string{"message": msg})
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_ = conn.WriteJSON(Message{Type: MsgTypeError, Data: data})
}

// flattenHeaders — http.Header → map[string]string dönüşümü
func flattenHeaders(h http.Header) map[string]string {
	result := make(map[string]string, len(h))
	for k, v := range h {
		result[k] = strings.Join(v, ", ")
	}
	return result
}

func cloneHeaders(h http.Header) map[string][]string {
	result := make(map[string][]string, len(h))
	for k, vals := range h {
		cloned := make([]string, len(vals))
		copy(cloned, vals)
		result[k] = cloned
	}
	return result
}

func encodeBodyUTF8(body []byte) string {
	if utf8.Valid(body) {
		return string(body)
	}
	return ""
}

func decodeWireBody(bodyB64, body string) ([]byte, error) {
	if bodyB64 != "" {
		return base64.StdEncoding.DecodeString(bodyB64)
	}
	return []byte(body), nil
}
