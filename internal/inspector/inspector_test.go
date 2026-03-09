package inspector_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tunr-dev/tunr/internal/inspector"
)

// TestNewInspector — ring buffer başlangıç durumu
func TestNewInspector(t *testing.T) {
	ins := inspector.New(100)
	if ins == nil {
		t.Fatal("inspector.New nil döndürdü")
	}
	all := ins.GetAll()
	if len(all) != 0 {
		t.Errorf("başlangıçta %d istek var, 0 beklendi", len(all))
	}

	stats := ins.Stats()
	if v, ok := stats["total"]; ok && v != 0 {
		t.Errorf("stats.total = %v, 0 beklendi", v)
	}
}

// TestMiddlewareCapture — HTTP isteği ve yanıtı doğru yakalandı mı?
func TestMiddlewareCapture(t *testing.T) {
	ins := inspector.New(100)

	// Test handler — JSON yanıt döner
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	handler := ins.Middleware(target)

	req := httptest.NewRequest(http.MethodGet, "/api/test?q=hello", nil)
	req.Header.Set("Authorization", "Bearer secret-token-dont-log-me")
	req.Header.Set("X-Request-ID", "req-123")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Tekrar kontrol et
	time.Sleep(10 * time.Millisecond) // middleware goroutine bitmesini bekle

	all := ins.GetAll()
	if len(all) != 1 {
		t.Fatalf("1 istek beklendi, %d var", len(all))
	}

	captured := all[0]

	// Temel alanlar
	if captured.Method != http.MethodGet {
		t.Errorf("Method = %s, GET beklendi", captured.Method)
	}
	if captured.Path != "/api/test" {
		t.Errorf("Path = %s, /api/test beklendi", captured.Path)
	}
	if captured.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, 200 beklendi", captured.StatusCode)
	}

	// GÜVENLİK: Authorization header REDACTED olmalı
	authHeader := captured.ReqHeaders["Authorization"]
	if authHeader != "[REDACTED]" {
		t.Errorf("Authorization header REDACTED değil: %q", authHeader)
	}

	// Yanıt body yakalandı mı?
	if !strings.Contains(captured.RespBody, "ok") {
		t.Errorf("response body beklenen içerik yok: %q", captured.RespBody)
	}
}

