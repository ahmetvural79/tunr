package main

import (
	"fmt"
	"os"

	"github.com/ahmetvural79/tunr/internal/tunnel"
)

var Version = "dev"
var BuildDate = ""
var Commit = ""

func init() {
	tunnel.Version = Version
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\nUnexpected error: %v\n", r)
			fmt.Fprintln(os.Stderr, "Please report: https://github.com/ahmetvural79/tunr/issues")
			os.Exit(1)
		}
	}()

	if err := Execute(); err != nil {
		os.Exit(1)
	}
}
