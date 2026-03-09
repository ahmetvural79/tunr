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

// DaemonState - daemon'ın şu anki durumu
// (PID file'a yazılan JSON — disk üzerindeki hafıza)
type DaemonState struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Version   string    `json:"version"`

	// Aktif tunnel bilgileri
	Tunnels []TunnelInfo `json:"tunnels"`
}

// TunnelInfo - tek bir aktif tunnel'ın özeti
type TunnelInfo struct {
	ID        string    `json:"id"`
	LocalPort int       `json:"local_port"`
	PublicURL string    `json:"public_url"`
	StartedAt time.Time `json:"started_at"`
}

// pidFilePath - PID dosyasının nereye yazılacağı
// Her platformda mantıklı bir yere koyuyoruz
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
		// XDG_RUNTIME_DIR ideal, yoksa /tmp'ye düş
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

// WritePID - daemon başladığında PID'i kaydet
// GÜVENLİK: PID file 0600 permission — sadece sahibi okur
func WritePID(version string) error {
	path, err := pidFilePath()
	if err != nil {
		return fmt.Errorf("PID dosya yolu belirlenemedi: %w", err)
	}

	// Klasörü oluştur
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("daemon dizini oluşturulamadı: %w", err)
	}

	state := DaemonState{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		Version:   version,
		Tunnels:   []TunnelInfo{},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("PID state serialize edilemedi: %w", err)
	}

	// GÜVENLİK: 0600 = sadece sahip okur, grup/diğer erişemez
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("PID dosyası yazılamadı: %w", err)
	}

	return nil
}

// ReadPID - çalışan daemon'ın PID'ini oku
func ReadPID() (*DaemonState, error) {
	path, err := pidFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // daemon çalışmıyor, bu normal
		}
		return nil, fmt.Errorf("PID dosyası okunamadı: %w", err)
	}

	var state DaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		// Bozuk PID file - temizle ve devam et
		_ = os.Remove(path)
		return nil, fmt.Errorf("bozuk PID dosyası temizlendi, yeniden başlatın")
	}

	return &state, nil
}

// IsRunning - daemon hâlâ çalışıyor mu?
// PID var olsa bile process'in gerçekten çalışıp çalışmadığını kontrol eder.
// (Sistem yeniden başladıysa PID artık geçerli olmayabilir)
func IsRunning() bool {
	state, err := ReadPID()
	if err != nil || state == nil {
		return false
	}

	return processExists(state.PID)
}

// processExists - verilen PID'e sahip process var mı?
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	switch runtime.GOOS {
	case "windows":
		return processExistsWindows(pid)
	default:
		// Unix-like: kill(pid, 0) — process'e sinyal göndermez, sadece varlığı kontrol eder
		process, err := os.FindProcess(pid)
		if err != nil {
			return false
		}
		err = process.Signal(syscall.Signal(0))
		return err == nil
	}
}

func processExistsWindows(pid int) bool {
	// Windows'ta /proc yok, farklı yöntem lazım
	// TaskList ile kontrol
	out, err := runCommand("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH", "/FO", "CSV")
	if err != nil {
		return false
	}
	return strings.Contains(out, strconv.Itoa(pid))
}

// Stop - çalışan daemon'ı durdur
func Stop() error {
	state, err := ReadPID()
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("çalışan daemon bulunamadı")
	}

	process, err := os.FindProcess(state.PID)
	if err != nil {
		return fmt.Errorf("process bulunamadı (PID %d): %w", state.PID, err)
	}

	// SIGTERM gönder - zarif kapatma için
	// SIGKILL değil! Daemon temizlik yapabilsin.
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// SIGTERM çalışmadıysa SIGKILL dene (son çare)
		if killErr := process.Kill(); killErr != nil {
			return fmt.Errorf("daemon durdurulamadı: %w", err)
		}
	}

	// PID dosyasını temizle
	return CleanPID()
}

// CleanPID - PID dosyasını sil (daemon kapanınca çağrılır)
func CleanPID() error {
	path, err := pidFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("PID dosyası silinemedi: %w", err)
	}
	return nil
}

// AddTunnel - aktif tunnel listesine ekle (dashboard için)
func AddTunnel(info TunnelInfo) error {
	state, err := ReadPID()
	if err != nil || state == nil {
		return fmt.Errorf("daemon state okunamadı")
	}

	state.Tunnels = append(state.Tunnels, info)
	return writePIDState(state)
}

// RemoveTunnel - tunnel listesinden çıkar
func RemoveTunnel(tunnelID string) error {
	state, err := ReadPID()
	if err != nil || state == nil {
		return nil // daemon zaten çalışmıyor, sorun değil
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
	return os.WriteFile(path, data, 0600) // güvenli permission yine
}

// runCommand - external komut çalıştır (sadece Windows için)
func runCommand(name string, args ...string) (string, error) {
	// Not: Bu fonksiyona kullanıcı input'u geçirilmemeli
	// (command injection riski var)
	// Şu an sadece sabit parametreler geçiyoruz, güvenli.
	out, err := os.ReadFile("/dev/null") // placeholder
	_ = out
	_ = err
	return "", nil
}
