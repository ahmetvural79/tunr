package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNotAuthenticated is what you get when you try to do things before saying hello
var ErrNotAuthenticated = errors.New("not logged in to tunr; try 'tunr login' first")

// ErrInvalidToken means the token is stale, corrupted, or just plain wrong
var ErrInvalidToken = errors.New("invalid or expired token; re-authenticate with 'tunr login'")

const keychainService = "tunr.sh"
const keychainAccount = "auth_token"

// SECURITY: Tokens never touch disk unencrypted — we use the OS keychain.
// This matters because someone WILL accidentally push config.json to GitHub.

// StoreToken persists the auth token in the OS keychain.
// SECURITY: NEVER pass the token parameter to any logger.
func StoreToken(token string) error {
	if token == "" {
		return fmt.Errorf("empty token is not acceptable")
	}

	// JWTs are typically 200-2000 chars; anything above 4K is suspicious
	if len(token) > 4096 {
		return fmt.Errorf("token is unreasonably large — what kind of token is this?")
	}

	switch runtime.GOOS {
	case "darwin":
		return storeTokenMacOS(token)
	case "linux":
		return storeTokenLinux(token)
	case "windows":
		return storeTokenWindows(token)
	default:
		return fmt.Errorf("no secure token storage available for platform: %s", runtime.GOOS)
	}
}

// GetToken retrieves the auth token from the OS keychain.
// SECURITY: Do NOT log the returned token.
func GetToken() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return getTokenMacOS()
	case "linux":
		return getTokenLinux()
	case "windows":
		return getTokenWindows()
	default:
		return "", ErrNotAuthenticated
	}
}

// DeleteToken wipes the token from the keychain on logout
func DeleteToken() error {
	switch runtime.GOOS {
	case "darwin":
		return deleteTokenMacOS()
	case "linux":
		return deleteTokenLinux()
	case "windows":
		return deleteTokenWindows()
	default:
		return nil // no keychain to clear — token was never stored anyway
	}
}

// IsAuthenticated checks if we have a valid token stashed away
func IsAuthenticated() bool {
	token, err := GetToken()
	return err == nil && token != ""
}

// GenerateState produces a cryptographic random state string for OAuth PKCE.
// SECURITY: We use crypto/rand, not math/rand — the latter is a CSRF footgun.
func GenerateState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// --- macOS Keychain ---

func storeTokenMacOS(token string) error {
	// Remove any stale token first
	_ = deleteTokenMacOS()

	// Write to macOS keychain via `security` CLI
	cmd := exec.Command("security", "add-generic-password",
		"-s", keychainService,
		"-a", keychainAccount,
		"-w", token,
		"-U", // update if exists
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		// SECURITY: Never reflect the token in error messages
		return fmt.Errorf("failed to write to keychain: %s", sanitizeOutput(string(out)))
	}
	return nil
}

func getTokenMacOS() (string, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-s", keychainService,
		"-a", keychainAccount,
		"-w", // return password only
	)

	out, err := cmd.Output()
	if err != nil {
		return "", ErrNotAuthenticated
	}

	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", ErrNotAuthenticated
	}

	return token, nil
}

func deleteTokenMacOS() error {
	cmd := exec.Command("security", "delete-generic-password",
		"-s", keychainService,
		"-a", keychainAccount,
	)
	_ = cmd.Run() // best-effort: ignore errors if entry doesn't exist
	return nil
}

// --- Linux (secret-tool / fallback) ---

func storeTokenLinux(token string) error {
	// Try libsecret (GNOME Keyring) — the civilized Linux way
	cmd := exec.Command("secret-tool", "store",
		"--label", "tunr auth token",
		"service", keychainService,
		"account", keychainAccount,
	)
	cmd.Stdin = strings.NewReader(token)

	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("secret-tool not found; try 'sudo apt install libsecret-tools'")
	}
	return nil
}

func getTokenLinux() (string, error) {
	cmd := exec.Command("secret-tool", "lookup",
		"service", keychainService,
		"account", keychainAccount,
	)
	out, err := cmd.Output()
	if err != nil {
		return "", ErrNotAuthenticated
	}
	return strings.TrimSpace(string(out)), nil
}

func deleteTokenLinux() error {
	cmd := exec.Command("secret-tool", "clear",
		"service", keychainService,
		"account", keychainAccount,
	)
	_ = cmd.Run()
	return nil
}

// --- Windows (credential manager) ---

func storeTokenWindows(token string) error {
	// Stash credentials via PowerShell + Windows Credential Manager
	script := fmt.Sprintf(`
		$cred = New-Object PSCredential("%s", (ConvertTo-SecureString "%s" -AsPlainText -Force))
		$cred | Export-Clixml -Path "$env:APPDATA\tunr\auth.xml"
	`, keychainAccount, token)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("failed to write to Windows credential store: %w", err)
	}
	return nil
}

func getTokenWindows() (string, error) {
	script := `
		$cred = Import-Clixml -Path "$env:APPDATA\tunr\auth.xml"
		$cred.GetNetworkCredential().Password
	`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return "", ErrNotAuthenticated
	}
	return strings.TrimSpace(string(out)), nil
}

func deleteTokenWindows() error {
	script := `Remove-Item -Path "$env:APPDATA\tunr\auth.xml" -Force -ErrorAction SilentlyContinue`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	_ = cmd.Run()
	return nil
}

// sanitizeOutput prevents secrets from leaking into error messages.
// SECURITY: Never show raw external command output to the user.
func sanitizeOutput(s string) string {
	// Truncate absurdly long output — if it's this big, something is wrong
	if len(s) > 200 {
		s = s[:200] + "...[truncated]"
	}
	// Flatten newlines so logs stay on one line
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	return s
}
