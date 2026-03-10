package main

import (
	"testing"
)

func TestUninstallCmdExists(t *testing.T) {
	cmd := newUninstallCmd()
	if cmd.Use != "uninstall" {
		t.Errorf("expected 'uninstall', got '%s'", cmd.Use)
	}
}
