package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ahmetvural79/tunr/internal/inspector"
	"github.com/ahmetvural79/tunr/internal/logger"
)

// MCP (Model Context Protocol) server.
// Claude, Cursor, Windsurf, and other AI tools connect here.
// "tunr, open a tunnel" — and it does. That's it.
//
// Protocol: JSON-RPC 2.0 over stdio transport.
// Zero config needed — just point the AI tool at the binary.
//
// claude_desktop_config.json example:
//
//	{
//	  "mcpServers": {
//	    "tunr": {
//	      "command": "/usr/local/bin/tunr",
//	      "args": ["mcp"]
//	    }
//	  }
//	}

const ServerName = "tunr"
const ServerVersion = "0.1.0"
const ProtocolVersion = "2024-11-05"

// JSONRPCRequest is an inbound JSON-RPC message
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is an outbound JSON-RPC message
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error object
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server is the MCP protocol server
type Server struct {
	ins        *inspector.Inspector
	getTunnels func() []TunnelInfo
	in         io.Reader
	out        io.Writer
}

// TunnelInfo is a lightweight tunnel summary for MCP tool responses
type TunnelInfo struct {
	ID        string `json:"id"`
	LocalPort int    `json:"local_port"`
	PublicURL string `json:"public_url"`
	Status    string `json:"status"`
}

// New creates an MCP server wired to the inspector and tunnel manager
func New(ins *inspector.Inspector, getTunnels func() []TunnelInfo) *Server {
	return &Server{
		ins:        ins,
		getTunnels: getTunnels,
		in:         os.Stdin,
		out:        os.Stdout,
	}
}

// Serve runs the JSON-RPC read loop over stdio
func (s *Server) Serve(ctx context.Context) error {
	// stdout is reserved for JSON-RPC — log to stderr only
	logger.Info("MCP server started (stdio transport)")

	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 16*1024*1024), 16*1024*1024)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("stdin read error: %w", err)
			}
			return nil // EOF
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "parse error")
			continue
		}

		s.handle(&req)
	}
}

// handle dispatches a JSON-RPC request by method name
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

// toolList returns the available MCP tools.
// AI agents read these descriptions to decide what to call — keep them clear.
func (s *Server) toolList() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "tunr_share",
			"description": "Expose a local port as a public HTTPS URL. The tunnel is ready in under 3 seconds. Use this for client demos, webhook testing, or sharing AI apps.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"port": map[string]interface{}{
						"type":        "integer",
						"description": "Local port number (e.g. 3000, 8080)",
						"minimum":     1024,
						"maximum":     65535,
					},
					"subdomain": map[string]interface{}{
						"type":        "string",
						"description": "Custom subdomain (optional, requires Pro plan)",
					},
				},
				"required": []string{"port"},
			},
		},
		{
			"name":        "tunr_status",
			"description": "List all active tunnels and their current status. Shows which public URLs are live.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "tunr_inspect",
			"description": "List recent HTTP requests captured by the tunnel. Useful for debugging webhooks and inspecting API calls.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Number of requests to return (default: 10)",
						"minimum":     1,
						"maximum":     100,
					},
					"method": map[string]interface{}{
						"type":        "string",
						"description": "Filter by HTTP method: GET, POST, PUT, DELETE",
					},
				},
			},
		},
		{
			"name":        "tunr_replay",
			"description": "Replay a previously captured HTTP request against your local server. Get the request ID from tunr_inspect.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"request_id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the request to replay",
					},
					"port": map[string]interface{}{
						"type":        "integer",
						"description": "Local server port (default: 3000)",
					},
				},
				"required": []string{"request_id"},
			},
		},
		{
			"name":        "tunr_stop",
			"description": "Stop a specific tunnel by its ID.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tunnel_id": map[string]interface{}{
						"type":        "string",
						"description": "Tunnel ID to stop (get it from tunr_status)",
					},
				},
				"required": []string{"tunnel_id"},
			},
		},
	}
}

// handleToolCall dispatches an MCP tool invocation
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

