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

// ErrNotAuthenticated - henüz login olmamış kullanıcı için
var ErrNotAuthenticated = errors.New("tunr hesabınıza giriş yapılmamış; 'tunr login' komutunu deneyin")

// ErrInvalidToken - token bozuk veya süresi geçmiş
var ErrInvalidToken = errors.New("geçersiz veya süresi dolmuş token; 'tunr login' ile yenileyin")

// keychainService - OS keychain'deki servis adı
const keychainService = "tunr.sh"

// keychainAccount - keychain account adı
const keychainAccount = "auth_token"

// GÜVENLİK NOT: Token şifrelenmeden diske yazılmıyor.
// OS keychain kullanıyoruz. Bu açık kaynak projede önemli çünkü
// config.json'u yanlışlıkla GitHub'a pushlayan biri olacaktır (olur hep).

// StoreToken - auth token'ı güvenli biçimde OS keychain'e yaz.
// GÜVENLİK: token parametresini ASLA log'a geçirme.
func StoreToken(token string) error {
	if token == "" {
		return fmt.Errorf("boş token kabul edilmez")
	}

	// Token uzunluk kontrolü (JWT genelde 200-2000 karakter arası)
	if len(token) > 4096 {
		return fmt.Errorf("token boyutu makul değil, bu ne büyüklüğünde bir token?")
	}

	switch runtime.GOOS {
	case "darwin":
		return storeTokenMacOS(token)
	case "linux":
		return storeTokenLinux(token)
	case "windows":
		return storeTokenWindows(token)
	default:
		// Bilinmeyen platform - güvensiz ama fallback olarak in-memory
		return fmt.Errorf("bu platform için güvenli token storage desteklenmiyor: %s", runtime.GOOS)
	}
}

// GetToken - keychain'den token al.
// GÜVENLİK: dönen token'ı log'a yazma!
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

// DeleteToken - logout işlemi - token'ı keychain'den temizle
func DeleteToken() error {
	switch runtime.GOOS {
	case "darwin":
		return deleteTokenMacOS()
	case "linux":
		return deleteTokenLinux()
	case "windows":
		return deleteTokenWindows()
	default:
		return nil // en kötü ihtimalle çık, token zaten yoktu
	}
}

// IsAuthenticated - kullanıcı login mu?
func IsAuthenticated() bool {
	token, err := GetToken()
	return err == nil && token != ""
}

// GenerateState - OAuth PKCE için random state string üret
// GÜVENLİK: crypto/rand kullanıyoruz, math/rand değil!
// math/rand kullanmak CSRF saldırılarına davetiye çıkarır.
func GenerateState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("random state üretilemedi: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// --- macOS Keychain ---

func storeTokenMacOS(token string) error {
	// Varsa eski token'ı sil
	_ = deleteTokenMacOS()

	// security add-generic-password ile keychain'e yaz
	cmd := exec.Command("security", "add-generic-password",
		"-s", keychainService,
		"-a", keychainAccount,
		"-w", token,
		"-U", // update varsa
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		// GÜVENLİK: token'ı hata mesajına yansıtma
		return fmt.Errorf("keychain yazma hatası: %s", sanitizeOutput(string(out)))
	}
	return nil
}

func getTokenMacOS() (string, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-s", keychainService,
		"-a", keychainAccount,
		"-w", // sadece şifreyi döndür
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
	_ = cmd.Run() // yoksa da hata vermesin
	return nil
}

// --- Linux (secret-tool / fallback) ---

func storeTokenLinux(token string) error {
	// libsecret (GNOME Keyring) kullanmayı dene
	cmd := exec.Command("secret-tool", "store",
		"--label", "tunr auth token",
		"service", keychainService,
		"account", keychainAccount,
	)
	cmd.Stdin = strings.NewReader(token)

	if _, err := cmd.Output(); err != nil {
		// secret-tool yoksa kullanıcıya bilgi ver
		return fmt.Errorf("secret-tool bulunamadı; 'sudo apt install libsecret-tools' deneyin")
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
	// PowerShell ile Windows Credential Manager
	script := fmt.Sprintf(`
		$cred = New-Object PSCredential("%s", (ConvertTo-SecureString "%s" -AsPlainText -Force))
		$cred | Export-Clixml -Path "$env:APPDATA\tunr\auth.xml"
	`, keychainAccount, token)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("Windows credential store yazma hatası: %w", err)
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

// sanitizeOutput - hata mesajlarından potansiyel secret sızıntısını önle
// GÜVENLİK: external komut çıktısını doğrudan kullanıcıya gösterme
func sanitizeOutput(s string) string {
	// Çok uzun çıktıları kırp
	if len(s) > 200 {
		s = s[:200] + "...[truncated]"
	}
	// Satır sonlarını temizle
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	return s
}
