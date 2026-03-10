package main

import (
	"testing"
)

func TestLogsCmdFlags(t *testing.T) {
	cmd := newLogsCmd()

	flags := []string{"follow", "flush", "json"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("missing flag: --%s", f)
		}
	}
}
