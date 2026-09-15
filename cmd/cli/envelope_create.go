package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"handoffguard/internal/envelope"
	"handoffguard/internal/keys"
)

var (
	envelopeCreateIn  string
	envelopeCreateKey string
	envelopeCreateOut string
)

var envelopeCmd = &cobra.Command{
	Use:   "envelope",
	Short: "Create, sign, and verify obligation envelopes",
}

var envelopeCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Sign a draft envelope, producing a signed envelope",
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := os.ReadFile(envelopeCreateIn)
		if err != nil {
			return fmt.Errorf("read draft: %w", err)
		}

		var e envelope.Envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("parse draft: %w", err)
		}

		if e.ID == "" {
			e.ID = envelope.NewID()
		}
		if e.Version == "" {
			e.Version = "1"
		}

		if err := envelope.ValidateStructure(e); err != nil {
			return fmt.Errorf("draft is missing required fields: %w", err)
		}

		priv, err := keys.LoadPrivate(envelopeCreateKey)
		if err != nil {
			return fmt.Errorf("load signing key: %w", err)
		}

		sig, err := envelope.Sign(e, priv)
		if err != nil {
			return fmt.Errorf("sign envelope: %w", err)
		}
		e.Signature = sig

		out, err := json.MarshalIndent(e, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal signed envelope: %w", err)
		}

		if envelopeCreateOut == "" {
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}
		if err := os.WriteFile(envelopeCreateOut, out, 0644); err != nil {
			return fmt.Errorf("write signed envelope: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Wrote signed envelope to %s\n", envelopeCreateOut)
		return nil
	},
}

func init() {
	envelopeCreateCmd.Flags().StringVar(&envelopeCreateIn, "in", "", "path to draft envelope JSON (required)")
	envelopeCreateCmd.Flags().StringVar(&envelopeCreateKey, "key", "", "path to Ed25519 private key file (required)")
	envelopeCreateCmd.Flags().StringVar(&envelopeCreateOut, "out", "", "path to write signed envelope JSON (defaults to stdout)")
	_ = envelopeCreateCmd.MarkFlagRequired("in")
	_ = envelopeCreateCmd.MarkFlagRequired("key")

	envelopeCmd.AddCommand(envelopeCreateCmd)
	rootCmd.AddCommand(envelopeCmd)
}