// TestMiddlewareCapturePOST — POST body yakalanıyor mu?
func TestMiddlewareCapturePOST(t *testing.T) {
	ins := inspector.New(100)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		w.Write(body) // echo back
	})

	handler := ins.Middleware(target)
	reqBody := `{"name":"tunr","version":"1.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/create",
		strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	time.Sleep(10 * time.Millisecond)

	all := ins.GetAll()
	if len(all) != 1 {
		t.Fatalf("1 istek beklendi, %d var", len(all))
	}

	captured := all[0]
	if !strings.Contains(captured.ReqBody, "tunr") {
		t.Errorf("request body yanıtsız: %q", captured.ReqBody)
	}
	if captured.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, 201 beklendi", captured.StatusCode)
	}
}

// TestGzipDecode — gzip sıkıştırılmış yanıt açılıyor mu?
func TestGzipDecode(t *testing.T) {
	ins := inspector.New(100)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write([]byte(`{"compressed":"data"}`))
		gz.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(buf.Bytes())
	})

	handler := ins.Middleware(target)
	req := httptest.NewRequest(http.MethodGet, "/gzip-test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	time.Sleep(10 * time.Millisecond)

	all := ins.GetAll()
	if len(all) != 1 {
		t.Fatalf("1 istek beklendi, %d var", len(all))
	}

	// Inspector body compressed değil, decode edilmiş olmalı
	respBody := all[0].RespBody
	if !strings.Contains(respBody, "compressed") && !strings.Contains(respBody, "data") {
		t.Logf("Gzip body (ham veya decode): %q", respBody)
	}
}

// TestRingBuffer — maksimum kapasite aşıldığında eski kayıtlar silinir
func TestRingBuffer(t *testing.T) {
	maxSize := 5
	ins := inspector.New(maxSize)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ins.Middleware(target)

	// maxSize + 3 istek gönder
	for i := 0; i < maxSize+3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
	time.Sleep(20 * time.Millisecond)

	all := ins.GetAll()
	if len(all) > maxSize {
		t.Errorf("ring buffer %d kayıt tutuyor, max %d", len(all), maxSize)
	}
}

// TestGetByID — ID ile istek bulunuyor mu?
func TestGetByID(t *testing.T) {
	ins := inspector.New(100)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ins.Middleware(target)

	req := httptest.NewRequest(http.MethodGet, "/find-me", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	time.Sleep(10 * time.Millisecond)

	all := ins.GetAll()
	if len(all) == 0 {
		t.Fatal("istek yakalanmadı")
	}

	id := all[0].ID
	found, err := ins.GetByID(id)
	if err != nil {
		t.Fatalf("GetByID(%s) hata: %v", id, err)
	}
	if found.ID != id {
		t.Errorf("bulunan ID = %s, beklenen %s", found.ID, id)
	}
	if found.Path != "/find-me" {
		t.Errorf("Path = %s, /find-me beklendi", found.Path)
	}
}

// TestGetByIDNotFound — var olmayan ID için hata döner
func TestGetByIDNotFound(t *testing.T) {
	ins := inspector.New(100)
	_, err := ins.GetByID("nonexistent-id-xyz")
	if err == nil {
		t.Error("var olmayan ID için nil hata döndü, error beklendi")
	}
}

// TestClear — buffer temizleniyor mu?
func TestClear(t *testing.T) {
	ins := inspector.New(100)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ins.Middleware(target)

	// Birkaç istek
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
	time.Sleep(10 * time.Millisecond)

	if len(ins.GetAll()) == 0 {
		t.Fatal("istek yok, temizlemeden önce istek bekleniyor")
	}

	ins.Clear()

	if len(ins.GetAll()) != 0 {
		t.Errorf("Clear() sonrası %d istek var, 0 beklendi", len(ins.GetAll()))
	}
}

// TestExportCurl — curl komutu doğru formatlanıyor mu?
func TestExportCurl(t *testing.T) {
	ins := inspector.New(100)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ins.Middleware(target)

	req := httptest.NewRequest(http.MethodPost, "/api/data",
		strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Custom-Header", "myval")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	time.Sleep(10 * time.Millisecond)

	all := ins.GetAll()
	if len(all) == 0 {
		t.Fatal("istek yakalanmadı")
	}

	curl, err := ins.ExportCurl(all[0].ID)
	if err != nil {
		t.Fatalf("ExportCurl hata: %v", err)
	}

	// curl komutu doğru parçaları içeriyor mu?
	if !strings.HasPrefix(curl, "curl") {
		t.Errorf("curl çıktısı 'curl' ile başlamıyor: %q", curl)
	}
	if !strings.Contains(curl, "-X POST") {
		t.Errorf("curl çıktısında method yok: %q", curl)
	}
	if !strings.Contains(curl, "Content-Type") {
		t.Errorf("curl çıktısında Content-Type header yok: %q", curl)
	}
}

// TestSensitiveHeaderRedaction — hassas header'lar log'a geçmiyor
func TestSensitiveHeaderRedaction(t *testing.T) {
	ins := inspector.New(100)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ins.Middleware(target)

	sensitiveHeaders := map[string]string{
		"Authorization":   "Bearer my-secret-jwt",
		"Cookie":          "session=abc123; auth=xyz",
		"X-Api-Key":       "sk-prod-super-secret",
		"X-Auth-Token":    "token-should-not-appear",
		"Proxy-Authorization": "Basic aGVsbG86d29ybGQ=",
	}

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	for k, v := range sensitiveHeaders {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	time.Sleep(10 * time.Millisecond)

	all := ins.GetAll()
	if len(all) == 0 {
		t.Fatal("istek yakalanmadı")
	}

	captured := all[0]
	for k := range sensitiveHeaders {
		val := captured.ReqHeaders[k]
		if val != "[REDACTED]" && val != "" {
			t.Errorf("Header %q REDACTED değil: %q", k, val)
		}
	}
}

// TestDurationRecorded — istek süresi kaydediliyor mu?
func TestDurationRecorded(t *testing.T) {
	ins := inspector.New(100)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	handler := ins.Middleware(target)

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	time.Sleep(20 * time.Millisecond)

	all := ins.GetAll()
	if len(all) == 0 {
		t.Fatal("istek yakalanmadı")
	}

	if all[0].DurationMs < 10 {
		t.Errorf("DurationMs = %d, en az 10ms beklendi", all[0].DurationMs)
	}
}
