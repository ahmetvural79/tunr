package main

import (
	"fmt"

	"github.com/ahmetvural79/tunr/internal/daemon"
	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/term"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"list"},
		Short:   "Show active tunnels and daemon status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := daemon.ReadPID()
			if err != nil {
				return fmt.Errorf("failed to read daemon state: %w", err)
			}

			if state == nil || !daemon.IsRunning() {
				logger.Info("No daemon running. Start one with: tunr start --port PORT")
				return nil
			}

			fmt.Println()
			term.Cyan.Println("  tunr daemon running")
			term.Dim.Printf("  PID: %d  Started: %s  Version: %s\n",
				state.PID,
				state.StartedAt.Format("15:04:05"),
				state.Version,
			)
			fmt.Println()

			if len(state.Tunnels) == 0 {
				term.Dim.Println("  No active tunnels.")
			} else {
				for _, t := range state.Tunnels {
					fmt.Printf("  %s  :%d  →  %s\n",
						term.Green.Sprint("●"),
						t.LocalPort,
						term.URL.Sprint(t.PublicURL),
					)
				}
			}
			fmt.Println()

			return nil
		},
	}
}
