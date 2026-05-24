package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/ahmetvural79/tunr/internal/inspector"
	"github.com/ahmetvural79/tunr/internal/mcp"
	"github.com/ahmetvural79/tunr/internal/tunnel"
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

			token, _ := auth.GetToken()
			mgr := tunnel.NewManager(relayURL())
			mgr.SetAuthToken(token)

			snapshot := func() []mcp.TunnelInfo {
				active := mgr.List()
				out := make([]mcp.TunnelInfo, 0, len(active))
				for _, t := range active {
					out = append(out, mcp.TunnelInfo{
						ID:        t.ID,
						LocalPort: t.LocalPort,
						PublicURL: t.PublicURL,
						Status:    string(t.Status),
					})
				}
				return out
			}

			starter := func(sctx context.Context, port int, opts mcp.ShareOptions) (mcp.TunnelInfo, error) {
				t, err := mgr.Start(sctx, port, tunnel.StartOptions{
					Subdomain: opts.Subdomain,
					AuthToken: token,
				})
				if err != nil {
					return mcp.TunnelInfo{}, err
				}
				return mcp.TunnelInfo{
					ID:        t.ID,
					LocalPort: t.LocalPort,
					PublicURL: t.PublicURL,
					Status:    string(t.Status),
				}, nil
			}

			stopper := func(id string) error {
				for _, t := range mgr.List() {
					if t.ID == id {
						mgr.Remove(id)
						return nil
					}
				}
				return fmt.Errorf("no tunnel with id %q", id)
			}

			server := mcp.New(ins, snapshot).
				WithTunnelStarter(starter).
				WithTunnelStopper(stopper)

			defer mgr.StopAll()
			return server.Serve(ctx)
		},
	}
}
