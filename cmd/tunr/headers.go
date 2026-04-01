package main

import "strings"

// splitHeaderSpec splits a "Header: value" string into [Header, value].
// Falls back to empty value if no colon is present.
func splitHeaderSpec(spec string) []string {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) == 2 {
		return []string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}
	}
	return []string{strings.TrimSpace(parts[0]), ""}
}
