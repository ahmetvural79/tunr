package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/term"
	"github.com/spf13/cobra"
)

const systemdTemplate = `[Unit]
Description=tunr tunnel agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User={{.User}}
ExecStart={{.BinaryPath}} share --port {{.Port}}{{if .Subdomain}} --subdomain {{.Subdomain}}{{end}}
Restart=always
RestartSec=5
Environment=HOME={{.Home}}

[Install]
WantedBy=multi-user.target
`

const launchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>sh.tunr.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>share</string>
        <string>--port</string>
        <string>{{.Port}}</string>
{{- if .Subdomain}}
        <string>--subdomain</string>
        <string>{{.Subdomain}}</string>
{{- end}}
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.Home}}/Library/Logs/tunr.log</string>
    <key>StandardErrorPath</key>
    <string>{{.Home}}/Library/Logs/tunr.err</string>
</dict>
</plist>
`

type serviceConfig struct {
	User       string
	BinaryPath string
	Port       string
	Subdomain  string
	Home       string
}

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage tunr as a system service",
		Long:  "Install, uninstall, or check the status of tunr running as a system service (systemd on Linux, launchd on macOS).",
	}

	cmd.AddCommand(newServiceInstallCmd())
	cmd.AddCommand(newServiceUninstallCmd())
	cmd.AddCommand(newServiceStatusCmd())

	return cmd
}

func newServiceInstallCmd() *cobra.Command {
	var port string
	var subdomain string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install tunr as a system service (auto-start on boot)",
		Example: `  tunr service install --port 3000
  tunr service install --port 3000 --subdomain myapp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if port == "" {
				return fmt.Errorf("--port is required")
			}

			binaryPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cannot find tunr binary: %w", err)
			}
			binaryPath, _ = filepath.Abs(binaryPath)

			home, _ := os.UserHomeDir()
			user := os.Getenv("USER")
			if user == "" {
				user = os.Getenv("USERNAME")
			}

			cfg := serviceConfig{
				User:       user,
				BinaryPath: binaryPath,
				Port:       port,
				Subdomain:  subdomain,
				Home:       home,
			}

			switch runtime.GOOS {
			case "linux":
				return installSystemd(cfg)
			case "darwin":
				return installLaunchd(cfg)
			default:
				return fmt.Errorf("service install not supported on %s yet — use Task Scheduler on Windows", runtime.GOOS)
			}
		},
	}

	cmd.Flags().StringVar(&port, "port", "", "Local port to tunnel (required)")
	cmd.Flags().StringVar(&subdomain, "subdomain", "", "Custom subdomain (Pro)")

	return cmd
}

func installSystemd(cfg serviceConfig) error {
	tmpl := template.Must(template.New("systemd").Parse(systemdTemplate))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return err
	}

	servicePath := "/etc/systemd/system/tunr.service"
	if err := os.WriteFile(servicePath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("failed to write %s (run with sudo): %w", servicePath, err)
	}

	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "tunr"},
		{"systemctl", "start", "tunr"},
	}

	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			logger.Warn("$ %s: %s %v", strings.Join(c, " "), string(out), err)
		}
	}

	term.Green.Println("  ✓ tunr service installed and started")
	term.Dim.Println("  Check status: sudo systemctl status tunr")
	term.Dim.Println("  View logs:    sudo journalctl -u tunr -f")
	return nil
}

func installLaunchd(cfg serviceConfig) error {
	tmpl := template.Must(template.New("launchd").Parse(launchdTemplate))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return err
	}

	plistDir := filepath.Join(cfg.Home, "Library", "LaunchAgents")
	_ = os.MkdirAll(plistDir, 0755)
	plistPath := filepath.Join(plistDir, "sh.tunr.agent.plist")

	if err := os.WriteFile(plistPath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", plistPath, err)
	}

	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		logger.Warn("launchctl load: %s %v", string(out), err)
	}

	term.Green.Println("  ✓ tunr service installed (starts on login)")
	term.Dim.Printf("  Plist: %s\n", plistPath)
	term.Dim.Println("  View logs: tail -f ~/Library/Logs/tunr.log")
	return nil
}

func newServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove tunr system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch runtime.GOOS {
			case "linux":
				exec.Command("systemctl", "stop", "tunr").Run()
				exec.Command("systemctl", "disable", "tunr").Run()
				os.Remove("/etc/systemd/system/tunr.service")
				exec.Command("systemctl", "daemon-reload").Run()
				term.Green.Println("  ✓ tunr service removed")

			case "darwin":
				home, _ := os.UserHomeDir()
				plistPath := filepath.Join(home, "Library", "LaunchAgents", "sh.tunr.agent.plist")
				exec.Command("launchctl", "unload", plistPath).Run()
				os.Remove(plistPath)
				term.Green.Println("  ✓ tunr service removed")

			default:
				return fmt.Errorf("not supported on %s", runtime.GOOS)
			}
			return nil
		},
	}
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check tunr service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch runtime.GOOS {
			case "linux":
				out, err := exec.Command("systemctl", "status", "tunr").CombinedOutput()
				if err != nil {
					term.Dim.Println("  tunr service is not running")
					return nil
				}
				fmt.Println(string(out))

			case "darwin":
				out, _ := exec.Command("launchctl", "list", "sh.tunr.agent").CombinedOutput()
				if strings.Contains(string(out), "tunr") {
					term.Green.Println("  ● tunr service is running")
				} else {
					term.Dim.Println("  ○ tunr service is not running")
				}
				fmt.Println(string(out))

			default:
				return fmt.Errorf("not supported on %s", runtime.GOOS)
			}
			return nil
		},
	}
}
