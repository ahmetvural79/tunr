package main

import (
	"testing"
)

func TestDoctorCmdExists(t *testing.T) {
	cmd := newDoctorCmd()
	if cmd.Use != "doctor" {
		t.Errorf("expected 'doctor', got '%s'", cmd.Use)
	}
}
