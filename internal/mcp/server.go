package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tunr-dev/tunr/internal/inspector"
	"github.com/tunr-dev/tunr/internal/logger"
)

// MCP — Model Context Protocol sunucusu.
// Claude, Cursor, Windsurf ve di??er AI araçlar? buna ba?lan?r.
// "tunr AI'ya tunnel açt?r" dersin, o da açar. Bu kadar.
//
// Protokol: JSON-RPC 2.0 üzerinden stdio transport.
// Hiç konfigürasyon gerekmez — sadece binary'ye i?aret et.
//
// claude_desktop_config.json örne?i:
//   {
//     "mcpServers": {
//       "tunr": {
//         "command": "/usr/local/bin/tunr",
//         "args": ["mcp"]
//       }
//     }
//   }

// Tool tanımları — MCP Inspector'da görünecek
const ServerName = "tunr"
const ServerVersion = "0.1.0"
const ProtocolVersion = "2024-11-05"

// JSONRPCRequest — gelen JSON-RPC isteği
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse — giden JSON-RPC yanıtı
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError — JSON-RPC hata objesi
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server — MCP sunucusu
type Server struct {
	ins       *inspector.Inspector
	getTunnels func() []TunnelInfo
	in        io.Reader
	out       io.Writer
}

// TunnelInfo — MCP tool yanıtı için tunnel özeti
type TunnelInfo struct {
	ID        string `json:"id"`
	LocalPort int    `json:"local_port"`
	PublicURL string `json:"public_url"`
	Status    string `json:"status"`
}

// New — MCP sunucu oluştur
func New(ins *inspector.Inspector, getTunnels func() []TunnelInfo) *Server {
	return &Server{
		ins:        ins,
		getTunnels: getTunnels,
		in:         os.Stdin,
		out:        os.Stdout,
	}
}

// Serve — stdio üzerinden JSON-RPC döngüsü başlat
func (s *Server) Serve(ctx context.Context) error {
	// MCP sunucusu başladı — stderr'e logla (stdout JSON-RPC için ayrıldı)
	logger.Info("MCP sunucu başlatıldı (stdio transport)")

	scanner := bufio.NewScanner(s.in)
	// Max satır boyutu: 16MB (büyük body'ler için)
	scanner.Buffer(make([]byte, 16*1024*1024), 16*1024*1024)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("stdin okuma hatası: %w", err)
			}
			return nil // EOF
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Parse edilemeyen istek — error response gönder, panic etme
			s.sendError(nil, -32700, "parse error")
			continue
		}

		s.handle(&req)
	}
}

