package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/fatih/color"
	"github.com/tunr-dev/tunr/internal/logger"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	var flush bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "logs [domain]",
		Short: "View HTTP request logs",
		Long: `Stream request logs from the running tunnel inspector.

Requires the inspector to be running (starts automatically with tunr share).`,
		Example: `  tunr logs
  tunr logs --follow
  tunr logs --json
  tunr logs --flush`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inspectorURL := "http://localhost:19842/api/v1/requests"

			if flush {
				resp, err := http.Post(inspectorURL+"?action=flush", "application/json", nil)
				if err != nil {
					return fmt.Errorf("failed to flush logs: %w", err)
				}
				resp.Body.Close()
				logger.Info("Logs flushed.")
				return nil
			}

			resp, err := http.Get(inspectorURL)
			if err != nil {
				return fmt.Errorf("inspector not reachable (is a tunnel running?): %w", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("failed to read logs: %w", err)
			}

			if jsonOutput {
				fmt.Println(string(body))
				return nil
			}

			var requests []struct {
				ID       string `json:"id"`
				Method   string `json:"method"`
				Path     string `json:"path"`
				Status   int    `json:"status"`
				Duration int64  `json:"duration_ms"`
				Time     string `json:"time"`
			}

			if err := json.Unmarshal(body, &requests); err != nil {
				fmt.Println(string(body))
				return nil
			}

			if len(requests) == 0 {
				logger.Info("No requests captured yet.")
				return nil
			}

			dim := color.New(color.FgHiBlack)

			for _, r := range requests {
				statusColor := color.New(color.FgGreen)
				if r.Status >= 400 {
					statusColor = color.New(color.FgYellow)
				}
				if r.Status >= 500 {
					statusColor = color.New(color.FgRed)
				}

				fmt.Printf("%s  %-7s %s  %s  %s\n",
					dim.Sprint(r.Time),
					r.Method,
					r.Path,
					statusColor.Sprintf("%d", r.Status),
					dim.Sprintf("%dms", r.Duration),
				)
			}

			if follow {
				logger.Info("Live follow mode: polling every 2s (Ctrl+C to stop)")
				for {
					time.Sleep(2 * time.Second)
					resp, err := http.Get(inspectorURL + "?since=latest")
					if err != nil {
						continue
					}
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()

					var newReqs []struct {
						Method   string `json:"method"`
						Path     string `json:"path"`
						Status   int    `json:"status"`
						Duration int64  `json:"duration_ms"`
						Time     string `json:"time"`
					}
					if json.Unmarshal(body, &newReqs) == nil {
						for _, r := range newReqs {
							statusColor := color.New(color.FgGreen)
							if r.Status >= 400 {
								statusColor = color.New(color.FgYellow)
							}
							if r.Status >= 500 {
								statusColor = color.New(color.FgRed)
							}
							fmt.Printf("%s  %-7s %s  %s  %s\n",
								dim.Sprint(r.Time),
								r.Method,
								r.Path,
								statusColor.Sprintf("%d", r.Status),
								dim.Sprintf("%dms", r.Duration),
							)
						}
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Live follow mode (tail -f style)")
	cmd.Flags().BoolVar(&flush, "flush", false, "Clear all captured logs")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}
