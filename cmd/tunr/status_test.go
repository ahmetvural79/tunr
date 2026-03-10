package main

import (
	"testing"
)

func TestStatusHasListAlias(t *testing.T) {
	cmd := newStatusCmd()
	if len(cmd.Aliases) == 0 {
		t.Fatal("status should have aliases")
	}
	if cmd.Aliases[0] != "list" {
		t.Errorf("expected 'list' alias, got '%s'", cmd.Aliases[0])
	}
}
