package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"handoffguard/internal/keys"
	"handoffguard/internal/server"
	"handoffguard/internal/store"
)

func newServerCmd() *cobra.Command {
	var addr, keyPath string
	var demoApprovals bool
	command := &cobra.Command{Use: "server", Short: "Run the PostgreSQL-backed control API", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		url, token := os.Getenv("HANDOFFGUARD_DATABASE_URL"), os.Getenv("HANDOFFGUARD_API_TOKEN")
		if url == "" || token == "" {
			return fmt.Errorf("HANDOFFGUARD_DATABASE_URL and HANDOFFGUARD_API_TOKEN are required")
		}
		key, err := keys.LoadPrivate(keyPath)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		startup, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		db, err := store.Open(startup, url)
		if err != nil {
			return err
		}
		defer db.Close()
		if err = db.Migrate(startup); err != nil {
			return err
		}
		api, err := server.New(startup, db, key, token)
		if err != nil {
			return err
		}
		api.AllowDemoApprovals = demoApprovals
		if os.Getenv("COUNTERSEAL_TRACE") == "1" {
			api.TraceLogger = slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), nil))
		}
		httpServer := &http.Server{Addr: addr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { done <- httpServer.Serve(listener) }()
		fmt.Fprintf(cmd.OutOrStdout(), "Counterseal listening on %s\n", listener.Addr().String())
		select {
		case err = <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err = httpServer.Shutdown(shutdown); err != nil {
				_ = httpServer.Close()
				return err
			}
			err = <-done
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	}}
	command.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "HTTP listen address")
	command.Flags().BoolVar(&demoApprovals, "allow-demo-approvals", false, "allow caller-asserted approvals in isolated test/demo environments only")
	command.Flags().StringVar(&keyPath, "key", "", "server Ed25519 private key file (required)")
	_ = command.MarkFlagRequired("key")
	return command
}
func init() { rootCmd.AddCommand(newServerCmd()) }
