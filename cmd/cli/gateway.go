package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"handoffguard/internal/gateway"
)

func newGatewayCmd() *cobra.Command {
	var configPath, agent, envelopeID, apiURL, upstreamURL string
	var extraEnv []string
	var timeout time.Duration
	command := &cobra.Command{
		Use: "gateway [flags] -- UPSTREAM_COMMAND [ARGS...]", Short: "Serve authorized upstream MCP tools over stdio",
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if (upstreamURL == "") == (len(args) == 0) {
				return fmt.Errorf("provide either --upstream-url or an upstream command after --")
			}
			if upstreamURL != "" && len(extraEnv) > 0 {
				return fmt.Errorf("--upstream-env applies only to subprocess upstreams")
			}
			config, err := gateway.LoadConfig(configPath)
			if err != nil {
				return err
			}
			authorizer, err := gateway.NewHTTPAuthorizer(apiURL, os.Getenv("HANDOFFGUARD_API_TOKEN"))
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			var transport mcp.Transport
			if upstreamURL != "" {
				transport, err = gateway.HTTPTransport(upstreamURL, os.Getenv("HANDOFFGUARD_UPSTREAM_TOKEN"))
			} else {
				transport, err = gateway.CommandTransport(ctx, args, extraEnv, cmd.ErrOrStderr())
			}
			if err != nil {
				return err
			}
			startup, cancel := context.WithTimeout(ctx, 15*time.Second)
			session, err := gateway.NewUpstreamClient().Connect(startup, transport, nil)
			cancel()
			if err != nil {
				return fmt.Errorf("connect upstream: %w", err)
			}
			defer session.Close()
			logger := slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), nil))
			proxy, err := gateway.New(ctx, session, authorizer, config, gateway.Options{AgentID: agent, EnvelopeID: envelopeID, Timeout: timeout, Logger: logger})
			if err != nil {
				return err
			}
			logger.Info("gateway ready", "agent_id", agent, "envelope_id", envelopeID)
			return proxy.Run(ctx, &mcp.StdioTransport{MaxLineLength: 1 << 20})
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "trusted JSON tool mapping file (required)")
	command.Flags().StringVar(&agent, "agent", "", "fixed agent identity (required)")
	command.Flags().StringVar(&envelopeID, "envelope", "", "fixed envelope ID (required)")
	command.Flags().StringVar(&apiURL, "api-url", "http://127.0.0.1:8080", "Counterseal control API URL")
	command.Flags().StringVar(&upstreamURL, "upstream-url", "", "upstream Streamable HTTP MCP URL instead of a subprocess")
	command.Flags().StringArrayVar(&extraEnv, "upstream-env", nil, "additional environment variable NAME to pass upstream (repeatable)")
	command.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "total authorization and tool-call timeout")
	for _, name := range []string{"config", "agent", "envelope"} {
		_ = command.MarkFlagRequired(name)
	}
	return command
}
func init() { rootCmd.AddCommand(newGatewayCmd()) }
