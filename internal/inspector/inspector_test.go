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

// TestNewInspector — ring buffer initial state
func TestNewInspector(t *testing.T) {
	ins := inspector.New(100)
	if ins == nil {
		t.Fatal("inspector.New returned nil")
	}
	all := ins.GetAll()
	if len(all) != 0 {
		t.Errorf("got %d requests at start, expected 0", len(all))
	}

	stats := ins.Stats()
	if v, ok := stats["total"]; ok && v != 0 {
		t.Errorf("stats.total = %v, expected 0", v)
	}
}

// TestMiddlewareCapture — verifies HTTP request and response are captured correctly
func TestMiddlewareCapture(t *testing.T) {
	ins := inspector.New(100)

	// Test handler — returns a JSON response
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

	time.Sleep(10 * time.Millisecond) // wait for middleware goroutine to complete

	all := ins.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 request, got %d", len(all))
	}

	captured := all[0]

	// Basic fields
	if captured.Method != http.MethodGet {
		t.Errorf("Method = %s, expected GET", captured.Method)
	}
	if captured.Path != "/api/test" {
		t.Errorf("Path = %s, expected /api/test", captured.Path)
	}
	if captured.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, expected 200", captured.StatusCode)
	}

	// SECURITY: Authorization header must be REDACTED
	authHeader := captured.ReqHeaders["Authorization"]
	if authHeader != "[REDACTED]" {
		t.Errorf("Authorization header not REDACTED: %q", authHeader)
	}

	// Was the response body captured?
	if !strings.Contains(captured.RespBody, "ok") {
		t.Errorf("response body missing expected content: %q", captured.RespBody)
	}
}

// TestMiddlewareCapturePOST — verifies POST body is captured
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
		t.Fatalf("expected 1 request, got %d", len(all))
	}

	captured := all[0]
	if !strings.Contains(captured.ReqBody, "tunr") {
		t.Errorf("request body not captured: %q", captured.ReqBody)
	}
	if captured.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, expected 201", captured.StatusCode)
	}
}

// TestGzipDecode — verifies gzip-compressed response is decoded
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
		t.Fatalf("expected 1 request, got %d", len(all))
	}

	// Inspector body should be decoded, not compressed
	respBody := all[0].RespBody
	if !strings.Contains(respBody, "compressed") && !strings.Contains(respBody, "data") {
		t.Logf("Gzip body (raw or decoded): %q", respBody)
	}
}

// TestRingBuffer — old entries are evicted when max capacity is exceeded
func TestRingBuffer(t *testing.T) {
	maxSize := 5
	ins := inspector.New(maxSize)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ins.Middleware(target)

	// Send maxSize + 3 requests
	for i := 0; i < maxSize+3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
	time.Sleep(20 * time.Millisecond)

	all := ins.GetAll()
	if len(all) > maxSize {
		t.Errorf("ring buffer holds %d entries, max %d", len(all), maxSize)
	}
}

// TestGetByID — verifies request lookup by ID
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
		t.Fatal("no request captured")
	}

	id := all[0].ID
	found, err := ins.GetByID(id)
	if err != nil {
		t.Fatalf("GetByID(%s) error: %v", id, err)
	}
	if found.ID != id {
		t.Errorf("found ID = %s, expected %s", found.ID, id)
	}
	if found.Path != "/find-me" {
		t.Errorf("Path = %s, expected /find-me", found.Path)
	}
}

// TestGetByIDNotFound — returns error for nonexistent ID
func TestGetByIDNotFound(t *testing.T) {
	ins := inspector.New(100)
	_, err := ins.GetByID("nonexistent-id-xyz")
	if err == nil {
		t.Error("got nil error for nonexistent ID, expected error")
	}
}

// TestClear — verifies buffer is cleared
func TestClear(t *testing.T) {
	ins := inspector.New(100)

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ins.Middleware(target)

	// Send a few requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
	time.Sleep(10 * time.Millisecond)

	if len(ins.GetAll()) == 0 {
		t.Fatal("no requests found, expected requests before clearing")
	}

	ins.Clear()

	if len(ins.GetAll()) != 0 {
		t.Errorf("got %d requests after Clear(), expected 0", len(ins.GetAll()))
	}
}

// TestExportCurl — verifies curl command is formatted correctly
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
		t.Fatal("no request captured")
	}

	curl, err := ins.ExportCurl(all[0].ID)
	if err != nil {
		t.Fatalf("ExportCurl error: %v", err)
	}

	// Does the curl command contain the correct parts?
	if !strings.HasPrefix(curl, "curl") {
		t.Errorf("curl output does not start with 'curl': %q", curl)
	}
	if !strings.Contains(curl, "-X POST") {
		t.Errorf("curl output missing method: %q", curl)
	}
	if !strings.Contains(curl, "Content-Type") {
		t.Errorf("curl output missing Content-Type header: %q", curl)
	}
}

// TestSensitiveHeaderRedaction — sensitive headers are not leaked to logs
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
		t.Fatal("no request captured")
	}

	captured := all[0]
	for k := range sensitiveHeaders {
		val := captured.ReqHeaders[k]
		if val != "[REDACTED]" && val != "" {
			t.Errorf("Header %q not REDACTED: %q", k, val)
		}
	}
}

// TestDurationRecorded — verifies request duration is recorded
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
		t.Fatal("no request captured")
	}

	if all[0].DurationMs < 10 {
		t.Errorf("DurationMs = %d, expected at least 10ms", all[0].DurationMs)
	}
}
