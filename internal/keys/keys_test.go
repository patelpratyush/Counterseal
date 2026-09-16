package keys

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
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

func TestGeneratePreservesExistingFiles(t *testing.T) {
	for _, suffix := range []string{".priv", ".pub"} {
		t.Run(suffix, func(t *testing.T) {
			prefix := filepath.Join(t.TempDir(), "key")
			original := []byte("existing key material")
			if err := os.WriteFile(prefix+suffix, original, 0644); err != nil {
				t.Fatal(err)
			}
			if err := Generate(prefix); !errors.Is(err, os.ErrExist) {
				t.Fatalf("expected existing-file error, got %v", err)
			}
			got, err := os.ReadFile(prefix + suffix)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatal("existing key overwritten")
			}
			other := ".pub"
			if suffix == ".pub" {
				other = ".priv"
			}
			if _, err := os.Stat(prefix + other); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("partial keypair remains: %v", err)
			}
		})
	}
}

func TestGeneratePrivatePermissionsAndNoReplacement(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "key")
	if err := Generate(prefix); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(prefix + ".priv")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("private permissions: %v", info.Mode())
	}
	before, err := os.ReadFile(prefix + ".priv")
	if err != nil {
		t.Fatal(err)
	}
	if err := Generate(prefix); err == nil {
		t.Fatal("replaced existing pair")
	}
	after, err := os.ReadFile(prefix + ".priv")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("private key changed")
	}
}