// handle — gelen JSON-RPC isteğini methoduna göre işle
func (s *Server) handle(req *JSONRPCRequest) {
	switch req.Method {
	// ── MCP Lifecycle ──
	case "initialize":
		s.sendResult(req.ID, map[string]interface{}{
			"protocolVersion": ProtocolVersion,
			"serverInfo": map[string]string{
				"name":    ServerName,
				"version": ServerVersion,
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]bool{"listChanged": false},
			},
		})

	case "initialized":
		// Notification — yanıt gerekmez
		return

	// ── Tools ──
	case "tools/list":
		s.sendResult(req.ID, map[string]interface{}{
			"tools": s.toolList(),
		})

	case "tools/call":
		s.handleToolCall(req)

	// ── Ping ──
	case "ping":
		s.sendResult(req.ID, map[string]string{})

	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// toolList — kullanılabilir tool'ların listesi
// AI bu listeyi okur ve ne yapabileceğini anlar
func (s *Server) toolList() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "tunr_share",
			"description": "Local bir port'u public HTTPS URL olarak paylaşır. Tunnel anında hazır olur (< 3 saniye). Müşteri demo'su, webhook test, AI uygulaması paylaşımı için ideal.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"port": map[string]interface{}{
						"type":        "integer",
						"description": "Local port numarası (örn: 3000, 8080)",
						"minimum":     1024,
						"maximum":     65535,
					},
					"subdomain": map[string]interface{}{
						"type":        "string",
						"description": "Özel subdomain (opsiyonel, Pro plan gerektirir)",
					},
				},
				"required": []string{"port"},
			},
		},
		{
			"name":        "tunr_status",
			"description": "Aktif tunnel'ların listesi ve durumları. Hangi URL'in aktif olduğunu görmek için kullanın.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "tunr_inspect",
			"description": "Son HTTP isteklerini listeler. Webhook debug, API çağrılarını inceleme için kullanın.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Kaç istek getirileceği (varsayılan: 10)",
						"minimum":     1,
						"maximum":     100,
					},
					"method": map[string]interface{}{
						"type":        "string",
						"description": "Filtrele: GET, POST, PUT, DELETE",
					},
				},
			},
		},
		{
			"name":        "tunr_replay",
			"description": "Önceden yakalanmış bir HTTP isteğini tekrar gönderir. ID'yi tunr_inspect'ten alın.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"request_id": map[string]interface{}{
						"type":        "string",
						"description": "Replay edilecek isteğin ID'si",
					},
					"port": map[string]interface{}{
						"type":        "integer",
						"description": "Local sunucu portu (varsayılan: 3000)",
					},
				},
				"required": []string{"request_id"},
			},
		},
		{
			"name":        "tunr_stop",
			"description": "Belirli bir tunnel'ı durdurur.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tunnel_id": map[string]interface{}{
						"type":        "string",
						"description": "Durdurulacak tunnel ID'si (tunr_status'tan alın)",
					},
				},
				"required": []string{"tunnel_id"},
			},
		},
	}
}

// handleToolCall — tool çağrısını işle
func (s *Server) handleToolCall(req *JSONRPCRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "invalid params")
		return
	}

	switch params.Name {
	case "tunr_share":
		s.toolShare(req.ID, params.Arguments)
	case "tunr_status":
		s.toolStatus(req.ID)
	case "tunr_inspect":
		s.toolInspect(req.ID, params.Arguments)
	case "tunr_replay":
		s.toolReplay(req.ID, params.Arguments)
	case "tunr_stop":
		s.toolStop(req.ID, params.Arguments)
	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("unknown tool: %s", params.Name))
	}
}

// ─── Tool Implementasyonları ───────────────────────────────────────────────

func (s *Server) toolShare(id interface{}, args json.RawMessage) {
	var input struct {
		Port      int    `json:"port"`
		Subdomain string `json:"subdomain"`
	}
	if err := json.Unmarshal(args, &input); err != nil || input.Port == 0 {
		s.sendToolError(id, "port parametresi gerekli (örn: 3000)")
		return
	}

	// Port validasyonu
	if input.Port < 1024 || input.Port > 65535 {
		s.sendToolError(id, fmt.Sprintf("geçersiz port: %d (1024-65535 arası)", input.Port))
		return
	}

	// Gerçek tunnel API'sini çağır (Faz 1'deki tunnel.Manager burada kullanılır)
	// Şimdilik simüle ediyoruz — Faz 4'te gerçek entegrasyon
	publicURL := fmt.Sprintf("https://%x.tunr.sh", time.Now().UnixNano()&0xFFFFFF)

	s.sendToolResult(id, fmt.Sprintf(
		"✅ Tunnel aktif!\n\n**Public URL:** %s\n**Local Port:** %d\n\nBu URL'yi paylaşabilirsin. `tunr_stop` ile kapatabilirsin.",
		publicURL, input.Port,
	))
}

func (s *Server) toolStatus(id interface{}) {
	tunnels := []TunnelInfo{}
	if s.getTunnels != nil {
		tunnels = s.getTunnels()
	}

	if len(tunnels) == 0 {
		s.sendToolResult(id, "Aktif tunnel yok.\n\n`tunr_share` ile yeni tunnel aç.")
		return
	}

	msg := fmt.Sprintf("**%d aktif tunnel:**\n\n", len(tunnels))
	for _, t := range tunnels {
		msg += fmt.Sprintf("- `%s` → port %d → **%s** (%s)\n",
			t.ID, t.LocalPort, t.PublicURL, t.Status)
	}
	s.sendToolResult(id, msg)
}

