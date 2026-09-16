package envelope

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func testEnvelope() Envelope {
	return Envelope{
		ID:             "env_1",
		Version:        "1",
		Issuer:         Party{Agent: "support-agent"},
		Recipient:      Party{Agent: "billing-agent"},
		Purpose:        "customer_support_refund",
		AllowedActions: []string{"orders.read"},
		Delegation:     Delegation{MaxDepth: 2},
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
		PolicyVersion:  "v1",
	}
}

func TestSignAndVerifyRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	e := testEnvelope()

	sig, err := Sign(e, priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	e.Signature = sig

	if err := Verify(e, pub); err != nil {
		t.Fatalf("expected valid signature, got error: %v", err)
	}
}

func TestVerifyFailsOnTamperedField(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	e := testEnvelope()
	sig, err := Sign(e, priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	e.Signature = sig

	// Tamper with a field after signing, without resigning.
	e.AllowedActions = append(e.AllowedActions, "orders.delete")

	if err := Verify(e, pub); err == nil {
		t.Fatalf("expected verification to fail on tampered envelope")
	}
}

func TestVerifyFailsOnMissingSignature(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	e := testEnvelope()
	if err := Verify(e, pub); err == nil {
		t.Fatalf("expected verification to fail on missing signature")
	}
}

func TestMalformedKeyLengthsReturnErrors(t *testing.T) {
	for _, size := range []int{0, 1, ed25519.PrivateKeySize - 1, ed25519.PrivateKeySize + 1} {
		if _, err := Sign(testEnvelope(), make(ed25519.PrivateKey, size)); err == nil {
			t.Fatalf("accepted private key of size %d", size)
		}
	}
	e := testEnvelope()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e.Signature, err = Sign(e, priv)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1, ed25519.PublicKeySize - 1, ed25519.PublicKeySize + 1} {
		if err := Verify(e, make(ed25519.PublicKey, size)); err == nil {
			t.Fatalf("accepted public key of size %d", size)
		}
	}
}
