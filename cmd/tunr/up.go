package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/term"
	"github.com/ahmetvural79/tunr/internal/tunnel"
	"github.com/spf13/cobra"
)

// TunnelDef describes a single tunnel in the multi-tunnel config.
type TunnelDef struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol,omitempty"`  // http, tcp, udp, tls
	Subdomain string `json:"subdomain,omitempty"` // Pro
	Domain    string `json:"domain,omitempty"`     // Pro
	Password  string `json:"password,omitempty"`
	Demo      bool   `json:"demo,omitempty"`
	Freeze    bool   `json:"freeze,omitempty"`
	Region    string `json:"region,omitempty"`
}

// MultiTunnelConfig represents the tunnels section of .tunr.json.
type MultiTunnelConfig struct {
	Tunnels map[string]TunnelDef `json:"tunnels"`
}

func newUpCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start all tunnels defined in .tunr.json",
		Long: `Start all named tunnels from your project configuration file.

Define tunnels in .tunr.json:
  {
    "tunnels": {
      "frontend": { "port": 3000, "subdomain": "app" },
      "api":      { "port": 8080, "subdomain": "api", "password": "secret" },
      "db":       { "port": 5432, "protocol": "tcp" }
    }
  }

Then run: tunr up`,
		Example: `  tunr up
  tunr up --config .tunr.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()

			if configPath == "" {
				configPath = ".tunr.json"
			}

			data, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("cannot read %s: %w (run 'tunr config init' first)", configPath, err)
			}

			var cfg MultiTunnelConfig
			if err := json.Unmarshal(data, &cfg); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			if len(cfg.Tunnels) == 0 {
				return fmt.Errorf("no tunnels defined in %s — add a \"tunnels\" section", configPath)
			}

			token, _ := auth.GetToken()
			mgr := tunnel.NewManager(relayURL())
			mgr.SetAuthToken(token)

			fmt.Println()
			term.Cyan.Printf("  Starting %d tunnel(s) from %s...\n\n", len(cfg.Tunnels), configPath)

			var wg sync.WaitGroup
			var mu sync.Mutex
			results := make(map[string]*tunnel.Tunnel)

			for name, def := range cfg.Tunnels {
				wg.Add(1)
				go func(n string, d TunnelDef) {
					defer wg.Done()

					proto := tunnel.ProtocolHTTP
					switch d.Protocol {
					case "tcp":
						proto = tunnel.ProtocolTCP
					case "udp":
						proto = tunnel.ProtocolUDP
					case "tls":
						proto = tunnel.ProtocolTLS
					}

					opts := tunnel.StartOptions{
						Protocol:  proto,
						Subdomain: d.Subdomain,
						Domain:    d.Domain,
						Password:  d.Password,
						DemoMode:  d.Demo,
						Freeze:    d.Freeze,
						Region:    d.Region,
						AuthToken: token,
					}

					t, err := mgr.Start(ctx, d.Port, opts)
					if err != nil {
						logger.Error("  ✗ %s (port %d): %v", n, d.Port, err)
						return
					}

					mu.Lock()
					results[n] = t
					mu.Unlock()

					proto_str := "HTTP"
					if d.Protocol != "" {
						proto_str = fmt.Sprintf("%s", d.Protocol)
					}
					term.Green.Printf("  ✓ ")
					fmt.Printf("%-12s ", n)
					term.Dim.Printf("(%s) ", proto_str)
					fmt.Printf(":%d → ", d.Port)
					term.Cyan.Println(t.PublicURL)
				}(name, def)
			}
			wg.Wait()

			if len(results) == 0 {
				return fmt.Errorf("no tunnels started successfully — check your config and retry")
			}

			fmt.Println()
			term.Dim.Printf("  %d/%d tunnels active. Press Ctrl+C to stop all.\n\n", len(results), len(cfg.Tunnels))

			<-ctx.Done()

			fmt.Println()
			logger.Info("Shutting down all tunnels...")
			mgr.StopAll()

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file (default: .tunr.json)")

	return cmd
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop all running daemon tunnels",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger.Info("Stopping all daemon tunnels...")
			// Signal the daemon to shut all tunnels
			mgr := tunnel.NewManager(relayURL())
			mgr.StopAll()
			term.Green.Println("  ✓ All tunnels stopped.")
			return nil
		},
	}
}
