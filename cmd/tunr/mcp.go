package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/ahmetvural79/tunr/internal/inspector"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/mcp"
	"github.com/ahmetvural79/tunr/internal/tunnel"
	"github.com/spf13/cobra"
)

// collectDeploy consumes the control plane's SSE build stream and folds it into
// a single result. Build output is kept so a failed deploy comes back with the
// reason attached, not just "it failed".
func collectDeploy(body io.Reader, name string) (mcp.DeployResult, error) {
	res := mcp.DeployResult{Name: name}
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev); err != nil {
			continue
		}
		switch ev["event"] {
		case "log":
			res.Log = append(res.Log, str(ev["line"]))
		case "live":
			res.URL = str(ev["url"])
			return res, nil
		case "failed":
			return res, fmt.Errorf("%s", str(ev["error"]))
		}
	}
	if err := sc.Err(); err != nil {
		return res, err
	}
	return res, fmt.Errorf("deploy stream ended without a result")
}

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

			// stdout is the JSON-RPC transport here — anything else printed on
			// it is a parse error for the client, so INFO/WARN move to stderr.
			logger.SetInfoOutput(os.Stderr)

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

			// deployer packs a directory and drives the same /v1/deploy pipeline
			// `tunr deploy` uses, collecting the SSE stream instead of printing
			// it — an MCP tool call has to return once, not stream.
			deployer := func(dctx context.Context, req mcp.DeployRequest) (mcp.DeployResult, error) {
				if token == "" {
					return mcp.DeployResult{}, fmt.Errorf("not logged in — run: tunr login")
				}
				dir := req.Dir
				if dir == "" {
					dir = "."
				}
				absDir, err := filepath.Abs(dir)
				if err != nil {
					return mcp.DeployResult{}, err
				}
				name := req.Name
				if name == "" {
					name = sanitizeName(filepath.Base(absDir))
				}

				var tarBuf bytes.Buffer
				if _, err := tarDir(absDir, &tarBuf); err != nil {
					return mcp.DeployResult{}, fmt.Errorf("packing %s failed: %w", absDir, err)
				}
				if tarBuf.Len() > 50<<20 {
					return mcp.DeployResult{}, fmt.Errorf("upload is %s (>50MB); exclude large dirs via .gitignore", human(tarBuf.Len()))
				}

				metaJSON, _ := json.Marshal(map[string]any{
					"name": name, "internal_port": req.Port, "env": req.Env,
				})
				var body bytes.Buffer
				mw := multipart.NewWriter(&body)
				_ = mw.WriteField("meta", string(metaJSON))
				fw, _ := mw.CreateFormFile("source", "source.tar.gz")
				if _, err := io.Copy(fw, &tarBuf); err != nil {
					return mcp.DeployResult{}, err
				}
				_ = mw.Close()

				dreq, err := http.NewRequestWithContext(dctx, http.MethodPost, relayURL()+"/v1/deploy", &body)
				if err != nil {
					return mcp.DeployResult{}, err
				}
				dreq.Header.Set("Authorization", "Bearer "+token)
				dreq.Header.Set("Content-Type", mw.FormDataContentType())

				resp, err := (&http.Client{}).Do(dreq)
				if err != nil {
					return mcp.DeployResult{}, err
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
					return mcp.DeployResult{}, fmt.Errorf("deploy failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
				}
				return collectDeploy(resp.Body, name)
			}

			logReader := func(lctx context.Context, name string, tail int) (string, error) {
				if token == "" {
					return "", fmt.Errorf("not logged in — run: tunr login")
				}
				u := fmt.Sprintf("%s/v1/apps/logs?name=%s&tail=%d", relayURL(), url.QueryEscape(name), tail)
				lreq, err := http.NewRequestWithContext(lctx, http.MethodGet, u, nil)
				if err != nil {
					return "", err
				}
				lreq.Header.Set("Authorization", "Bearer "+token)
				resp, err := http.DefaultClient.Do(lreq)
				if err != nil {
					return "", err
				}
				defer resp.Body.Close()
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
				if resp.StatusCode != http.StatusOK {
					return "", fmt.Errorf("%s", strings.TrimSpace(string(b)))
				}
				return string(b), nil
			}

			deleter := func(dctx context.Context, name string) error {
				if token == "" {
					return fmt.Errorf("not logged in — run: tunr login")
				}
				dreq, err := http.NewRequestWithContext(dctx, http.MethodDelete,
					relayURL()+"/v1/apps?name="+url.QueryEscape(name), nil)
				if err != nil {
					return err
				}
				dreq.Header.Set("Authorization", "Bearer "+token)
				resp, err := http.DefaultClient.Do(dreq)
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
					return fmt.Errorf("%s", strings.TrimSpace(string(b)))
				}
				return nil
			}

			server := mcp.New(ins, snapshot).
				WithTunnelStarter(starter).
				WithTunnelStopper(stopper).
				WithAppsLister(appsLister).
				WithAppDeployer(deployer).
				WithAppLogReader(logReader).
				WithAppDeleter(deleter)

			defer mgr.StopAll()
			return server.Serve(ctx)
		},
	}
}
