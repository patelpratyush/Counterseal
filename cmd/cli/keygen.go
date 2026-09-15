package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"handoffguard/internal/keys"
)

func defaultKeyPrefix() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "key"
	}
	return filepath.Join(home, ".handoffguard", "key")
}

var keygenOut string

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate an Ed25519 signing keypair",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.MkdirAll(filepath.Dir(keygenOut), 0700); err != nil {
			return fmt.Errorf("create key directory: %w", err)
		}
		if err := keys.Generate(keygenOut); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s.priv and %s.pub\n", keygenOut, keygenOut)
		return nil
	},
}

func init() {
	keygenCmd.Flags().StringVar(&keygenOut, "out", defaultKeyPrefix(), "key file prefix (writes <prefix>.priv and <prefix>.pub)")
	rootCmd.AddCommand(keygenCmd)
}
