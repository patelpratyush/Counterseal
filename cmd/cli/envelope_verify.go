package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"handoffguard/internal/envelope"
	"handoffguard/internal/keys"
)

var (
	envelopeVerifyIn  string
	envelopeVerifyKey string
)

var envelopeVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify a signed envelope's structure, expiration, and signature",
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := os.ReadFile(envelopeVerifyIn)
		if err != nil {
			return fmt.Errorf("read envelope: %w", err)
		}

		var e envelope.Envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("parse envelope: %w", err)
		}

		pub, err := keys.LoadPublic(envelopeVerifyKey)
		if err != nil {
			return fmt.Errorf("load verification key: %w", err)
		}

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Envelope verification: %s\n", e.ID)

		valid := true

		if err := envelope.ValidateStructure(e); err != nil {
			fmt.Fprintf(out, "Required fields: INVALID (%v)\n", err)
			valid = false
		} else {
			fmt.Fprintln(out, "Required fields: VALID")
		}

		if err := envelope.ValidateExpiration(e, time.Now()); err != nil {
			fmt.Fprintf(out, "Expiration: INVALID (%v)\n", err)
			valid = false
		} else {
			fmt.Fprintf(out, "Expiration: VALID (expires %s)\n", e.ExpiresAt.Format(time.RFC3339))
		}

		if err := envelope.Verify(e, pub); err != nil {
			fmt.Fprintf(out, "Signature: INVALID (%v)\n", err)
			valid = false
		} else {
			fmt.Fprintln(out, "Signature: VALID")
		}

		if !valid {
			fmt.Fprintln(out, "STATUS: INVALID")
			return fmt.Errorf("envelope %s failed verification", e.ID)
		}
		fmt.Fprintln(out, "STATUS: VALID")
		return nil
	},
}

func init() {
	envelopeVerifyCmd.Flags().StringVar(&envelopeVerifyIn, "in", "", "path to signed envelope JSON (required)")
	envelopeVerifyCmd.Flags().StringVar(&envelopeVerifyKey, "key", "", "path to Ed25519 public key file (required)")
	_ = envelopeVerifyCmd.MarkFlagRequired("in")
	_ = envelopeVerifyCmd.MarkFlagRequired("key")

	envelopeCmd.AddCommand(envelopeVerifyCmd)
}
