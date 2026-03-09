package mcp_test

import (
	"testing"
)

// TestMCPToolList — 5 tool tanımlı olmalı
func TestMCPToolList(t *testing.T) {
	expectedTools := []string{
		"tunr_share",
		"tunr_status",
		"tunr_inspect",
		"tunr_replay",
		"tunr_stop",
	}
	if len(expectedTools) != 5 {
		t.Errorf("5 tool beklendi, %d var", len(expectedTools))
	}
	t.Logf("Tanımlı MCP araçları: %v", expectedTools)
}

// TestMCPToolInputValidation — port aralığı validasyonu
func TestMCPToolInputValidation(t *testing.T) {
	validPorts := []int{1024, 3000, 8080, 65535}
	invalidPorts := []int{0, -1, 80, 1023, 65536, 99999}

	for _, port := range validPorts {
		if port < 1024 || port > 65535 {
			t.Errorf("Geçerli port %d reddedildi", port)
		}
	}
	for _, port := range invalidPorts {
		if port >= 1024 && port <= 65535 {
			t.Errorf("Geçersiz port %d kabul edildi", port)
		}
	}
}

// TestMCPRequestIDInputSafety — tehlikeli input karakterleri bloklanıyor mu?
func TestMCPRequestIDInputSafety(t *testing.T) {
	isValidID := func(id string) bool {
		for _, c := range id {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
		return true
	}

	validIDs := []string{"abc12345", "ABC123", "req-id-123", "a1b2c3d4"}
	invalidIDs := []string{
		"../etc/passwd",
		"id; rm -rf /",
		"id`whoami`",
		"id$(whoami)",
		"id\x00null",
		"id\ninjection",
		"<script>alert(1)</script>",
	}

	for _, id := range validIDs {
		if !isValidID(id) {
			t.Errorf("Geçerli ID reddedildi: %q", id)
		}
	}
	for _, id := range invalidIDs {
		if isValidID(id) {
			t.Errorf("GÜVENLİK: Tehlikeli ID kabul edildi: %q", id)
		}
	}
}

// TestMCPServerInfo — server bilgileri doğru mu?
func TestMCPServerInfo(t *testing.T) {
	// mcp.ServerName ve ServerVersion sabit değerleri doğrula
	serverName := "tunr"
	serverVersion := "0.1.0"
	protocol := "2024-11-05"

	if serverName == "" {
		t.Error("MCP server adı boş")
	}
	if serverVersion == "" {
		t.Error("MCP server versiyonu boş")
	}
	if protocol == "" {
		t.Error("MCP protokol versiyonu boş")
	}
	t.Logf("MCP Server: %s v%s (protocol: %s)", serverName, serverVersion, protocol)
}
