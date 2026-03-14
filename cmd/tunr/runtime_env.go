package main

import "os"

func relayURL() string {
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
