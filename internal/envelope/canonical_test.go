package envelope

import (
	"bytes"
	"testing"
	"time"
)

func TestCanonicalBytesDeterministicAcrossMapOrder(t *testing.T) {
	base := Envelope{
		ID:             "env_1",
		Version:        "1",
		Issuer:         Party{Agent: "a"},
		Recipient:      Party{Agent: "b"},
		Purpose:        "p",
		AllowedActions: []string{"x.read"},
		Delegation:     Delegation{MaxDepth: 1},
		ExpiresAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		PolicyVersion:  "v1",
	}

	e1 := base
	e1.Resources = map[string][]string{}
	e1.Resources["zebra"] = []string{"1"}
	e1.Resources["alpha"] = []string{"2"}

	e2 := base
	e2.Resources = map[string][]string{}
	e2.Resources["alpha"] = []string{"2"}
	e2.Resources["zebra"] = []string{"1"}

	b1, err := CanonicalBytes(e1)
	if err != nil {
		t.Fatalf("canonical e1: %v", err)
	}
	b2, err := CanonicalBytes(e2)
	if err != nil {
		t.Fatalf("canonical e2: %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("expected identical canonical bytes regardless of map insertion order:\n%s\nvs\n%s", b1, b2)
	}
}

func TestCanonicalBytesExcludesSignature(t *testing.T) {
	e := Envelope{ID: "env_1", Signature: "should-not-appear"}
	b, err := CanonicalBytes(e)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if bytes.Contains(b, []byte("should-not-appear")) {
		t.Fatalf("canonical bytes must not include signature field: %s", b)
	}
}
