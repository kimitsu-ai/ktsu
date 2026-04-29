package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/kimitsu-ai/ktsu/internal/version"
	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Version:\t%s\n", version.Version)
			fmt.Fprintf(tw, "Commit:\t%s\n", version.Commit)
			fmt.Fprintf(tw, "Built:\t%s\n", version.BuildDate)
			tw.Flush()
		},
	}
}
