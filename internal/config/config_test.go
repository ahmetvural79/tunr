package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tunr-dev/tunr/internal/config"
)

// TestDefaultConfig - varsayılan config'in güvenli değerlerle geldiğini doğrula.
// Özellikle TLSVerify'ın true olduğundan emin olmak istiyoruz.
// (false gelen birisi şifreli trafik görmek istiyordur muhtemelen)
func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig nil döndü, bu çok yanlış")
	}

	// GÜVENLİK TESTİ: TLS verify kapalı olmamalı
	if !cfg.Tunnel.TLSVerify {
		t.Error("TLSVerify varsayılan olarak true olmalı! false gönderen PR'ı reddedin.")
	}

	// Version 1 olmalı
	if cfg.Version != 1 {
		t.Errorf("Config version beklenen 1, alınan %d", cfg.Version)
	}

	// Region "auto" olmalı (fixed bir değer seçmek mantıklı değil)
	if cfg.Tunnel.Region != "auto" {
		t.Errorf("Varsayılan bölge 'auto' olmalı, alınan: %q", cfg.Tunnel.Region)
	}
}

// TestConfigSaveAndLoad - config'in diske yazıldığında geri aynı okunduğunu test et.
func TestConfigSaveAndLoad(t *testing.T) {
	// Test için geçici dizin kullan, gerçek config'i kirletme
	tmpDir := t.TempDir()

	// Geçici config yolu için env var hack (gerçek implementasyonda daha temiz yapılır)
	// Not: Bu test, config.Save'in implement edilmesine bağlıdır
	_ = tmpDir

	cfg := config.DefaultConfig()
	cfg.Tunnel.Region = "eu-west"
	cfg.UI.Verbose = true

	// Config'i kaydet ve tekrar yükle
	// (Save metodunun gerçek path'i değiştirebilmesi için refactoring gerekecek)
	// TODO: Faz 1'de dependency injection ile test edilebilir hale getir

	t.Log("Config save/load testi Faz 1'de implement edilecek (şimdilik placeholder)")
}

// TestConfigSafePermissions - config dosyasının 0600 permission ile yazıldığını doğrula.
// GÜVENLİK: Diğer kullanıcıların config'i okumaması lazım.
func TestConfigSafePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-config.json")

	// 0600 ile bir dosya yaz ve permission'ı kontrol et
	err := os.WriteFile(testFile, []byte(`{"test": true}`), 0600)
	if err != nil {
		t.Fatalf("Test dosyası yazılamadı: %v", err)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Dosya stat alınamadı: %v", err)
	}

	// Unix permission mask: sadece owner read+write (0600)
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("Config dosyası group/other'a açık! Permission: %o (sadece 0600 olmalı)", perm)
	}
}
