package tunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/gorilla/websocket"
)

// tcpStream tracks a single TCP session forwarded between relay ↔ local-service.
type tcpStream struct {
	conn   net.Conn
	w      *net.Conn
	cancel context.CancelFunc
}

// TCPRelayConn wraps a control WebSocket used for TCP tunnel signaling.
type TCPRelayConn struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex
	streams   map[string]*tcpStream
	streamsMu sync.Mutex
}

func (tc *TCPRelayConn) writeJSON(v interface{}) error {
	tc.writeMu.Lock()
	defer tc.writeMu.Unlock()
	_ = tc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return tc.conn.WriteJSON(v)
}

// RunTCPLoop reads relay control messages and manages bidirectional TCP forwarding.
func (tc *TCPRelayConn) RunTCPLoop(ctx context.Context, localPort int) error {
	tc.streams = make(map[string]*tcpStream)
	errCh := make(chan error, 1)

	go func() {
		for {
			var msg wsMessage
			if err := tc.conn.ReadJSON(&msg); err != nil {
				errCh <- err
				return
			}

			switch msg.Type {
			case MsgTypePing:
				_ = tc.writeJSON(wsMessage{Type: MsgTypePong})

			case MsgTypeTCPOpen:
				var open tcpOpenData
				if err := json.Unmarshal(msg.Data, &open); err != nil {
					logger.Warn("TCP open parse error: %v", err)
					continue
				}
				go tc.handleNewStream(ctx, open, localPort)

			case MsgTypeTCPData:
				var d tcpDataPayload
				if err := json.Unmarshal(msg.Data, &d); err != nil {
					continue
				}
				tc.streamsMu.Lock()
				s := tc.streams[d.StreamID]
				tc.streamsMu.Unlock()
				if s != nil {
					raw, _ := base64.StdEncoding.DecodeString(d.PayloadB64)
					s.conn.Write(raw)
				}

			case MsgTypeTCPClose:
				var cl tcpClosePayload
				if err := json.Unmarshal(msg.Data, &cl); err != nil {
					continue
				}
				tc.closeStream(cl.StreamID)

			case MsgTypeClose:
				errCh <- nil
				return

			case MsgTypeError:
				logger.Warn("TCP relay error: %s", string(msg.Data))
			}
		}
	}()

	select {
	case <-ctx.Done():
		tc.closeAllStreams()
		_ = tc.writeJSON(wsMessage{Type: MsgTypeClose})
		tc.conn.Close()
		return ctx.Err()
	case err := <-errCh:
		tc.closeAllStreams()
		tc.conn.Close()
		return err
	}
}

func (tc *TCPRelayConn) handleNewStream(ctx context.Context, open tcpOpenData, localPort int) {
	conn, err := dialLocalPort(ctx, localPort)
	if err != nil {
		logger.Warn("TCP local connect failed: %v", err)
		tc.sendTCPClose(open.StreamID, "local_unavailable")
		return
	}

	stream := &tcpStream{
		conn: conn,
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream.cancel = cancel

	tc.streamsMu.Lock()
	tc.streams[open.StreamID] = stream
	tc.streamsMu.Unlock()

	// local → relay
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				encoded := base64.StdEncoding.EncodeToString(buf[:n])
				payload, _ := json.Marshal(tcpDataPayload{
					StreamID:   open.StreamID,
					PayloadB64: encoded,
				})
				if werr := tc.writeJSON(wsMessage{Type: MsgTypeTCPData, Data: payload}); werr != nil {
					cancel()
					return
				}
			}
			if err != nil {
				cancel()
				return
			}
			select {
			case <-streamCtx.Done():
				return
			default:
			}
		}
	}()

	// Wait for either side to close
	<-streamCtx.Done()
	tc.closeStream(open.StreamID)
}

func (tc *TCPRelayConn) closeStream(id string) {
	tc.streamsMu.Lock()
	s := tc.streams[id]
	if s != nil {
		if s.cancel != nil {
			s.cancel()
		}
		s.conn.Close()
		delete(tc.streams, id)
	}
	tc.streamsMu.Unlock()
}

func (tc *TCPRelayConn) closeAllStreams() {
	tc.streamsMu.Lock()
	for id := range tc.streams {
		if tc.streams[id].cancel != nil {
			tc.streams[id].cancel()
		}
		tc.streams[id].conn.Close()
	}
	tc.streams = make(map[string]*tcpStream)
	tc.streamsMu.Unlock()
}

func (tc *TCPRelayConn) sendTCPClose(streamID, reason string) {
	payload, _ := json.Marshal(tcpClosePayload{StreamID: streamID, Reason: reason})
	_ = tc.writeJSON(wsMessage{Type: MsgTypeTCPClose, Data: payload})
}

// ────────────────────────── Data Types ──────────────────────────

type tcpOpenData struct {
	StreamID   string `json:"stream_id"`
	RemoteAddr string `json:"remote_addr,omitempty"`
}

type tcpDataPayload struct {
	StreamID   string `json:"stream_id"`
	PayloadB64 string `json:"payload_b64"`
}

type tcpClosePayload struct {
	StreamID string `json:"stream_id"`
	Reason   string `json:"reason,omitempty"`
}

// ────────────────────────── Connection Helpers ──────────────────────────

func dialLocalPort(ctx context.Context, port int) (net.Conn, error) {
	d := &net.Dialer{Timeout: 5 * time.Second}
	return d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

// ConnectRelayTCP establishes a relay WebSocket for a TCP tunnel, performs hello/welcome handshake.
func ConnectRelayTCP(ctx context.Context, relayURL, token string, port int, subdomain, version, region string) (*TCPRelayConn, *welcomeData, error) {
	wsURL, err := buildWSURL(relayURL)
	if err != nil {
		return nil, nil, err
	}

	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, nil, fmt.Errorf("WS dial failed: %w", err)
	}
	conn.SetReadLimit(64 << 20)

	rc := &TCPRelayConn{conn: conn}

	helloPayload, _ := json.Marshal(helloData{
		Token:     token,
		LocalPort: port,
		Subdomain: subdomain,
		Version:   version,
		Protocol:  "tcp",
		Region:    region,
	})
	if err := rc.writeJSON(wsMessage{Type: MsgTypeHello, Data: helloPayload}); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("TCP hello failed: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	var welcomeMsg wsMessage
	if err := conn.ReadJSON(&welcomeMsg); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("TCP welcome failed: %w", err)
	}

	if welcomeMsg.Type == MsgTypeError {
		conn.Close()
		return nil, nil, fmt.Errorf("relay rejected TCP: %s", string(welcomeMsg.Data))
	}
	if welcomeMsg.Type != MsgTypeWelcome {
		conn.Close()
		return nil, nil, fmt.Errorf("unexpected TCP message: %s", welcomeMsg.Type)
	}

	var welcome welcomeData
	if err := json.Unmarshal(welcomeMsg.Data, &welcome); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("TCP welcome parse failed: %w", err)
	}

	_ = conn.SetReadDeadline(time.Time{})
	return rc, &welcome, nil
}
