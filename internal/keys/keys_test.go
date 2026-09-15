package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
)

func TestGenerateThenLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "key")

	if err := Generate(prefix); err != nil {
		t.Fatalf("generate: %v", err)
	}

	priv, err := LoadPrivate(prefix + ".priv")
	if err != nil {
		t.Fatalf("load private: %v", err)
	}
	pub, err := LoadPublic(prefix + ".pub")
	if err != nil {
		t.Fatalf("load public: %v", err)
	}

	msg := []byte("test message")
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatalf("loaded keypair does not round trip sign/verify")
	}
}

func TestLoadPrivateRejectsWrongSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.priv")
	// Write a validly base64-encoded but wrong-length payload.
	badKey := make([]byte, 10)
	_, _ = rand.Read(badKey)
	if err := writeBase64(path, badKey); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := LoadPrivate(path); err == nil {
		t.Fatalf("expected error loading wrong-size private key")
	}
}
