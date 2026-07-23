package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

			// appsLister queries the cloud control plane for the user's deployed apps.
			appsLister := func() ([]mcp.AppInfo, error) {
				if token == "" {
					return nil, fmt.Errorf("not logged in — run: tunr login")
				}
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, relayURL()+"/v1/apps", nil)
				if err != nil {
					return nil, err
				}
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return nil, err
				}
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusUnauthorized {
					return nil, fmt.Errorf("not logged in — run: tunr login")
				}
				if resp.StatusCode != http.StatusOK {
					b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
					return nil, fmt.Errorf("control plane returned %d: %s", resp.StatusCode, string(b))
				}
				var out struct {
					Apps []mcp.AppInfo `json:"apps"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
					return nil, err
				}
				return out.Apps, nil
			}

			server := mcp.New(ins, snapshot).
				WithTunnelStarter(starter).
				WithTunnelStopper(stopper).
				WithAppsLister(appsLister)

			defer mgr.StopAll()
			return server.Serve(ctx)
		},
	}
}
