// Package keys generates and loads Ed25519 keypairs stored as
// base64-encoded raw key bytes on disk. This is sufficient for the CLI
// and local demo use case; a server-side key management story is a
// later sub-project, not part of this package.
package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

// Generate creates a new Ed25519 keypair and writes it to
// "<prefix>.priv" and "<prefix>.pub" as base64-encoded raw key bytes.
func Generate(prefix string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	if err := writeBase64(prefix+".priv", priv); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if err := writeBase64(prefix+".pub", pub); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}
	return nil
}

// LoadPrivate reads a base64-encoded Ed25519 private key from path.
func LoadPrivate(path string) (ed25519.PrivateKey, error) {
	key, err := readBase64(path)
	if err != nil {
		return nil, err
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size %d, want %d", len(key), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(key), nil
}

// LoadPublic reads a base64-encoded Ed25519 public key from path.
func LoadPublic(path string) (ed25519.PublicKey, error) {
	key, err := readBase64(path)
	if err != nil {
		return nil, err
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size %d, want %d", len(key), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(key), nil
}

func writeBase64(path string, b []byte) error {
	return os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(b)), 0600)
}

func readBase64(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	key, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return key, nil
}
