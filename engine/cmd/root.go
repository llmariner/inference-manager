package main

import "github.com/spf13/cobra"

// rootCmd is the root of the command-line application.
var rootCmd = &cobra.Command{
	Use:   "engine",
	Short: "engine",
}

func init() {
	rootCmd.AddCommand(runCmd())
	rootCmd.AddCommand(pullCmd())
	rootCmd.SilenceUsage = true
}
