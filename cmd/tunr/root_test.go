package main

import (
	"testing"
)

func TestVersionIsSet(t *testing.T) {
	if Version == "" {
		t.Fatal("Version should not be empty")
	}
}

func TestRootCommandExists(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd should not be nil")
	}

	if rootCmd.Use != "tunr" {
		t.Errorf("expected 'tunr', got '%s'", rootCmd.Use)
	}
}

func TestRootHasSubcommands(t *testing.T) {
	expected := []string{
		"share", "start", "stop", "status", "logs",
		"doctor", "login", "logout", "version",
		"open", "replay", "mcp", "config",
		"update", "uninstall",
	}

	cmds := rootCmd.Commands()
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name()] = true
	}

	for _, exp := range expected {
		if !names[exp] {
			t.Errorf("missing subcommand: %s", exp)
		}
	}
}
