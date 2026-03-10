package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/ahmetvural79/tunr/internal/config"
	"github.com/ahmetvural79/tunr/internal/daemon"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/spf13/cobra"
)

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove tunr and all its data",
		Long:  "Stops the daemon, removes config, auth tokens, and the binary itself.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dim := color.New(color.FgHiBlack)
			green := color.New(color.FgGreen)

			fmt.Println()
			logger.Info("Uninstalling tunr...")
			fmt.Println()

			steps := []struct {
				name string
				fn   func() error
			}{
				{"Stopping daemon", func() error {
					if !daemon.IsRunning() {
						return nil
					}
					return daemon.Stop()
				}},
				{"Removing config directory", func() error {
					dir, err := config.ConfigDir()
					if err != nil {
						return nil
					}
					return os.RemoveAll(dir)
				}},
				{"Removing .tunr.json", func() error {
					home, err := os.UserHomeDir()
					if err != nil {
						return nil
					}
					_ = os.Remove(filepath.Join(home, ".tunr.json"))
					_ = os.Remove(".tunr.json")
					return nil
				}},
				{"Removing binary", func() error {
					exe, err := os.Executable()
					if err != nil {
						return nil
					}
					exe, _ = filepath.EvalSymlinks(exe)
					return os.Remove(exe)
				}},
			}

			for _, s := range steps {
				dim.Printf("  %s...", s.name)
				if err := s.fn(); err != nil {
					dim.Println(" skipped")
				} else {
					green.Println(" done")
				}
			}

			fmt.Println()
			logger.Info("tunr has been completely removed.")
			fmt.Println()
			return nil
		},
	}
}
