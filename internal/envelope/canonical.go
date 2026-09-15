package envelope

import "encoding/json"

// CanonicalBytes returns the deterministic JSON encoding of the envelope
// used for hashing and signing. The Signature field is always cleared
// before encoding, since the signature covers everything except itself.
//
// This relies on encoding/json's documented behavior: struct fields are
// emitted in declaration order, and map keys are sorted lexicographically.
// That is sufficient determinism for this struct shape without a separate
// canonicalization library.
func CanonicalBytes(e Envelope) ([]byte, error) {
	e.Signature = ""
	return json.Marshal(e)
}
