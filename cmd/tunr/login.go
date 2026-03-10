package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/fatih/color"
	"github.com/tunr-dev/tunr/internal/auth"
	"github.com/tunr-dev/tunr/internal/logger"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to your tunr.sh account",
		Long: `Authenticate with tunr.sh using your browser.
Opens a login page and waits for the callback with your auth token.

Required for Pro features (custom subdomains, custom domains, etc.)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if auth.IsAuthenticated() {
				logger.Info("Already logged in. Use 'tunr logout' first to switch accounts.")
				return nil
			}

			state, err := auth.GenerateState()
			if err != nil {
				return fmt.Errorf("failed to generate auth state: %w", err)
			}

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return fmt.Errorf("failed to start callback server: %w", err)
			}
			callbackPort := listener.Addr().(*net.TCPAddr).Port

			tokenCh := make(chan string, 1)
			errCh := make(chan error, 1)

			mux := http.NewServeMux()
			mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
				token := r.URL.Query().Get("token")
				returnedState := r.URL.Query().Get("state")

				if returnedState != state {
					errCh <- fmt.Errorf("state mismatch — possible CSRF attack")
					http.Error(w, "Authentication failed", http.StatusForbidden)
					return
				}

				if token == "" {
					errCh <- fmt.Errorf("no token received")
					http.Error(w, "No token", http.StatusBadRequest)
					return
				}

				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<html><body style="font-family:system-ui;text-align:center;padding:80px">
					<h2>Logged in to tunr!</h2>
					<p>You can close this tab and return to the terminal.</p>
				</body></html>`)

				tokenCh <- token
			})

			srv := &http.Server{Handler: mux}
			go func() { _ = srv.Serve(listener) }()

			loginURL := fmt.Sprintf("https://app.tunr.sh/auth/cli?state=%s&callback=http://localhost:%d/callback",
				state, callbackPort)

			dim := color.New(color.FgHiBlack)
			fmt.Println()
			logger.Info("Opening browser for login...")
			dim.Printf("  %s\n\n", loginURL)
			dim.Println("  Waiting for authentication...")
			fmt.Println()

			openBrowser(loginURL)

			select {
			case token := <-tokenCh:
				if err := auth.StoreToken(token); err != nil {
					return fmt.Errorf("failed to save token: %w", err)
				}
				logger.Info("Logged in successfully! Token stored in OS keychain.")

			case err := <-errCh:
				return fmt.Errorf("login failed: %w", err)

			case <-time.After(5 * time.Minute):
				return fmt.Errorf("login timed out after 5 minutes")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(ctx)

			return nil
		},
	}
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out and remove stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.DeleteToken(); err != nil {
				return fmt.Errorf("logout failed: %w", err)
			}
			logger.Info("Logged out. Token removed from keychain.")
			return nil
		},
	}
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = exec.Command("open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		logger.Warn("Could not open browser: %v", err)
		logger.Info("Open this URL manually: %s", url)
	}
}
