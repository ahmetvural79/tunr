package main

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/tunr-dev/tunr/internal/daemon"
	"github.com/tunr-dev/tunr/internal/logger"
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

			cyan := color.New(color.FgCyan, color.Bold)
			dim := color.New(color.FgHiBlack)
			green := color.New(color.FgGreen, color.Bold)

			fmt.Println()
			cyan.Println("  tunr daemon running")
			dim.Printf("  PID: %d  Started: %s  Version: %s\n",
				state.PID,
				state.StartedAt.Format("15:04:05"),
				state.Version,
			)
			fmt.Println()

			if len(state.Tunnels) == 0 {
				dim.Println("  No active tunnels.")
			} else {
				for _, t := range state.Tunnels {
					fmt.Printf("  %s  :%d  →  %s\n",
						green.Sprint("●"),
						t.LocalPort,
						color.New(color.FgGreen, color.Underline).Sprint(t.PublicURL),
					)
				}
			}
			fmt.Println()

			return nil
		},
	}
}
