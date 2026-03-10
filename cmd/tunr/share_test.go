package main

import (
	"testing"
)

func TestShareCmdRequiresPort(t *testing.T) {
	cmd := newShareCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Error("share should fail without --port")
	}
}

func TestShareCmdHasDomainFlag(t *testing.T) {
	cmd := newShareCmd()
	f := cmd.Flags().Lookup("domain")
	if f == nil {
		t.Error("share should have --domain flag")
	}
}

func TestShareCmdFlags(t *testing.T) {
	cmd := newShareCmd()

	flags := []string{
		"port", "subdomain", "domain", "no-open", "json",
		"demo", "freeze", "inject-widget", "auto-login",
		"password", "ttl", "expire", "route",
	}

	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("missing flag: --%s", f)
		}
	}
}
