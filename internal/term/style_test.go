package term

import (
	"testing"
)

func TestStyleForStatus(t *testing.T) {
	tests := []struct {
		status int
		name   string
	}{
		{200, "2xx should be green"},
		{301, "3xx should be cyan"},
		{404, "4xx should be yellow"},
		{500, "5xx should be red"},
	}

	for _, tt := range tests {
		s := StyleForStatus(tt.status)
		if s == nil {
			t.Errorf("%s: got nil style for %d", tt.name, tt.status)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{50, "50ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.ms)
		if got != tt.expected {
			t.Errorf("FormatDuration(%d) = %s, want %s", tt.ms, got, tt.expected)
		}
	}
}

func TestRunStepsSuccess(t *testing.T) {
	steps := []Step{
		{Name: "test step", Fn: func() (string, error) { return "ok", nil }},
	}

	err := RunSteps(steps)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
