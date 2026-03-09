package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Config - tunr'nun beyni. Burayı dağıtma.
type Config struct {
	// Version - config formatı değişirse migration yaparız
	Version int `json:"version"`

	// Auth - token bilgileri. DİKKAT: asla log'a yazma!
	Auth AuthConfig `json:"auth"`

	// Tunnel - varsayılan tunnel ayarları
	Tunnel TunnelConfig `json:"tunnel"`

	// UI - display tercihleri
	UI UIConfig `json:"ui"`
}

// AuthConfig - güvenlik kritik alan. Token asla plaintext config'e yazılmaz.
// OS keychain kullanıyoruz (Faz 1'de implement edilecek).
// Şimdilik boş, ama placeholder olarak burada duruyor.
type AuthConfig struct {
	// Email - kullanıcı email adresi (token değil, bu güvenli)
	Email string `json:"email,omitempty"`
	// KeychainService - keychain'deki servis adı
	KeychainService string `json:"keychain_service,omitempty"`
}

// TunnelConfig - tunnel varsayılanları
type TunnelConfig struct {
	// Region - tercih edilen bölge (auto = en yakın edge)
	Region string `json:"region"`
	// SubdomainPrefix - özel subdomain prefix (pro feature)
	SubdomainPrefix string `json:"subdomain_prefix,omitempty"`
	// TLSVerify - prod'da her zaman true olmalı
	TLSVerify bool `json:"tls_verify"`
}

// UIConfig - terminal UI tercihleri
type UIConfig struct {
	// Color - renkli output? Tabii ki evet. Bu soru bile komikti.
	Color bool `json:"color"`
	// Verbose - debug log'ları göster
	Verbose bool `json:"verbose"`
}

// DefaultConfig - fabrika ayarları gibi düşün
func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		Tunnel: TunnelConfig{
			Region:    "auto",
			TLSVerify: true, // GÜVENLIK: production'da asla false yapma
		},
		UI: UIConfig{
			Color:   true,
			Verbose: false,
		},
	}
}

// ConfigDir - tunr config klasörü nerede?
// Her OS kendine göre mantıklı bir yere koyar.
func ConfigDir() (string, error) {
	var base string

	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Application Support/tunr
		// (Homebrew ekosistemiyle uyumluluk için)
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir bulunamadı: %w", err)
		}
		base = filepath.Join(home, "Library", "Application Support", "tunr")
	case "linux":
		// Linux: XDG standartlarına uyalım
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			base = filepath.Join(xdg, "tunr")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("home dir bulunamadı: %w", err)
			}
			base = filepath.Join(home, ".config", "tunr")
		}
	case "windows":
		// Windows: %APPDATA%\tunr
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA env var bulunamadı; Windows mu bu?")
		}
		base = filepath.Join(appData, "tunr")
	default:
		// Diğer platformlar: ~/.tunr (basit ama işe yarar)
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir bulunamadı: %w", err)
		}
		base = filepath.Join(home, ".tunr")
	}

	return base, nil
}

// configFilePath - config.json'ın tam yolu
func configFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load - config'i diskten yükle. Yoksa default ile başla.
func Load() (*Config, error) {
	path, err := configFilePath()
	if err != nil {
		return DefaultConfig(), nil // config yoksa default kullan
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// İlk kez çalışıyor, welcome!
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("config okunamadı: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config parse hatası (belki elle düzenlediniz?): %w", err)
	}

	// Version migration buraya gelecek (Faz 1'de)
	// Şimdilik naive olarak yükle

	return &cfg, nil
}

// Save - config'i diske yaz.
// GÜVENLİK: token/secret bu fonksiyona asla geçmemeli.
func (c *Config) Save() error {
	path, err := configFilePath()
	if err != nil {
		return err
	}

	// Klasörü oluştur (yoksa)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil { // 0700 = sadece sahip okuyabilir
		return fmt.Errorf("config klasörü oluşturulamadı: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config serialize edilemedi: %w", err)
	}

	// GÜVENLİK: config dosyası sadece sahibi tarafından okunabilir (0600)
	// chmod 644 yapan PR'ı reddedin lütfen
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("config yazılamadı: %w", err)
	}

	return nil
}
