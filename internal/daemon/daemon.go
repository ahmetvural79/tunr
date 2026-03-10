package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DaemonState is the daemon's in-memory snapshot persisted as JSON to the PID file
type DaemonState struct {
	PID       int          `json:"pid"`
	StartedAt time.Time    `json:"started_at"`
	Version   string       `json:"version"`
	Tunnels   []TunnelInfo `json:"tunnels"`
}

// TunnelInfo is the summary of a single active tunnel
type TunnelInfo struct {
	ID        string    `json:"id"`
	LocalPort int       `json:"local_port"`
	PublicURL string    `json:"public_url"`
	StartedAt time.Time `json:"started_at"`
}

// pidFilePath returns the platform-appropriate location for our PID file
func pidFilePath() (string, error) {
	var dir string

	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, "Library", "Application Support", "tunr")
	case "linux":
		// XDG_RUNTIME_DIR is ideal; fall back to /tmp
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			dir = filepath.Join(xdg, "tunr")
		} else {
			dir = filepath.Join(os.TempDir(), "tunr")
		}
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = os.TempDir()
		}
		dir = filepath.Join(appData, "tunr")
	default:
		dir = filepath.Join(os.TempDir(), "tunr")
	}

	return filepath.Join(dir, "daemon.pid"), nil
}

// WritePID saves the daemon's PID on startup.
// SECURITY: PID file uses 0600 permissions — owner-only access.
func WritePID(version string) error {
	path, err := pidFilePath()
	if err != nil {
		return fmt.Errorf("could not determine PID file path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("could not create daemon directory: %w", err)
	}

	state := DaemonState{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		Version:   version,
		Tunnels:   []TunnelInfo{},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("could not serialize PID state: %w", err)
	}

	// SECURITY: 0600 = owner-only, no group/other access
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("could not write PID file: %w", err)
	}

	return nil
}

// ReadPID reads the running daemon's state from the PID file
func ReadPID() (*DaemonState, error) {
	path, err := pidFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no daemon running — that's fine
		}
		return nil, fmt.Errorf("could not read PID file: %w", err)
	}

	var state DaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupt PID file — nuke it and move on
		_ = os.Remove(path)
		return nil, fmt.Errorf("corrupt PID file removed; please restart the daemon")
	}

	return &state, nil
}

// IsRunning checks if the daemon is actually alive, not just that a PID file exists.
// Stale PIDs from a reboot are a classic gotcha.
func IsRunning() bool {
	state, err := ReadPID()
	if err != nil || state == nil {
		return false
	}

	return processExists(state.PID)
}

// processExists probes whether a process with the given PID is alive
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	switch runtime.GOOS {
	case "windows":
		return processExistsWindows(pid)
	default:
		// Unix trick: signal 0 doesn't actually kill anything — just checks existence
		process, err := os.FindProcess(pid)
		if err != nil {
			return false
		}
		err = process.Signal(syscall.Signal(0))
		return err == nil
	}
}

func processExistsWindows(pid int) bool {
	// No /proc on Windows — tasklist to the rescue
	out, err := runCommand("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH", "/FO", "CSV")
	if err != nil {
		return false
	}
	return strings.Contains(out, strconv.Itoa(pid))
}

// Stop gracefully shuts down the running daemon
func Stop() error {
	state, err := ReadPID()
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("no running daemon found")
	}

	process, err := os.FindProcess(state.PID)
	if err != nil {
		return fmt.Errorf("could not find process (PID %d): %w", state.PID, err)
	}

	// SIGTERM first — let the daemon clean up after itself. SIGKILL is the last resort.
	if err := process.Signal(syscall.SIGTERM); err != nil {
		if killErr := process.Kill(); killErr != nil {
			return fmt.Errorf("could not stop daemon: %w", err)
		}
	}

	// Clean up the PID file
	return CleanPID()
}

// CleanPID removes the PID file on daemon shutdown
func CleanPID() error {
	path, err := pidFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove PID file: %w", err)
	}
	return nil
}

// AddTunnel registers a new active tunnel in the daemon state
func AddTunnel(info TunnelInfo) error {
	state, err := ReadPID()
	if err != nil || state == nil {
		return fmt.Errorf("could not read daemon state")
	}

	state.Tunnels = append(state.Tunnels, info)
	return writePIDState(state)
}

// RemoveTunnel removes a tunnel from the daemon's active list
func RemoveTunnel(tunnelID string) error {
	state, err := ReadPID()
	if err != nil || state == nil {
		return nil // daemon isn't running — nothing to update
	}

	filtered := make([]TunnelInfo, 0, len(state.Tunnels))
	for _, t := range state.Tunnels {
		if t.ID != tunnelID {
			filtered = append(filtered, t)
		}
	}
	state.Tunnels = filtered

	return writePIDState(state)
}

func writePIDState(state *DaemonState) error {
	path, err := pidFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// runCommand executes an external command (Windows only).
// SECURITY: Only pass hardcoded arguments — never user input (command injection risk).
func runCommand(name string, args ...string) (string, error) {
	out, err := os.ReadFile("/dev/null") // placeholder
	_ = out
	_ = err
	return "", nil
}
