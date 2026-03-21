package relay

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ahmetvural79/tunr/relay/internal/logger"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func isBrowserWebSocket(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func browserTunnelCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	return h == "tunr.sh" || strings.HasSuffix(h, ".tunr.sh")
}

// serveBrowserWebSocket upgrades the public HTTPS connection and bridges frames
// to the CLI over the existing control WebSocket (Outbound channel).
func (p *Proxy) serveBrowserWebSocket(w http.ResponseWriter, r *http.Request, entry *TunnelEntry) {
	up := websocket.Upgrader{
		CheckOrigin:     browserTunnelCheckOrigin,
		ReadBufferSize:  64 * 1024,
		WriteBufferSize: 64 * 1024,
	}

	browserConn, err := up.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("browser WS upgrade failed: %v", err)
		return
	}

	streamID := uuid.New().String()
	entry.StoreBrowserWS(streamID, browserConn)

	defer func() {
		entry.RemoveBrowserWS(streamID)
		_ = browserConn.Close()
	}()

	openData, _ := json.Marshal(WsOpenData{
		StreamID:  streamID,
		Path:      r.URL.RequestURI(),
		Headers:   flattenHeaders(r.Header),
		HeadersV2: cloneHeaders(r.Header),
	})

	select {
	case <-entry.Done:
		return
	case entry.Outbound <- Message{Type: MsgTypeWsOpen, Data: openData}:
	case <-time.After(10 * time.Second):
		logger.Warn("ws_open outbound timeout stream=%s", streamID)
		return
	}

	for {
		mt, payload, err := browserConn.ReadMessage()
		if err != nil {
			sendWsCloseToCLI(entry, streamID, websocket.CloseNormalClosure, "browser closed")
			return
		}
		fr, jerr := json.Marshal(WsFrameData{
			StreamID:   streamID,
			Opcode:     mt,
			PayloadB64: base64.StdEncoding.EncodeToString(payload),
		})
		if jerr != nil {
			continue
		}
		select {
		case <-entry.Done:
			return
		case entry.Outbound <- Message{Type: MsgTypeWsFrame, Data: fr}:
		case <-time.After(60 * time.Second):
			sendWsCloseToCLI(entry, streamID, websocket.CloseGoingAway, "relay outbound slow")
			return
		}
	}
}

func sendWsCloseToCLI(entry *TunnelEntry, streamID string, code int, reason string) {
	data, err := json.Marshal(WsCloseData{
		StreamID: streamID,
		Code:     code,
		Reason:   reason,
	})
	if err != nil {
		return
	}
	select {
	case entry.Outbound <- Message{Type: MsgTypeWsClose, Data: data}:
	default:
	}
}
