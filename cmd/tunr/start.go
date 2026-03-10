package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/ahmetvural79/tunr/internal/daemon"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/tunnel"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	var port int
	var subdomain string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start tunnel in background (daemon mode)",
		Long:  "Run tunnel as a background daemon. Persists after terminal closes.",
		Example: `  tunr start --port 3000
  tunr start --port 8080 --subdomain myapi`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemon.IsRunning() {
				return fmt.Errorf("daemon already running — use 'tunr status' to check")
			}

			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid port: %d (must be 1-65535)", port)
			}

			if err := daemon.WritePID(Version); err != nil {
				logger.Warn("Could not write PID: %v", err)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()
			defer func() { _ = daemon.CleanPID() }()

			token, _ := auth.GetToken()
			mgr := tunnel.NewManager("https://relay.tunr.sh")
			mgr.SetAuthToken(token)

			t, err := mgr.Start(ctx, port, tunnel.StartOptions{
				Subdomain: subdomain,
				HTTPS:     true,
				AuthToken: token,
			})
			if err != nil {
				return fmt.Errorf("daemon tunnel failed: %w", err)
			}

			_ = daemon.AddTunnel(daemon.TunnelInfo{
				ID:        t.ID,
				LocalPort: port,
				PublicURL: t.PublicURL,
				StartedAt: t.StartedAt,
			})

			logger.PrintURL(t.PublicURL)
			logger.Info("Daemon running (PID %d)", os.Getpid())

			<-ctx.Done()
			mgr.StopAll()
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Local port to expose (required)")
	cmd.Flags().StringVarP(&subdomain, "subdomain", "s", "", "Custom subdomain (Pro)")
	_ = cmd.MarkFlagRequired("port")

	return cmd
}
