package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/gorilla/websocket"
)

// MsgType mirrors the relay server's protocol message types.
type MsgType string

const (
	MsgTypeHello    MsgType = "hello"
	MsgTypeWelcome  MsgType = "welcome"
	MsgTypeRequest  MsgType = "request"
	MsgTypeResponse MsgType = "response"
	MsgTypePing     MsgType = "ping"
	MsgTypePong     MsgType = "pong"
	MsgTypeError    MsgType = "error"
	MsgTypeClose    MsgType = "close"
)

type wsMessage struct {
	Type MsgType         `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type helloData struct {
	Token     string `json:"token"`
	LocalPort int    `json:"local_port"`
	Subdomain string `json:"subdomain,omitempty"`
	Version   string `json:"version"`
}

type welcomeData struct {
	TunnelID  string `json:"tunnel_id"`
	Subdomain string `json:"subdomain"`
	PublicURL string `json:"public_url"`
}

type requestData struct {
	RequestID string            `json:"request_id"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
}

type responseData struct {
	RequestID  string            `json:"request_id"`
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// RelayConn holds a live WebSocket connection to the relay server.
type RelayConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex // gorilla/websocket requires serialized writes
}

// writeJSON serializes v as JSON and sends it over the WebSocket (thread-safe).
func (rc *RelayConn) writeJSON(v interface{}) error {
	rc.writeMu.Lock()
	defer rc.writeMu.Unlock()
	_ = rc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return rc.conn.WriteJSON(v)
}

// ConnectRelay dials the relay, performs the hello/welcome handshake,
// and returns a live connection ready for the request/response loop.
func ConnectRelay(ctx context.Context, relayURL string, token string, port int, subdomain string, version string) (*RelayConn, *welcomeData, error) {
	wsURL, err := buildWSURL(relayURL)
	if err != nil {
		return nil, nil, err
	}

	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("WebSocket dial failed: %w", err)
	}

	rc := &RelayConn{conn: conn}

	helloPayload, _ := json.Marshal(helloData{
		Token:     token,
		LocalPort: port,
		Subdomain: subdomain,
		Version:   version,
	})
	if err := rc.writeJSON(wsMessage{
		Type: MsgTypeHello,
		Data: helloPayload,
	}); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to send hello: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	var welcomeMsg wsMessage
	if err := conn.ReadJSON(&welcomeMsg); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to read welcome: %w", err)
	}

	if welcomeMsg.Type == MsgTypeError {
		conn.Close()
		errMsg := string(welcomeMsg.Data)
		// Check if the error is related to plan limits or quotas
		if strings.Contains(strings.ToLower(errMsg), "limit") || strings.Contains(strings.ToLower(errMsg), "quota") || strings.Contains(strings.ToLower(errMsg), "free plan") {
			logger.Error("\n[!] Tunnel Limit Reached\n")
			logger.Error("You have reached the maximum number of active tunnels for your current plan.")
			logger.Error("Please upgrade to Tunr Pro to open more concurrent tunnels.")
			logger.Error("Upgrade here: https://tunr.sh/dashboard/settings/billing\n")
		} else {
			logger.Error("Relay rejected connection: %s", errMsg)
		}
		return nil, nil, fmt.Errorf("relay rejected connection: %s", errMsg)
	}
	if welcomeMsg.Type != MsgTypeWelcome {
		conn.Close()
		return nil, nil, fmt.Errorf("unexpected message type: %s (expected welcome)", welcomeMsg.Type)
	}

	var welcome welcomeData
	if err := json.Unmarshal(welcomeMsg.Data, &welcome); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("invalid welcome payload: %w", err)
	}

	_ = conn.SetReadDeadline(time.Time{})
	return rc, &welcome, nil
}

// RunLoop reads messages from the relay and dispatches them.
// For "request" messages it calls forwardToLocal; for "ping" it responds with "pong".
// Blocks until ctx is cancelled or the connection drops.
func (rc *RelayConn) RunLoop(ctx context.Context, forwardToLocal func(ctx context.Context, req *requestData) *responseData) error {
	errCh := make(chan error, 1)

	go func() {
		for {
			var msg wsMessage
			if err := rc.conn.ReadJSON(&msg); err != nil {
				errCh <- err
				return
			}

			switch msg.Type {
			case MsgTypePing:
				if err := rc.writeJSON(wsMessage{Type: MsgTypePong}); err != nil {
					errCh <- err
					return
				}

			case MsgTypeRequest:
				var req requestData
				if err := json.Unmarshal(msg.Data, &req); err != nil {
					logger.Warn("Invalid request payload from relay: %v", err)
					continue
				}

				go func(r requestData) {
					resp := forwardToLocal(ctx, &r)
					respPayload, _ := json.Marshal(resp)
					if err := rc.writeJSON(wsMessage{Type: MsgTypeResponse, Data: respPayload}); err != nil {
						logger.Warn("Failed to send response to relay: %v", err)
					}
				}(req)

			case MsgTypeError:
				logger.Warn("Relay error: %s", string(msg.Data))

			case MsgTypeClose:
				errCh <- io.EOF
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		_ = rc.writeJSON(wsMessage{Type: MsgTypeClose})
		rc.conn.Close()
		return ctx.Err()
	case err := <-errCh:
		rc.conn.Close()
		return err
	}
}

// buildWSURL converts "https://relay.tunr.sh" → "wss://relay.tunr.sh/tunnel/connect"
func buildWSURL(relayURL string) (string, error) {
	u, err := url.Parse(relayURL)
	if err != nil {
		return "", fmt.Errorf("invalid relay URL: %w", err)
	}

	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
		// already a WS URL
	default:
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	if !strings.HasSuffix(u.Path, "/tunnel/connect") {
		u.Path = strings.TrimRight(u.Path, "/") + "/tunnel/connect"
	}

	return u.String(), nil
}
