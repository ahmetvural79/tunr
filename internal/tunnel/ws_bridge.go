package tunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/proxy"
	"github.com/gorilla/websocket"
)

type wsStreamHub struct {
	mu    sync.Mutex
	conns map[string]*websocket.Conn
}

func newWSStreamHub() *wsStreamHub {
	return &wsStreamHub{conns: make(map[string]*websocket.Conn)}
}

func (h *wsStreamHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, c := range h.conns {
		_ = c.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "tunnel stopped"),
			time.Now().Add(2*time.Second))
		_ = c.Close()
		delete(h.conns, id)
	}
}

func (h *wsStreamHub) set(streamID string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[streamID] = c
}

func (h *wsStreamHub) shutdownStream(streamID string, code int, reason string) {
	h.mu.Lock()
	c := h.conns[streamID]
	delete(h.conns, streamID)
	h.mu.Unlock()
	if c == nil {
		return
	}
	_ = c.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(code, truncateCloseReason(reason)),
		time.Now().Add(3*time.Second))
	_ = c.Close()
}

func (h *wsStreamHub) writeFrame(streamID string, messageType int, payload []byte) error {
	h.mu.Lock()
	c := h.conns[streamID]
	h.mu.Unlock()
	if c == nil {
		return fmt.Errorf("no upstream ws for stream %s", streamID)
	}
	return c.WriteMessage(messageType, payload)
}

func truncateCloseReason(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

type wsOpenPayload struct {
	StreamID  string              `json:"stream_id"`
	Path      string              `json:"path"`
	Headers   map[string]string   `json:"headers,omitempty"`
	HeadersV2 map[string][]string `json:"headers_v2,omitempty"`
}

type wsFramePayload struct {
	StreamID   string `json:"stream_id"`
	Opcode     int    `json:"opcode"`
	PayloadB64 string `json:"payload_b64"`
}

type wsClosePayload struct {
	StreamID string `json:"stream_id"`
	Code     int    `json:"code"`
	Reason   string `json:"reason"`
}

func pathForRouteMatch(requestURI string) string {
	p := strings.TrimSpace(requestURI)
	if i := strings.Index(p, "?"); i >= 0 {
		p = p[:i]
	}
	if i := strings.Index(p, "#"); i >= 0 {
		p = p[:i]
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func isSecWebSocketProtocolHeader(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Protocol")
}

func extractSecWebSocketProtocol(open *wsOpenPayload) []string {
	var raw string
	if len(open.HeadersV2) > 0 {
		for k, vals := range open.HeadersV2 {
			if !isSecWebSocketProtocolHeader(k) || len(vals) == 0 {
				continue
			}
			raw = strings.Join(vals, ",")
			break
		}
	}
	if raw == "" && open.Headers != nil {
		for k, v := range open.Headers {
			if isSecWebSocketProtocolHeader(k) {
				raw = v
				break
			}
		}
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func skipWSForwardHeader(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "connection", "upgrade", "content-length", "transfer-encoding",
		"sec-websocket-key", "sec-websocket-accept", "sec-websocket-protocol":
		return true
	default:
		return false
	}
}

func buildUpstreamWSHeaders(open *wsOpenPayload, targetPort int) http.Header {
	h := http.Header{}
	if len(open.HeadersV2) > 0 {
		for k, vals := range open.HeadersV2 {
			if skipWSForwardHeader(k) {
				continue
			}
			ck := http.CanonicalHeaderKey(k)
			for _, v := range vals {
				h.Add(ck, v)
			}
		}
	} else {
		for k, v := range open.Headers {
			if skipWSForwardHeader(k) {
				continue
			}
			ck := http.CanonicalHeaderKey(k)
			h.Set(ck, v)
		}
	}
	h.Set("Host", fmt.Sprintf("localhost:%d", targetPort))
	return h
}

func sendWsCloseToRelay(rc *RelayConn, streamID string, code int, reason string) {
	if streamID == "" {
		return
	}
	if code == 0 {
		code = websocket.CloseNormalClosure
	}
	payload, err := json.Marshal(wsClosePayload{
		StreamID: streamID,
		Code:     code,
		Reason:   reason,
	})
	if err != nil {
		return
	}
	_ = rc.writeJSON(wsMessage{Type: MsgTypeWsClose, Data: payload})
}

func runCLIWebSocketBridge(ctx context.Context, rc *RelayConn, hub *wsStreamHub, lp *proxy.LocalProxy, raw json.RawMessage) {
	var open wsOpenPayload
	if err := json.Unmarshal(raw, &open); err != nil {
		logger.Warn("ws_open invalid payload: %v", err)
		return
	}
	if open.StreamID == "" || open.Path == "" {
		return
	}

	targetPort := lp.ResolvePortForPath(pathForRouteMatch(open.Path))
	reqPath := open.Path
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d%s", targetPort, reqPath)

	hdr := buildUpstreamWSHeaders(&open, targetPort)
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		Subprotocols:     extractSecWebSocketProtocol(&open),
	}

	localConn, resp, err := dialer.Dial(wsURL, hdr)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		logger.Warn("local WS dial failed %s: %v", wsURL, err)
		sendWsCloseToRelay(rc, open.StreamID, websocket.CloseTLSHandshake, "upstream dial failed")
		return
	}

	hub.set(open.StreamID, localConn)
	readDone := make(chan struct{})

	go func() {
		defer close(readDone)
		for {
			mt, payload, rerr := localConn.ReadMessage()
			if rerr != nil {
				sendWsCloseToRelay(rc, open.StreamID, websocket.CloseNormalClosure, "upstream read ended")
				return
			}
			fr, jerr := json.Marshal(wsFramePayload{
				StreamID:   open.StreamID,
				Opcode:     mt,
				PayloadB64: base64.StdEncoding.EncodeToString(payload),
			})
			if jerr != nil {
				continue
			}
			if werr := rc.writeJSON(wsMessage{Type: MsgTypeWsFrame, Data: fr}); werr != nil {
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-readDone:
	}

	hub.shutdownStream(open.StreamID, websocket.CloseNormalClosure, "bridge end")
}
