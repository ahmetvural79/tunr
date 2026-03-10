package main

import (
	"os/signal"
	"syscall"

	"github.com/ahmetvural79/tunr/internal/inspector"
	"github.com/ahmetvural79/tunr/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (Claude, Cursor, Windsurf)",
		Long: `Start the Model Context Protocol server over stdio.
AI agents can create tunnels and inspect requests programmatically.

Claude Desktop (~/.claude/claude_desktop_config.json):
  {"mcpServers":{"tunr":{"command":"tunr","args":["mcp"]}}}

Cursor (.cursor/mcp.json):
  {"mcpServers":{"tunr":{"command":"tunr","args":["mcp"]}}}`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			ins := inspector.New(1000)
			server := mcp.New(ins, nil)
			return server.Serve(ctx)
		},
	}
}
