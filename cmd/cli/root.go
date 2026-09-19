package main

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "handoffguard",
	Short: "Counterseal: an authorization inheritance layer for multi-agent AI systems",
}
