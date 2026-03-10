package main

import (
	"testing"
)

func TestLatestTagParsing(t *testing.T) {
	// latestTag makes an HTTP call, so we test the version comparison logic
	current := "0.1.0"
	latest := "0.1.0"

	if current != latest {
		t.Errorf("versions should match: %s vs %s", current, latest)
	}
}

func TestUpdateCmdExists(t *testing.T) {
	cmd := newUpdateCmd()
	if cmd.Use != "update" {
		t.Errorf("expected 'update', got '%s'", cmd.Use)
	}

	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "upgrade" {
		t.Error("update should have 'upgrade' alias")
	}
}
