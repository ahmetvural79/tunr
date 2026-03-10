package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Config holds all tunr settings. Treat this struct with care.
type Config struct {
	Version int          `json:"version"`
	Auth    AuthConfig   `json:"auth"`
	Tunnel  TunnelConfig `json:"tunnel"`
	UI      UIConfig     `json:"ui"`
}

// AuthConfig stores non-secret auth metadata. Actual tokens live in the OS keychain.
type AuthConfig struct {
	Email           string `json:"email,omitempty"`
	KeychainService string `json:"keychain_service,omitempty"`
}

// TunnelConfig holds tunnel defaults — region "auto" picks the nearest edge
type TunnelConfig struct {
	Region          string `json:"region"`
	SubdomainPrefix string `json:"subdomain_prefix,omitempty"`
	TLSVerify       bool   `json:"tls_verify"`
}

// UIConfig controls terminal display — color defaults to true because life is too short for monochrome
type UIConfig struct {
	Color   bool `json:"color"`
	Verbose bool `json:"verbose"`
}

// DefaultConfig returns sensible factory defaults
func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		Tunnel: TunnelConfig{
			Region:    "auto",
			TLSVerify: true, // SECURITY: never set to false in production
		},
		UI: UIConfig{
			Color:   true,
			Verbose: false,
		},
	}
}

// ConfigDir returns the platform-appropriate config directory
func ConfigDir() (string, error) {
	var base string

	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Application Support/tunr
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not determine home directory: %w", err)
		}
		base = filepath.Join(home, "Library", "Application Support", "tunr")
	case "linux":
		// XDG compliance — we're good citizens
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			base = filepath.Join(xdg, "tunr")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
			return "", fmt.Errorf("could not determine home directory: %w", err)
		}
		base = filepath.Join(home, ".config", "tunr")
	}
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA env var not found — is this really Windows?")
		}
		base = filepath.Join(appData, "tunr")
	default:
		// Fallback: ~/.tunr — simple but it works
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not determine home directory: %w", err)
		}
		base = filepath.Join(home, ".tunr")
	}

	return base, nil
}

// configFilePath returns the full path to config.json
func configFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config from disk, falling back to defaults if none exists
func Load() (*Config, error) {
	path, err := configFilePath()
	if err != nil {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// First run — welcome aboard!
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("could not read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config parse error (hand-edited perhaps?): %w", err)
	}

	// TODO: version migration goes here

	return &cfg, nil
}

// Save persists config to disk.
// SECURITY: Tokens/secrets must never be passed through this function.
func (c *Config) Save() error {
	path, err := configFilePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("could not serialize config: %w", err)
	}

	// SECURITY: Owner-only read/write (0600). Reject any PR that weakens this.
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("could not write config: %w", err)
	}

	return nil
}
