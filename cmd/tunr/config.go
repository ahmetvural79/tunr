package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ahmetvural79/tunr/internal/config"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage workspace configuration (.tunr.json)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Display current config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found: %w", err)
			}
			data, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Create .tunr.json in current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := os.Stat(".tunr.json"); err == nil {
				return fmt.Errorf(".tunr.json already exists")
			}

			defaultCfg := map[string]interface{}{
				"$schema":          "https://tunr.sh/schema/.tunr.schema.json",
				"port":             3000,
				"inspectorEnabled": true,
				"dashboardPort":    19842,
				"mcp":              map[string]bool{"enabled": true},
			}

			data, err := json.MarshalIndent(defaultCfg, "", "  ")
			if err != nil {
				return err
			}

			if err := os.WriteFile(".tunr.json", data, 0644); err != nil {
				return fmt.Errorf("failed to write .tunr.json: %w", err)
			}

			logger.Info("Created .tunr.json")
			return nil
		},
	})

	return cmd
}
