package main

import (
	"fmt"

	"github.com/tunr-dev/tunr/internal/daemon"
	"github.com/tunr-dev/tunr/internal/logger"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the background daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !daemon.IsRunning() {
				logger.Info("No daemon running.")
				return nil
			}

			if err := daemon.Stop(); err != nil {
				return fmt.Errorf("failed to stop daemon: %w", err)
			}

			logger.Info("Daemon stopped.")
			return nil
		},
	}
}
