package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type msg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type helloData struct {
	LocalPort int    `json:"local_port"`
	Subdomain string `json:"subdomain,omitempty"`
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

type relayState struct {
	mu      sync.RWMutex
	conn    *websocket.Conn
	hello   helloData
	waiters map[string]chan responseData
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func main() {
	state := &relayState{waiters: map[string]chan responseData{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})

	mux.HandleFunc("/tunnel/connect", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var first msg
		if err := conn.ReadJSON(&first); err != nil || first.Type != "hello" {
			_ = conn.Close()
			return
		}

		var hello helloData
		_ = json.Unmarshal(first.Data, &hello)
		if hello.Subdomain == "" {
			hello.Subdomain = "e2e"
		}

		state.mu.Lock()
		state.conn = conn
		state.hello = hello
		state.mu.Unlock()

		welcome := map[string]any{
			"type": "welcome",
			"data": map[string]any{
				"tunnel_id":  "e2e01",
				"subdomain":  hello.Subdomain,
				"public_url": fmt.Sprintf("https://%s.tunr.test", hello.Subdomain),
			},
		}
		_ = conn.WriteJSON(welcome)

		go func() {
			defer conn.Close()
			for {
				var m msg
				if err := conn.ReadJSON(&m); err != nil {
					return
				}
				switch m.Type {
				case "pong":
				case "response":
					var resp responseData
					_ = json.Unmarshal(m.Data, &resp)
					state.mu.Lock()
					ch := state.waiters[resp.RequestID]
					if ch != nil {
						ch <- resp
						close(ch)
						delete(state.waiters, resp.RequestID)
					}
					state.mu.Unlock()
				case "close":
					return
				}
			}
		}()
	})

	mux.HandleFunc("/_test/request", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		conn := state.conn
		state.mu.RUnlock()
		if conn == nil {
			http.Error(w, "no active tunnel connection", http.StatusServiceUnavailable)
			return
		}

		var req requestData
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.RequestID == "" {
			req.RequestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}
		if req.Method == "" {
			req.Method = "GET"
		}

		payload, _ := json.Marshal(req)
		waitCh := make(chan responseData, 1)

		state.mu.Lock()
		state.waiters[req.RequestID] = waitCh
		state.mu.Unlock()

		if err := conn.WriteJSON(map[string]any{"type": "request", "data": json.RawMessage(payload)}); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		select {
		case resp := <-waitCh:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case <-time.After(5 * time.Second):
			http.Error(w, "timeout waiting response", http.StatusGatewayTimeout)
		}
	})

	addr := "127.0.0.1:19080"
	log.Printf("mock relay listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
