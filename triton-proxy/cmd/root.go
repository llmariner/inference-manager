package main

import "github.com/spf13/cobra"

// rootCmd is the root of the command-line application.
var rootCmd = &cobra.Command{
	Use:   "triton-proxy",
	Short: "triton-proxy",
}

func init() {
	rootCmd.AddCommand(runCmd())
	rootCmd.SilenceUsage = true
}
