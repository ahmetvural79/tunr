package main

import (
	"fmt"
	"os/signal"
	"syscall"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/ahmetvural79/tunr/internal/config"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/term"
	"github.com/ahmetvural79/tunr/internal/tunnel"
	"github.com/spf13/cobra"
)

func newTLSCmd() *cobra.Command {
	var port int
	var noOpen bool
	var jsonOutput bool
	var qrCode bool
	var allowedIPs []string
	var region string

	cmd := &cobra.Command{
		Use:     "tls",
		Aliases: []string{"tls-proxy", "e2e"},
		Short:   "Expose a local TLS port with end-to-end encryption",
		Long: `Create a TLS tunnel with end-to-end encryption — the relay CANNOT read your traffic.

TLS tunnels use SNI-based routing: the relay passes encrypted bytes through
without TLS termination. Perfect for zero-trust / compliance scenarios.`,
		Example: `  tunr tls --port 8443
  tunr tls --port 443 --qr
  tunr tls --port 8443 --allow-ip 10.0.0.0/8 --region ams`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()

			if port == 0 {
				return fmt.Errorf("port is required (use --port)")
			}

			cfg, err := config.Load()
			if err != nil {
				logger.Warn("Config not found, using defaults")
				cfg = config.DefaultConfig()
			}

			token, _ := auth.GetToken()

			mgr := tunnel.NewManager(relayURL())
			mgr.SetAuthToken(token)

			opts := tunnel.StartOptions{
				Protocol:   tunnel.ProtocolTLS,
				Region:     region,
				AuthToken:  token,
				AllowedIPs: allowedIPs,
				QREnabled:  qrCode,
				HTTPS:      cfg.Tunnel.TLSVerify,
			}

			logger.Info("Starting TLS tunnel (port %d)...", port)

			t, err := mgr.Start(ctx, port, opts)
			if err != nil {
				return fmt.Errorf("TLS tunnel failed: %w", err)
			}

			if jsonOutput {
				fmt.Printf(`{"url":"%s","id":"%s","port":%d,"protocol":"tls"}`+"\n", t.PublicURL, t.ID, port)
			} else {
				logger.PrintURL(t.PublicURL)
				printTLSInfo(t, port, opts)
			}

			<-ctx.Done()

			fmt.Println()
			logger.Info("Closing TLS tunnel %s...", t.ID)
			mgr.Remove(t.ID)

			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Local TLS port to expose (required)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't auto-open browser")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().BoolVar(&qrCode, "qr", false, "Display QR code for the public URL")
	cmd.Flags().StringSliceVar(&allowedIPs, "allow-ip", nil, "Whitelist IPs (CIDR, comma-separated)")
	cmd.Flags().StringVar(&region, "region", "", "Relay region (e.g. ams, sea, sin)")

	_ = cmd.MarkFlagRequired("port")

	return cmd
}

func printTLSInfo(t *tunnel.Tunnel, port int, opts tunnel.StartOptions) {
	fmt.Println()
	term.Green.Printf("  => ")
	fmt.Printf("localhost:%d", port)
	term.Dim.Print("  →  ")
	term.Cyan.Println(t.PublicURL)
	term.Yellow.Printf("  Protocol:   TLS (end-to-end encrypted)\n")
	term.Green.Printf("  🔒 Zero-trust: relay cannot read your traffic\n")
	fmt.Println()

	if len(opts.AllowedIPs) > 0 {
		term.Dim.Printf("  Allowed IPs:  %s\n", fmt.Sprintf("%v", opts.AllowedIPs))
	}

	fmt.Println()
	term.Dim.Println("  Press Ctrl+C to disconnect")
	fmt.Println()
}
