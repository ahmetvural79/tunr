package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/ahmetvural79/tunr/internal/config"
	"github.com/ahmetvural79/tunr/internal/daemon"
	"github.com/ahmetvural79/tunr/internal/term"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostic checks",
		Long:  "Check that tunr is installed correctly and all dependencies are met.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

func runDoctor() error {
	fmt.Println()
	term.Cyan.Println("  tunr doctor")
	fmt.Printf("  %s\n", term.Divider(37))
	fmt.Println()

	passed, total := 0, 0

	check := func(name string, fn func() (string, bool)) {
		total++
		msg, good := fn()
		if good {
			passed++
			fmt.Printf("  %s  %-24s %s\n", term.CheckMark, name, term.Dim.Sprint(msg))
		} else {
			fmt.Printf("  %s  %-24s %s\n", term.CrossMark, name, term.Yellow.Sprint(msg))
		}
	}

	check("Internet", func() (string, bool) {
		c := &http.Client{Timeout: 3 * time.Second}
		resp, err := c.Get("https://1.1.1.1")
		if err != nil {
			return "no connection", false
		}
		resp.Body.Close()
		return "", true
	})

	check("Binary", func() (string, bool) {
		return fmt.Sprintf("v%s", Version), true
	})

	check("Daemon", func() (string, bool) {
		if daemon.IsRunning() {
			state, _ := daemon.ReadPID()
			if state != nil {
				return fmt.Sprintf("PID %d, %d tunnels", state.PID, len(state.Tunnels)), true
			}
			return "running", true
		}
		return "not running", false
	})

	check("Config", func() (string, bool) {
		if _, err := config.Load(); err != nil {
			return "not found", false
		}
		dir, _ := config.ConfigDir()
		return dir, true
	})

	check("Auth", func() (string, bool) {
		if auth.IsAuthenticated() {
			return "logged in", true
		}
		return "run 'tunr login'", false
	})

	check("Relay", func() (string, bool) {
		c := &http.Client{Timeout: 5 * time.Second}
		resp, err := c.Get("https://relay.tunr.sh/api/v1/health")
		if err != nil {
			return "unreachable", false
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return "ok", true
		}
		return fmt.Sprintf("HTTP %d", resp.StatusCode), false
	})

	check("Inspector", func() (string, bool) {
		c := &http.Client{Timeout: 1 * time.Second}
		resp, err := c.Get("http://localhost:19842/api/v1/health")
		if err != nil {
			return "not running", false
		}
		resp.Body.Close()
		return "running", true
	})

	fmt.Println()
	fmt.Printf("  %s\n", term.Divider(37))

	if passed == total {
		term.Green.Printf("  All %d checks passed\n\n", total)
	} else {
		term.Yellow.Printf("  %d/%d passed\n\n", passed, total)
		term.Dim.Println("  Help: https://tunr.sh/docs/troubleshooting")
		term.Dim.Println("  Issue: https://github.com/ahmetvural79/tunr/issues")
		fmt.Println()
	}

	return nil
}
