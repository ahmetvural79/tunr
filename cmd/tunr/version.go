package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print tunr version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("tunr v%s", Version)
			if Commit != "" {
				fmt.Printf(" (%s)", Commit[:min(7, len(Commit))])
			}
			if BuildDate != "" {
				fmt.Printf(" built %s", BuildDate)
			}
			fmt.Println()
		},
	}
}
