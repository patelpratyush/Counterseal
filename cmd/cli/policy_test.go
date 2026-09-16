package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"handoffguard/internal/envelope"
	"handoffguard/internal/keys"
)

func cliPair(t *testing.T) (envelope.Envelope, envelope.Envelope) {
	t.Helper()
	raw, err := os.ReadFile("../../examples/refund-draft.json")
	if err != nil {
		t.Fatal(err)
	}
	var p, c envelope.Envelope
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	p.ID = "env_parent"
	c.ID = "env_child"
	c.ParentEnvelope = p.ID
	c.Issuer = p.Recipient
	c.Recipient = envelope.Party{Agent: "notification"}
	c.Delegation.CurrentDepth = 2
	return p, c
}

func writePolicyFixture(t *testing.T, path string, v envelope.Envelope) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func executePolicy(args ...string) (string, error) {
	command := newPolicyCmd()
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}

func TestPolicyCLI(t *testing.T) {
	p, c := cliPair(t)
	dir := t.TempDir()
	pp := filepath.Join(dir, "parent.json")
	cp := filepath.Join(dir, "child.json")
	writePolicyFixture(t, pp, p)
	writePolicyFixture(t, cp, c)
	out, err := executePolicy("diff", pp, cp)
	if err != nil || !strings.Contains(out, "DECISION: ALLOW") {
		t.Fatalf("%s %v", out, err)
	}
	if !strings.Contains(out, "signatures not checked") {
		t.Fatal("missing authentication scope")
	}
	out, err = executePolicy("validate", pp, "--format", "json")
	if err != nil || !json.Valid([]byte(out)) {
		t.Fatalf("%s %v", out, err)
	}
	c.AllowedActions = append(c.AllowedActions, "customers.delete")
	writePolicyFixture(t, cp, c)
	out, err = executePolicy("diff", pp, cp, "--format", "json")
	if err == nil || !json.Valid([]byte(out)) || !strings.Contains(out, "ACTION_EXPANDED") {
		t.Fatalf("%s %v", out, err)
	}
	for _, args := range [][]string{{"diff", pp}, {"diff", pp, cp, "--parent-key", "missing"}, {"diff", pp, cp, "--format", "xml"}, {"validate", pp, "--format", "xml"}, {"validate", "missing.json"}} {
		if _, err := executePolicy(args...); err == nil {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
}

func TestPolicyCLISignatures(t *testing.T) {
	p, c := cliPair(t)
	dir := t.TempDir()
	prefix := filepath.Join(dir, "key")
	if err := keys.Generate(prefix); err != nil {
		t.Fatal(err)
	}
	priv, err := keys.LoadPrivate(prefix + ".priv")
	if err != nil {
		t.Fatal(err)
	}
	p.Signature, err = envelope.Sign(p, priv)
	if err != nil {
		t.Fatal(err)
	}
	c.Signature, err = envelope.Sign(c, priv)
	if err != nil {
		t.Fatal(err)
	}
	pp, cp := filepath.Join(dir, "parent.json"), filepath.Join(dir, "child.json")
	writePolicyFixture(t, pp, p)
	writePolicyFixture(t, cp, c)
	args := []string{"diff", pp, cp, "--parent-key", prefix + ".pub", "--child-key", prefix + ".pub", "--format", "json"}
	out, err := executePolicy(args...)
	if err != nil || !strings.Contains(out, `"decision":"ALLOW"`) {
		t.Fatalf("%s %v", out, err)
	}
	// A narrower but unsigned change still must fail authentication.
	c.AllowedActions = nil
	writePolicyFixture(t, cp, c)
	out, err = executePolicy(args...)
	if err == nil || !strings.Contains(out, "SIGNATURE_INVALID") {
		t.Fatalf("%s %v", out, err)
	}
}

func TestReadPolicyRejectsAmbiguousInput(t *testing.T) {
	cases := []struct{ ext, input string }{
		{"json", `{"id":"one","id":"two"}`},
		{"json", `{"id":"one","ID":"two"}`},
		{"json", `{"issuer":{"agent":"one","agent":"two"}}`},
		{"json", `{"unexpected":true}`},
		{"json", `{"issuer":{"unexpected":true}}`},
		{"json", `{} {}`},
		{"json", `{"id":`},
		{"json", `[]`},
		{"yaml", "id: one\nid: two\n"},
		{"yaml", "id: one\n---\nid: two\n"},
		{"yaml", "unexpected: true\n"},
	}
	for _, tc := range cases {
		path := filepath.Join(t.TempDir(), "policy."+tc.ext)
		if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readPolicy(path); err == nil {
			t.Errorf("accepted %s", tc.input)
		}
	}
}

func TestReadPolicyYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	input := `id: env_yaml
version: "1"
issuer:
  agent: support
recipient:
  agent: billing
purpose: refund
policy_version: v1
expires_at: 2099-01-01T00:00:00Z
resources:
  orders: ["48319"]
allowed_actions: [orders.read]
delegation:
  max_depth: 2
  current_depth: 0
  may_expand_authority: false
approvals:
  - condition: refund.amount > 500
    required_role: manager
`
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := executePolicy("validate", path)
	if err != nil || !strings.Contains(out, "ALLOW") {
		t.Fatalf("%s %v", out, err)
	}
}
