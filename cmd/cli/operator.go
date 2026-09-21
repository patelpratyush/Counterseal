package main

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"handoffguard/internal/server"
	"handoffguard/internal/store"
	"os"
	"time"
)

func newOperatorCmd() *cobra.Command {
	root := &cobra.Command{Use: "operator", Short: "Manage individual operator accounts using host database access"}
	for _, operation := range []string{"create", "reset-password", "disable"} {
		var username, name, role, passwordEnv string
		var ifAbsent bool
		command := &cobra.Command{Use: operation, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			url := os.Getenv("HANDOFFGUARD_DATABASE_URL")
			if url == "" {
				return fmt.Errorf("HANDOFFGUARD_DATABASE_URL is required")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			db, err := store.Open(ctx, url)
			if err != nil {
				return err
			}
			defer db.Close()
			if err = db.Migrate(ctx); err != nil {
				return err
			}
			password := os.Getenv(passwordEnv)
			if operation == "create" {
				err = server.ProvisionOperator(ctx, db, username, name, role, password, ifAbsent)
			} else {
				err = server.ChangeOperator(ctx, db, username, password, operation == "disable")
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Operator account updated")
			return nil
		}}
		command.Flags().StringVar(&username, "username", "", "unique lowercase username (required)")
		_ = command.MarkFlagRequired("username")
		if operation != "disable" {
			command.Flags().StringVar(&passwordEnv, "password-env", "HANDOFFGUARD_OPERATOR_PASSWORD", "environment variable containing the password")
		}
		if operation == "create" {
			command.Flags().StringVar(&name, "name", "", "display name (required)")
			_ = command.MarkFlagRequired("name")
			command.Flags().StringVar(&role, "role", "viewer", "viewer or refund_manager")
			command.Flags().BoolVar(&ifAbsent, "if-absent", false, "leave an existing account unchanged")
		}
		root.AddCommand(command)
	}
	return root
}
func init() { rootCmd.AddCommand(newOperatorCmd()) }
