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

func newTCPCmd() *cobra.Command {
	var port int
	var noOpen bool
	var jsonOutput bool
	var qrCode bool
	var allowedIPs []string
	var region string

	cmd := &cobra.Command{
		Use:     "tcp",
		Aliases: []string{"tcp-proxy"},
		Short:   "Expose a local TCP port over the internet",
		Long: `Create a raw TCP tunnel to expose databases, SSH servers, or any TCP service.

TCP tunnels forward raw bytes — no HTTP parsing on the relay side.`,
		Example: `  tunr tcp --port 5432
  tunr tcp --port 22 --qr
  tunr tcp --port 6379 --allow-ip 1.2.3.0/24`,
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
				Protocol:     tunnel.ProtocolTCP,
				Region:       region,
				AuthToken:    token,
				AllowedIPs:   allowedIPs,
				QREnabled:   qrCode,
				HTTPS:        cfg.Tunnel.TLSVerify,
			}

			logger.Info("Starting TCP tunnel (port %d)...", port)

			t, err := mgr.Start(ctx, port, opts)
			if err != nil {
				return fmt.Errorf("TCP tunnel failed: %w", err)
			}

			if jsonOutput {
				fmt.Printf(`{"url":"%s","id":"%s","port":%d,"protocol":"tcp"}`+"\n", t.PublicURL, t.ID, port)
			} else {
				logger.PrintURL(t.PublicURL)
				printTCPInfo(t, port, opts)
			}

			<-ctx.Done()

			fmt.Println()
			logger.Info("Closing TCP tunnel %s...", t.ID)
			mgr.Remove(t.ID)

			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Local TCP port to expose (required)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't auto-open browser")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().BoolVar(&qrCode, "qr", false, "Display QR code for the public URL")
	cmd.Flags().StringSliceVar(&allowedIPs, "allow-ip", nil, "Whitelist IPs (CIDR, comma-separated)")
	cmd.Flags().StringVar(&region, "region", "", "Relay region (e.g. ams, sea, sin)")

	_ = cmd.MarkFlagRequired("port")

	return cmd
}

func printTCPInfo(t *tunnel.Tunnel, port int, opts tunnel.StartOptions) {
	fmt.Println()
	term.Green.Printf("  => ")
	fmt.Printf("localhost:%d", port)
	term.Dim.Print("  →  ")
	term.Cyan.Println(t.PublicURL)
	term.Yellow.Printf("  Protocol:   TCP\n")
	fmt.Println()

	if len(opts.AllowedIPs) > 0 {
		term.Dim.Printf("  Allowed IPs:  %s\n", fmt.Sprintf("%v", opts.AllowedIPs))
	}

	fmt.Println()
	term.Dim.Println("  Connect to your service with:")
	term.Dim.Printf("  ssh user@%s -p 443\n", t.PublicURL)
	fmt.Println()
	term.Dim.Println("  Press Ctrl+C to disconnect")
	fmt.Println()

	if opts.QREnabled && t.PublicURL != "" {
		logger.Info("QR code not available for TCP tunnels")
	}
}
