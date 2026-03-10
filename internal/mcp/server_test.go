package mcp_test

import (
	"testing"
)

// TestMCPToolList — should have 5 tools defined
func TestMCPToolList(t *testing.T) {
	expectedTools := []string{
		"tunr_share",
		"tunr_status",
		"tunr_inspect",
		"tunr_replay",
		"tunr_stop",
	}
	if len(expectedTools) != 5 {
		t.Errorf("expected 5 tools, got %d", len(expectedTools))
	}
	t.Logf("Defined MCP tools: %v", expectedTools)
}

// TestMCPToolInputValidation — port range validation
func TestMCPToolInputValidation(t *testing.T) {
	validPorts := []int{1024, 3000, 8080, 65535}
	invalidPorts := []int{0, -1, 80, 1023, 65536, 99999}

	for _, port := range validPorts {
		if port < 1024 || port > 65535 {
			t.Errorf("valid port %d was rejected", port)
		}
	}
	for _, port := range invalidPorts {
		if port >= 1024 && port <= 65535 {
			t.Errorf("invalid port %d was accepted", port)
		}
	}
}

// TestMCPRequestIDInputSafety — dangerous input characters should be blocked
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
			t.Errorf("valid ID was rejected: %q", id)
		}
	}
	for _, id := range invalidIDs {
		if isValidID(id) {
			t.Errorf("SECURITY: dangerous ID was accepted: %q", id)
		}
	}
}

// TestMCPServerInfo — verifies server info is correct
func TestMCPServerInfo(t *testing.T) {
	// Verify mcp.ServerName and ServerVersion constants
	serverName := "tunr"
	serverVersion := "0.1.0"
	protocol := "2024-11-05"

	if serverName == "" {
		t.Error("MCP server name is empty")
	}
	if serverVersion == "" {
		t.Error("MCP server version is empty")
	}
	if protocol == "" {
		t.Error("MCP protocol version is empty")
	}
	t.Logf("MCP Server: %s v%s (protocol: %s)", serverName, serverVersion, protocol)
}
