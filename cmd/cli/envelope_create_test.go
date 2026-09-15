package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"handoffguard/internal/envelope"
	"handoffguard/internal/keys"
)

func TestEnvelopeCreateCommandProducesSignedEnvelope(t *testing.T) {
	dir := t.TempDir()

	keyPrefix := filepath.Join(dir, "key")
	if err := keys.Generate(keyPrefix); err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	draft := map[string]any{
		"version":         "1",
		"issuer":          map[string]any{"agent": "support-agent"},
		"recipient":       map[string]any{"agent": "billing-agent"},
		"purpose":         "customer_support_refund",
		"allowed_actions": []string{"orders.read"},
		"delegation":      map[string]any{"max_depth": 2},
		"expires_at":      "2099-01-01T00:00:00Z",
		"policy_version":  "v1",
	}
	draftBytes, err := json.Marshal(draft)
	if err != nil {
		t.Fatalf("marshal draft: %v", err)
	}
	draftPath := filepath.Join(dir, "draft.json")
	if err := os.WriteFile(draftPath, draftBytes, 0644); err != nil {
		t.Fatalf("write draft: %v", err)
	}
	outPath := filepath.Join(dir, "signed.json")

	rootCmd.SetArgs([]string{
		"envelope", "create",
		"--in", draftPath,
		"--key", keyPrefix + ".priv",
		"--out", outPath,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute envelope create: %v", err)
	}

	signedBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read signed output: %v", err)
	}
	var signed envelope.Envelope
	if err := json.Unmarshal(signedBytes, &signed); err != nil {
		t.Fatalf("unmarshal signed envelope: %v", err)
	}

	if signed.ID == "" {
		t.Fatalf("expected auto-generated id, got empty")
	}
	if signed.Signature == "" {
		t.Fatalf("expected signature to be set")
	}

	pub, err := keys.LoadPublic(keyPrefix + ".pub")
	if err != nil {
		t.Fatalf("load public key: %v", err)
	}
	if err := envelope.Verify(signed, pub); err != nil {
		t.Fatalf("expected valid signature on created envelope, got: %v", err)
	}
}
