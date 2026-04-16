package main

import (
	"fmt"
	"os/signal"
	"syscall"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/ahmetvural79/tunr/internal/config"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/proxy"
	"github.com/ahmetvural79/tunr/internal/term"
	"github.com/ahmetvural79/tunr/internal/tunnel"
	"github.com/spf13/cobra"
)

func newUDPCmd() *cobra.Command {
	var port int
	var noOpen bool
	var jsonOutput bool
	var qrCode bool
	var allowedIPs []string
	var region string

	cmd := &cobra.Command{
		Use:     "udp",
		Aliases: []string{"udp-proxy"},
		Short:   "Expose a local UDP port over the internet",
		Long: `Create a UDP tunnel to expose DNS servers, game servers, or any UDP service.

UDP tunnels forward raw datagrams — zero TCP/HTTP overhead.`,
		Example: `  tunr udp --port 53
  tunr udp --port 27015 --qr
  tunr udp --port 53 --allow-ip 10.0.0.0/8 --region ams`,
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
				Protocol:   tunnel.ProtocolUDP,
				Region:     region,
				AuthToken:  token,
				AllowedIPs: allowedIPs,
				QREnabled:  qrCode,
				HTTPS:      cfg.Tunnel.TLSVerify,
			}

			logger.Info("Starting UDP tunnel (port %d)...", port)

			t, err := mgr.Start(ctx, port, opts)
			if err != nil {
				return fmt.Errorf("UDP tunnel failed: %w", err)
			}

			if jsonOutput {
				fmt.Printf(`{"url":"%s","id":"%s","port":%d,"protocol":"udp"}`+"\n", t.PublicURL, t.ID, port)
			} else {
				logger.PrintURL(t.PublicURL)
				printUDPInfo(t, port, opts)
			}

			<-ctx.Done()

			fmt.Println()
			logger.Info("Closing UDP tunnel %s...", t.ID)
			mgr.Remove(t.ID)

			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Local UDP port to expose (required)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't auto-open browser")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().BoolVar(&qrCode, "qr", false, "Display QR code for the public URL")
	cmd.Flags().StringSliceVar(&allowedIPs, "allow-ip", nil, "Whitelist IPs (CIDR, comma-separated)")
	cmd.Flags().StringVar(&region, "region", "", "Relay region (e.g. ams, sea, sin)")

	_ = cmd.MarkFlagRequired("port")

	return cmd
}

func printUDPInfo(t *tunnel.Tunnel, port int, opts tunnel.StartOptions) {
	fmt.Println()
	term.Green.Printf("  => ")
	fmt.Printf("localhost:%d", port)
	term.Dim.Print("  →  ")
	term.Cyan.Println(t.PublicURL)
	term.Yellow.Printf("  Protocol:   UDP\n")
	fmt.Println()

	if len(opts.AllowedIPs) > 0 {
		term.Dim.Printf("  Allowed IPs:  %s\n", fmt.Sprintf("%v", opts.AllowedIPs))
	}

	fmt.Println()
	term.Dim.Println("  Perfect for DNS, game servers, and real-time apps.")
	fmt.Println()
	term.Dim.Println("  Press Ctrl+C to disconnect")
	fmt.Println()

	if opts.QREnabled && t.PublicURL != "" {
		qr := proxy.GenerateQRCode(t.PublicURL)
		if qr != "" {
			term.Cyan.Println("  Scan to open on mobile:")
			fmt.Println(qr)
		}
	}
}
