package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
	"handoffguard/internal/envelope"
	"handoffguard/internal/keys"
	"handoffguard/internal/policy"
)

func newPolicyCmd() *cobra.Command {
	command := &cobra.Command{Use: "policy", Short: "Validate policies and check delegation"}
	var parentKey, childKey, format string
	diff := &cobra.Command{
		Use: "diff PARENT CHILD", Short: "Check whether a child envelope narrows its parent's authority",
		Long: "Compare JSON or YAML envelopes. Without public key flags this checks policy content only; it does not authenticate signatures.",
		Args: cobra.ExactArgs(2), SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return fmt.Errorf("format must be text or json")
			}
			if (parentKey == "") != (childKey == "") {
				return fmt.Errorf("supply both --parent-key and --child-key")
			}
			parent, err := readPolicy(args[0])
			if err != nil {
				return fmt.Errorf("parent: %w", err)
			}
			child, err := readPolicy(args[1])
			if err != nil {
				return fmt.Errorf("child: %w", err)
			}
			engine, err := policy.New()
			if err != nil {
				return err
			}
			result := engine.Diff(parent, child, time.Now())
			if parentKey != "" {
				for _, item := range []struct {
					name, path string
					value      envelope.Envelope
				}{{"parent", parentKey, parent}, {"child", childKey, child}} {
					pub, err := keys.LoadPublic(item.path)
					if err == nil {
						err = envelope.Verify(item.value, pub)
					}
					if err != nil {
						result.Decision = "DENY"
						result.Violations = append(result.Violations, policy.Violation{Code: "SIGNATURE_INVALID", Field: item.name + ".signature", Message: err.Error()})
					}
				}
			}
			mode := "policy content only; signatures not checked"
			if parentKey != "" {
				mode = "policy content and signatures"
			}
			return printPolicyResult(cmd, result, format, mode)
		},
	}
	diff.Flags().StringVar(&parentKey, "parent-key", "", "parent Ed25519 public key")
	diff.Flags().StringVar(&childKey, "child-key", "", "child Ed25519 public key")
	diff.Flags().StringVar(&format, "format", "text", "output format: text or json")
	var validateFormat string
	validate := &cobra.Command{
		Use: "validate ENVELOPE", Short: "Validate envelope structure, expiry, and CEL conditions", Args: cobra.ExactArgs(1), SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if validateFormat != "text" && validateFormat != "json" {
				return fmt.Errorf("format must be text or json")
			}
			v, err := readPolicy(args[0])
			if err != nil {
				return err
			}
			engine, err := policy.New()
			if err != nil {
				return err
			}
			return printPolicyResult(cmd, engine.Validate(v, time.Now()), validateFormat, "policy content only; signatures not checked")
		},
	}
	validate.Flags().StringVar(&validateFormat, "format", "text", "output format: text or json")
	command.AddCommand(diff, validate)
	return command
}

func printPolicyResult(cmd *cobra.Command, result policy.Result, format, mode string) error {
	if format == "json" {
		report := struct {
			policy.Result
			Checks string `json:"checks"`
		}{result, mode}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "POLICY CHECK (%s)\n", mode); err != nil {
			return err
		}
		for _, v := range result.Violations {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s [%s]: %s\n", v.Code, v.Field, v.Message); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "DECISION: %s\n", result.Decision); err != nil {
			return err
		}
	}
	if result.Decision == "DENY" {
		return fmt.Errorf("policy check denied")
	}
	return nil
}

// readPolicy rejects unknown fields, duplicate keys, and multiple documents.
func readPolicy(path string) (envelope.Envelope, error) {
	var v envelope.Envelope
	raw, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".yaml" || ext == ".yml" {
		var data any
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		if err := decoder.Decode(&data); err != nil {
			return v, err
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return v, fmt.Errorf("expected exactly one YAML document")
		}
		raw, err = json.Marshal(data)
		if err != nil {
			return v, err
		}
	}
	duplicateCheck := json.NewDecoder(bytes.NewReader(raw))
	duplicateCheck.UseNumber()
	if err := checkJSONValue(duplicateCheck); err != nil {
		return v, err
	}
	if _, err := duplicateCheck.Token(); err != io.EOF {
		return v, fmt.Errorf("expected exactly one JSON document")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&v); err != nil {
		return v, err
	}
	return v, nil
}

func checkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("expected object key")
			}
			// encoding/json matches struct fields case-insensitively. Reject aliases
			// such as issuer/Issuer too, so a signed field cannot silently be replaced.
			normalized := strings.ToLower(name)
			if seen[normalized] {
				return fmt.Errorf("duplicate JSON key %q", name)
			}
			seen[normalized] = true
			if err := checkJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := checkJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
	_, err = decoder.Token()
	return err
}

func init() { rootCmd.AddCommand(newPolicyCmd()) }
