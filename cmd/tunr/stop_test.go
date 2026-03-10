package main

import (
	"testing"
)

func TestStopCmdExists(t *testing.T) {
	cmd := newStopCmd()
	if cmd.Use != "stop" {
		t.Errorf("expected 'stop', got '%s'", cmd.Use)
	}
}
