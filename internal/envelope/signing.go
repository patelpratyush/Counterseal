package envelope

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// Sign computes the Ed25519 signature over SHA-256(CanonicalBytes(e)) and
// returns it base64-encoded. The envelope's own Signature field is not
// part of the signed payload.
func Sign(e Envelope, priv ed25519.PrivateKey) (string, error) {
	b, err := CanonicalBytes(e)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	sig := ed25519.Sign(priv, hash[:])
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Verify checks that e.Signature is a valid Ed25519 signature over
// SHA-256(CanonicalBytes(e)) for the given public key.
func Verify(e Envelope, pub ed25519.PublicKey) error {
	if e.Signature == "" {
		return errors.New("envelope has no signature")
	}
	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}
	b, err := CanonicalBytes(e)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	if !ed25519.Verify(pub, hash[:], sig) {
		return errors.New("signature invalid")
	}
	return nil
}
