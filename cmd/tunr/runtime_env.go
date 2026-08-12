package main

import "os"

// relayFlag holds the value of the global --relay flag (see root.go).
//
// Self-hosting used to be documented as `tunr share --port 3000 --relay <url>`
// while the only real knob was the TUNR_RELAY_URL environment variable, so the
// documented command failed with "unknown flag". The flag now exists and takes
// precedence over the env var, which stays supported for CI and daemons.
var relayFlag string

func relayURL() string {
	if relayFlag != "" {
		return relayFlag
	}
	if v := os.Getenv("TUNR_RELAY_URL"); v != "" {
		return v
	}
	return "https://relay.tunr.sh"
}

func appURL() string {
	if v := os.Getenv("TUNR_APP_URL"); v != "" {
		return v
	}
	return "https://app.tunr.sh"
}

func updateRepo() string {
	if v := os.Getenv("TUNR_UPDATE_REPO"); v != "" {
		return v
	}
	return "ahmetvural79/tunr"
}

func updateBaseURL() string {
	if v := os.Getenv("TUNR_UPDATE_BASE_URL"); v != "" {
		return v
	}
	return "https://github.com"
}
