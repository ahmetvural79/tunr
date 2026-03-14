package main

import (
	"fmt"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/ahmetvural79/tunr/internal/config"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/term"
	"github.com/ahmetvural79/tunr/internal/tunnel"
	"github.com/spf13/cobra"
)

func newShareCmd() *cobra.Command {
	var port int
	var subdomain string
	var domain string
	var noOpen bool
	var jsonOutput bool

	var demoMode bool
	var freeze bool
	var injectWidget bool
	var autoLogin string

	var password string
	var ttl time.Duration
	var expire time.Duration
	var pathRoutes []string

	cmd := &cobra.Command{
		Use:   "share",
		Short: "Expose a local port with a public HTTPS URL",
		Long: `Share your local dev server to the internet in < 3 seconds.

Vibecoder Demo Flags (Pro):
  --demo            Block mutating requests (POST, PUT, DELETE)
  --freeze          Serve cached responses if localhost crashes
  --inject-widget   Inject feedback UI into HTML pages
  --auto-login      Auto-inject auth cookies for clients`,
		Example: `  tunr share --port 3000
  tunr share --port 8080 --subdomain myapp
  tunr share --port 3000 --domain myapp.example.com
  tunr share -p 3000 --demo --freeze --inject-widget
  tunr share -p 3000 --password secret --ttl 30m`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if subdomain != "" && domain != "" {
				return fmt.Errorf("cannot use --subdomain and --domain together")
			}

			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()

			cfg, err := config.Load()
			if err != nil {
				logger.Warn("Config not found, using defaults")
				cfg = config.DefaultConfig()
			}

			token, _ := auth.GetToken()

			mgr := tunnel.NewManager(relayURL())
			mgr.SetAuthToken(token)

			logger.Info("Starting tunnel (port %d)...", port)

			parsedRoutes := make(map[string]int)
			for _, r := range pathRoutes {
				parts := strings.SplitN(r, "=", 2)
				if len(parts) == 2 {
					p, _ := strconv.Atoi(parts[1])
					if p > 0 {
						parsedRoutes[parts[0]] = p
					}
				}
			}

			opts := tunnel.StartOptions{
				Subdomain:    subdomain,
				Domain:       domain,
				HTTPS:        cfg.Tunnel.TLSVerify,
				AuthToken:    token,
				DemoMode:     demoMode,
				Freeze:       freeze,
				InjectWidget: injectWidget,
				AutoLogin:    autoLogin,
				Password:     password,
				TTL:          ttl,
				PathRoutes:   parsedRoutes,
			}
			if expire > 0 && ttl == 0 {
				opts.TTL = expire
			}

			t, err := mgr.Start(ctx, port, opts)
			if err != nil {
				if strings.Contains(err.Error(), "Pro subscription") {
					return handleProRequired(port, subdomain, domain, password)
				}
				return fmt.Errorf("tunnel failed: %w", err)
			}

			if jsonOutput {
				fmt.Printf(`{"url":"%s","id":"%s","port":%d}`+"\n", t.PublicURL, t.ID, port)
			} else {
				logger.PrintURL(t.PublicURL)
				printShareInfo(t, port, opts)
			}

			<-ctx.Done()

			fmt.Println()
			logger.Info("Closing tunnel %s...", t.ID)
			mgr.Remove(t.ID)

			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Local port to expose (required)")
	cmd.Flags().StringVarP(&subdomain, "subdomain", "s", "", "Custom subdomain (Pro)")
	cmd.Flags().StringVar(&domain, "domain", "", "Custom domain for this tunnel (Pro)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't auto-open browser")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	cmd.Flags().BoolVar(&demoMode, "demo", false, "Block mutating requests (read-only mode)")
	cmd.Flags().BoolVar(&freeze, "freeze", false, "Cache responses, serve on crash")
	cmd.Flags().BoolVar(&injectWidget, "inject-widget", false, "Inject feedback widget into HTML")
	cmd.Flags().StringVar(&autoLogin, "auto-login", "", "Auto-inject auth cookie/header")

	cmd.Flags().StringVar(&password, "password", "", "Protect with Basic Auth (user:pass or just pass)")
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "Auto-close after duration (e.g. 1h, 30m)")
	cmd.Flags().DurationVar(&expire, "expire", 0, "Alias for --ttl")
	cmd.Flags().StringSliceVar(&pathRoutes, "route", nil, "Route paths to ports (e.g. --route /api=8080)")

	_ = cmd.MarkFlagRequired("port")

	return cmd
}

func printShareInfo(t *tunnel.Tunnel, port int, opts tunnel.StartOptions) {
	fmt.Println()
	term.Green.Printf("  => ")
	fmt.Printf("localhost:%d", port)
	term.Dim.Print("  →  ")
	term.Cyan.Println(t.PublicURL)

	if opts.Domain != "" {
		term.Green.Printf("  => ")
		term.Cyan.Println("https://" + opts.Domain)
	}

	fmt.Println()

	if opts.Password != "" {
		term.Dim.Printf("  Password:  %s\n", opts.Password)
	}
	if opts.TTL > 0 {
		term.Dim.Printf("  Expires:   %s\n", opts.TTL)
	}
	if opts.DemoMode {
		term.Dim.Println("  Mode:      read-only (POST/PUT/DELETE blocked)")
	}
	if opts.Freeze {
		term.Dim.Println("  Freeze:    enabled (cache-on-crash)")
	}
	if opts.InjectWidget {
		term.Dim.Println("  Widget:    feedback overlay injected")
	}

	fmt.Println()
	term.Dim.Println("  Press Ctrl+C to disconnect")
	fmt.Println()
}

func handleProRequired(port int, subdomain, domain, password string) error {
	var feature string
	switch {
	case subdomain != "":
		feature = "Custom subdomains"
	case domain != "":
		feature = "Custom domains"
	case password != "":
		feature = "Password protection"
	default:
		feature = "This feature"
	}

	fmt.Println()
	term.Red.Printf("  %s requires a Pro subscription.\n", feature)
	fmt.Println()
	term.Dim.Println("  Upgrade at: https://app.tunr.sh/settings/billing")
	fmt.Println()
	term.Dim.Printf("  Free:  tunr share --port %d\n", port)
	term.Dim.Printf("  Pro:   tunr share --port %d --subdomain myapp\n", port)
	fmt.Println()

	return nil
}
