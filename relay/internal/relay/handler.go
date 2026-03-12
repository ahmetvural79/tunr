package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ahmetvural79/tunr/relay/internal/auth"
	relaydb "github.com/ahmetvural79/tunr/relay/internal/db"
	"github.com/ahmetvural79/tunr/relay/internal/logger"
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
	Body      string            `json:"body"` // base64 encode edilmemiş (UTF-8 safe)
}

// ResponseData — CLI'ın relay'e gönderdiği HTTP yanıtı
type ResponseData struct {
	RequestID  string            `json:"request_id"`
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
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
	if hello.Token != "" {
		claims, err := h.jwtAuth.Verify(hello.Token)
		if err != nil {
			writeErr(conn, "geçersiz token")
			return
		}
		userID = claims.UserID
	} else {
		userID = "anon:" + r.RemoteAddr
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

	// 2. Relay → CLI: HTTP isteklerini ilet
	go func() {
		for {
			select {
			case <-entry.Done:
				errCh <- nil
				return
			case req := <-entry.Requests:
				reqData, _ := json.Marshal(RequestData{
					RequestID: req.ID,
					Method:    req.Method,
					Path:      req.Path,
					Headers:   flattenHeaders(req.Headers),
					Body:      string(req.Body),
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
		for k, v := range resp.Headers {
			respHeaders.Set(k, v)
		}

		entry.ResolveResponse(resp.RequestID, &TunnelResponse{
			StatusCode: resp.StatusCode,
			Headers:    respHeaders,
			Body:       []byte(resp.Body),
		})

	case MsgTypePong:
		h.registry.UpdatePing(entry.ID)

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