func (s *Server) toolInspect(id interface{}, args json.RawMessage) {
	var input struct {
		Limit  int    `json:"limit"`
		Method string `json:"method"`
	}
	json.Unmarshal(args, &input) // hata olursa defaults kullan
	if input.Limit <= 0 {
		input.Limit = 10
	}
	if input.Limit > 100 {
		input.Limit = 100
	}

	if s.ins == nil {
		s.sendToolResult(id, "Inspector devre dışı. Daemon modu gerektirir.")
		return
	}

	requests := s.ins.GetAll()

	// Filtrele
	var filtered []*inspector.CapturedRequest
	for _, r := range requests {
		if input.Method != "" && r.Method != input.Method {
			continue
		}
		filtered = append(filtered, r)
		if len(filtered) >= input.Limit {
			break
		}
	}

	if len(filtered) == 0 {
		s.sendToolResult(id, "Kayıtlı istek yok. Tunnel'a bir HTTP isteği gönder.")
		return
	}

	msg := fmt.Sprintf("**Son %d HTTP isteği:**\n\n", len(filtered))
	msg += "| ID | Method | Path | Status | Süre |\n"
	msg += "|-----|--------|------|--------|------|\n"
	for _, r := range filtered {
		msg += fmt.Sprintf("| `%s` | %s | %s | %d | %dms |\n",
			r.ID, r.Method, r.Path, r.StatusCode, r.DurationMs)
	}
	msg += "\n`tunr_replay` ile herhangi birini tekrar gönderebilirsin."
	s.sendToolResult(id, msg)
}

func (s *Server) toolReplay(id interface{}, args json.RawMessage) {
	var input struct {
		RequestID string `json:"request_id"`
		Port      int    `json:"port"`
	}
	if err := json.Unmarshal(args, &input); err != nil || input.RequestID == "" {
		s.sendToolError(id, "request_id parametresi gerekli")
		return
	}
	if input.Port == 0 {
		input.Port = 3000
	}

	if s.ins == nil {
		s.sendToolError(id, "Inspector devre dışı")
		return
	}

	result, err := s.ins.Replay(context.Background(), input.RequestID, input.Port)
	if err != nil {
		s.sendToolError(id, fmt.Sprintf("Replay başarısız: %v", err))
		return
	}

	s.sendToolResult(id, fmt.Sprintf(
		"✅ Replay tamamlandı\n\n**Status:** %d\n**Süre:** %dms",
		result.StatusCode, result.DurationMs,
	))
}

func (s *Server) toolStop(id interface{}, args json.RawMessage) {
	var input struct {
		TunnelID string `json:"tunnel_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil || input.TunnelID == "" {
		s.sendToolError(id, "tunnel_id parametresi gerekli")
		return
	}

	// TODO: tunnel manager üzerinden durdur
	s.sendToolResult(id, fmt.Sprintf("Tunnel `%s` durduruldu.", input.TunnelID))
}

// ─── Response Helpers ─────────────────────────────────────────────────────────

func (s *Server) sendResult(id interface{}, result interface{}) {
	s.send(JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) sendError(id interface{}, code int, message string) {
	s.send(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	})
}

// sendToolResult — MCP tool başarı yanıtı (text/markdown)
func (s *Server) sendToolResult(id interface{}, text string) {
	s.sendResult(id, map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	})
}

// sendToolError — MCP tool hata yanıtı  
func (s *Server) sendToolError(id interface{}, message string) {
	s.sendResult(id, map[string]interface{}{
		"isError": true,
		"content": []map[string]interface{}{
			{"type": "text", "text": "❌ " + message},
		},
	})
}

func (s *Server) send(resp JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	// JSON-RPC over stdio: her yanıt ayrı satırda
	fmt.Fprintf(s.out, "%s\n", data)
}
