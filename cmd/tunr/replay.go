package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/tunr-dev/tunr/internal/logger"
	"github.com/spf13/cobra"
)

func newReplayCmd() *cobra.Command {
	var localPort int
	var exportCurl bool

	cmd := &cobra.Command{
		Use:   "replay <request-id>",
		Short: "Replay a captured HTTP request",
		Long:  "Re-send a request captured by the inspector to your local server.\nAlso supports exporting as a curl command.",
		Example: `  tunr replay abc12345
  tunr replay abc12345 --port 3000
  tunr replay abc12345 --curl`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestID := args[0]

			for _, c := range requestID {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
					return fmt.Errorf("invalid request ID: only alphanumeric and hyphens allowed")
				}
			}

			apiURL := fmt.Sprintf("http://localhost:19842/api/v1/requests/%s", requestID)

			if exportCurl {
				resp, err := http.Post(apiURL+"?action=curl", "application/json", nil)
				if err != nil {
					return fmt.Errorf("failed to export curl: %w", err)
				}
				defer resp.Body.Close()
				_, err = fmt.Fscan(os.Stdout, resp.Body)
				return err
			}

			resp, err := http.Post(
				fmt.Sprintf("%s?action=replay&port=%d", apiURL, localPort),
				"application/json", nil,
			)
			if err != nil {
				return fmt.Errorf("replay failed (is a tunnel running?): %w", err)
			}
			defer resp.Body.Close()

			logger.Info("Replay completed (status %d)", resp.StatusCode)
			return nil
		},
	}

	cmd.Flags().IntVarP(&localPort, "port", "p", 3000, "Local server port")
	cmd.Flags().BoolVar(&exportCurl, "curl", false, "Export as curl command")
	return cmd
}
