package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"handoffguard/internal/envelope"
	"handoffguard/internal/keys"
)

func writeSignedEnvelope(t *testing.T, dir string, mutate func(e *envelope.Envelope)) (envPath, pubKeyPath string) {
	t.Helper()
	keyPrefix := filepath.Join(dir, "key")
	if err := keys.Generate(keyPrefix); err != nil {
		t.Fatalf("generate keys: %v", err)
	}
	priv, err := keys.LoadPrivate(keyPrefix + ".priv")
	if err != nil {
		t.Fatalf("load private: %v", err)
	}

	e := envelope.Envelope{
		ID:             envelope.NewID(),
		Version:        "1",
		Issuer:         envelope.Party{Agent: "support-agent"},
		Recipient:      envelope.Party{Agent: "billing-agent"},
		Purpose:        "customer_support_refund",
		AllowedActions: []string{"orders.read"},
		Delegation:     envelope.Delegation{MaxDepth: 2},
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
		PolicyVersion:  "v1",
	}
	sig, err := envelope.Sign(e, priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	e.Signature = sig

	if mutate != nil {
		mutate(&e)
	}

	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	envPath = filepath.Join(dir, "signed.json")
	if err := os.WriteFile(envPath, b, 0644); err != nil {
		t.Fatalf("write signed envelope: %v", err)
	}
	return envPath, keyPrefix + ".pub"
}

func TestEnvelopeVerifyCommandPassesForValidEnvelope(t *testing.T) {
	dir := t.TempDir()
	envPath, pubPath := writeSignedEnvelope(t, dir, nil)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"envelope", "verify", "--in", envPath, "--key", pubPath})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected verify to succeed, got error: %v, output: %s", err, out.String())
	}
	if !strings.Contains(out.String(), "STATUS: VALID") {
		t.Fatalf("expected STATUS: VALID in output, got: %s", out.String())
	}
}

func TestEnvelopeVerifyCommandFailsForTamperedEnvelope(t *testing.T) {
	dir := t.TempDir()
	envPath, pubPath := writeSignedEnvelope(t, dir, func(e *envelope.Envelope) {
		e.AllowedActions = append(e.AllowedActions, "orders.delete")
	})

	rootCmd.SetArgs([]string{"envelope", "verify", "--in", envPath, "--key", pubPath})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected verify to fail for tampered envelope")
	}
}

func TestEnvelopeVerifyCommandFailsForExpiredEnvelope(t *testing.T) {
	dir := t.TempDir()
	envPath, pubPath := writeSignedEnvelope(t, dir, func(e *envelope.Envelope) {
		e.ExpiresAt = time.Now().Add(-time.Hour).UTC()
	})

	// Note: mutating ExpiresAt after signing also invalidates the signature,
	// which is expected — both checks should be reported, and the command
	// should still fail.
	rootCmd.SetArgs([]string{"envelope", "verify", "--in", envPath, "--key", pubPath})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected verify to fail for expired envelope")
	}
}
