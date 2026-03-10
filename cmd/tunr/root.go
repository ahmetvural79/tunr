package main

import (
	"fmt"
	"os"

	"github.com/tunr-dev/tunr/internal/logger"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tunr",
	Short: "Local → Public in < 3 seconds",
	Long: `tunr exposes your local dev server to the internet with automatic HTTPS,
WebSocket support, and zero configuration.

  tunr share --port 3000       # public HTTPS URL
  tunr share -p 3000 --demo    # read-only demo mode
  tunr start --port 3000       # background daemon
  tunr status                  # active tunnels

Docs: https://tunr.sh/docs`,
	Version: Version,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cmd.SilenceUsage = true
		if verbose {
			logger.SetLevel(logger.DEBUG)
		}
	},
}

var verbose bool

func Execute() error {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SilenceErrors = true

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", err)
		return err
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")

	rootCmd.AddCommand(
		newShareCmd(),
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newLogsCmd(),
		newDoctorCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newVersionCmd(),
		newOpenCmd(),
		newReplayCmd(),
		newMCPCmd(),
		newConfigCmd(),
		newUpdateCmd(),
		newUninstallCmd(),
	)
}