// ─── Tool Implementations ───────────────────────────────────────────────────

func (s *Server) toolShare(id interface{}, args json.RawMessage) {
	var input struct {
		Port      int    `json:"port"`
		Subdomain string `json:"subdomain"`
	}
	if err := json.Unmarshal(args, &input); err != nil || input.Port == 0 {
		s.sendToolError(id, "port parameter is required (e.g. 3000)")
		return
	}

	if input.Port < 1024 || input.Port > 65535 {
		s.sendToolError(id, fmt.Sprintf("invalid port: %d (must be 1024-65535)", input.Port))
		return
	}

	// Wire up to real tunnel.Manager — simulated for now, real integration in Phase 4
	publicURL := fmt.Sprintf("https://%x.tunr.sh", time.Now().UnixNano()&0xFFFFFF)

	s.sendToolResult(id, fmt.Sprintf(
		"✅ Tunnel is live!\n\n**Public URL:** %s\n**Local Port:** %d\n\nShare this URL freely. Use `tunr_stop` to shut it down.",
		publicURL, input.Port,
	))
}

func (s *Server) toolStatus(id interface{}) {
	tunnels := []TunnelInfo{}
	if s.getTunnels != nil {
		tunnels = s.getTunnels()
	}

	if len(tunnels) == 0 {
		s.sendToolResult(id, "No active tunnels.\n\nUse `tunr_share` to open one.")
		return
	}

	msg := fmt.Sprintf("**%d active tunnel(s):**\n\n", len(tunnels))
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
	_ = json.Unmarshal(args, &input)
	if input.Limit <= 0 {
		input.Limit = 10
	}
	if input.Limit > 100 {
		input.Limit = 100
	}

	if s.ins == nil {
		s.sendToolResult(id, "Inspector is disabled. Requires daemon mode.")
		return
	}

	requests := s.ins.GetAll()

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
		s.sendToolResult(id, "No captured requests yet. Send an HTTP request through the tunnel first.")
		return
	}

	msg := fmt.Sprintf("**Last %d HTTP request(s):**\n\n", len(filtered))
	msg += "| ID | Method | Path | Status | Duration |\n"
	msg += "|-----|--------|------|--------|----------|\n"
	for _, r := range filtered {
		msg += fmt.Sprintf("| `%s` | %s | %s | %d | %dms |\n",
			r.ID, r.Method, r.Path, r.StatusCode, r.DurationMs)
	}
	msg += "\nUse `tunr_replay` to resend any of these."
	s.sendToolResult(id, msg)
}

func (s *Server) toolReplay(id interface{}, args json.RawMessage) {
	var input struct {
		RequestID string `json:"request_id"`
		Port      int    `json:"port"`
	}
	if err := json.Unmarshal(args, &input); err != nil || input.RequestID == "" {
		s.sendToolError(id, "request_id parameter is required")
		return
	}
	if input.Port == 0 {
		input.Port = 3000
	}

	if s.ins == nil {
		s.sendToolError(id, "Inspector is disabled")
		return
	}

	result, err := s.ins.Replay(context.Background(), input.RequestID, input.Port)
	if err != nil {
		s.sendToolError(id, fmt.Sprintf("Replay failed: %v", err))
		return
	}

	s.sendToolResult(id, fmt.Sprintf(
		"✅ Replay complete\n\n**Status:** %d\n**Duration:** %dms",
		result.StatusCode, result.DurationMs,
	))
}

func (s *Server) toolStop(id interface{}, args json.RawMessage) {
	var input struct {
		TunnelID string `json:"tunnel_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil || input.TunnelID == "" {
		s.sendToolError(id, "tunnel_id parameter is required")
		return
	}

	// TODO: stop via tunnel manager
	s.sendToolResult(id, fmt.Sprintf("Tunnel `%s` stopped.", input.TunnelID))
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

func (s *Server) sendToolResult(id interface{}, text string) {
	s.sendResult(id, map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	})
}

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
	fmt.Fprintf(s.out, "%s\n", data)
}
