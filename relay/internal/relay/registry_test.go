package relay_test

import (
	"sync"
	"testing"
	"time"

	"github.com/tunr-dev/tunr/relay/internal/relay"
)

// TestRegistryRegisterAndLookup — kayıt ve arama
func TestRegistryRegisterAndLookup(t *testing.T) {
	r := relay.NewRegistry()

	entry, err := r.Register("user-123", "")
	if err != nil {
		t.Fatalf("Register hata: %v", err)
	}
	if entry.ID == "" {
		t.Error("tunnel ID boş")
	}
	if entry.Subdomain == "" {
		t.Error("subdomain boş")
	}
	if !entry.IsAlive() {
		t.Error("yeni tunnel aktif olmalı")
	}

	// Subdomain ile bul
	found, ok := r.Lookup(entry.Subdomain)
	if !ok {
		t.Fatalf("Lookup(%q) başarısız", entry.Subdomain)
	}
	if found.ID != entry.ID {
		t.Errorf("Lookup yanlış tunnel döndürdü: %q != %q", found.ID, entry.ID)
	}
}

// TestRegistrySubdomainPreference — tercih edilen subdomain kullanılıyor mu?
func TestRegistrySubdomainPreference(t *testing.T) {
	r := relay.NewRegistry()

	entry, err := r.Register("user-456", "myapp")
	if err != nil {
		t.Fatalf("Register hata: %v", err)
	}
	if entry.Subdomain != "myapp" {
		t.Errorf("Subdomain = %q, myapp beklendi", entry.Subdomain)
	}

	// Aynı subdomaini tekrar almaya çalış
	_, err = r.Register("user-789", "myapp")
	if err == nil {
		t.Error("Duplicate subdomain için error beklendi, nil geldi")
	}
}

// TestRegistryUnregister — silme işlemi
func TestRegistryUnregister(t *testing.T) {
	r := relay.NewRegistry()

	entry, _ := r.Register("user-123", "")
	subdomain := entry.Subdomain

	r.Unregister(entry.ID)

	// Artık bulunamaz olmalı
	_, ok := r.Lookup(subdomain)
	if ok {
		t.Error("Unregister sonrası tunnel hala bulunuyor")
	}

	// Tunnel Done kanalı kapalı olmalı
	if entry.IsAlive() {
		t.Error("Unregister sonrası tunnel hala aktif görünüyor")
	}
}

// TestRegistryUnregisterTwice — çift silme panic etmemeli
func TestRegistryUnregisterTwice(t *testing.T) {
	r := relay.NewRegistry()
	entry, _ := r.Register("user-123", "")

	// Paniklemeden iki kez çağrılabilmeli
	r.Unregister(entry.ID)
	r.Unregister(entry.ID) // ikinci çağrı güvenli olmalı
}

// TestRegistryLookupNonexistent — var olmayan subdomain için false döner
func TestRegistryLookupNonexistent(t *testing.T) {
	r := relay.NewRegistry()

	_, ok := r.Lookup("nonexistent-subdomain-xyz")
	if ok {
		t.Error("var olmayan subdomain için true döndü")
	}
}

// TestRegistryPublicURL — public URL doğru formatlanıyor mu?
func TestRegistryPublicURL(t *testing.T) {
	r := relay.NewRegistry()
	entry, _ := r.Register("user-test", "myapp")

	url := entry.PublicURL("tunr.sh")
	expected := "https://myapp.tunr.sh"
	if url != expected {
		t.Errorf("PublicURL = %q, %q beklendi", url, expected)
	}
}

// TestRegistryStats — istatistikler güncelleniyor mu?
func TestRegistryStats(t *testing.T) {
	r := relay.NewRegistry()

	stats := r.Stats()
	if stats["active_tunnels"] != 0 {
		t.Errorf("başlangıçta %v aktif tunnel, 0 beklendi", stats["active_tunnels"])
	}

	entry1, _ := r.Register("user-1", "")
	entry2, _ := r.Register("user-2", "")

	stats = r.Stats()
	if stats["active_tunnels"] != 2 {
		t.Errorf("active_tunnels = %v, 2 beklendi", stats["active_tunnels"])
	}

	r.Unregister(entry1.ID)
	stats = r.Stats()
	if stats["active_tunnels"] != 1 {
		t.Errorf("silme sonrası active_tunnels = %v, 1 beklendi", stats["active_tunnels"])
	}

	r.Unregister(entry2.ID)
}

// TestRegistryConcurrency — eş zamanlı erişimde race condition yok mu?
// go test -race ile çalıştırılmalı
func TestRegistryConcurrency(t *testing.T) {
	r := relay.NewRegistry()
	var wg sync.WaitGroup

	// 50 goroutine aynı anda register/lookup/unregister yapsın
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			entry, err := r.Register("user-concurrent", "")
			if err != nil {
				return // subdomain çakışması olabilir, normal
			}

			// Biraz bekle
			time.Sleep(time.Millisecond)

			// Lookup
			_, _ = r.Lookup(entry.Subdomain)
			_ = r.Stats()

			r.Unregister(entry.ID)
		}(i)
	}

	wg.Wait()

	// Sonunda temiz olmalı
	stats := r.Stats()
	if stats["active_tunnels"] != 0 {
		t.Errorf("concurrency testi sonrası %v tunnel kaldı", stats["active_tunnels"])
	}
}

// TestRegistryPingUpdate — ping zamanı güncelleniyor mu?
func TestRegistryPingUpdate(t *testing.T) {
	r := relay.NewRegistry()
	entry, _ := r.Register("user-ping", "")

	// Ping güncelleme çağrısı hata vermemeli
	r.UpdatePing(entry.ID)

	// Var olmayan ID için de güvenli olmalı
	r.UpdatePing("nonexistent-id-should-not-panic")

	r.Unregister(entry.ID)
}
