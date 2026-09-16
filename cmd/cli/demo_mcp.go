package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"handoffguard/internal/demomcp"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{Use: "demo-mcp", Short: "Run a simulated, unguarded commerce MCP server over stdio", Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return demomcp.New().Run(ctx, &mcp.StdioTransport{MaxLineLength: 1 << 20})
	}})
}
