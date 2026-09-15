# Envelope Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Obligation Envelope data structure, deterministic canonical JSON, Ed25519 signing/verification, and a `handoffguard` CLI (`keygen`, `envelope create`, `envelope verify`).

**Architecture:** A pure Go package `internal/envelope` holds the struct, canonicalization, signing, and validation logic with no I/O. A separate `internal/keys` package handles keypair generation/loading (file I/O). `cmd/cli` is a thin cobra-based CLI wired on top of both packages — it does no crypto or JSON logic itself.

**Tech Stack:** Go 1.25+, `crypto/ed25519` + `crypto/sha256` (stdlib), `encoding/json` (stdlib — Go's `json.Marshal` already sorts map keys and preserves struct field order, so no external canonicalization library is needed), `github.com/spf13/cobra` for the CLI.

**Spec:** `docs/superpowers/specs/2026-09-15-envelope-core-design.md`

## Global Constraints

- No new dependency for cryptography or JSON canonicalization — stdlib only.
- `cobra` is the only new third-party dependency, used solely in `cmd/cli`.
- Envelope struct field names/JSON tags must exactly match PRD §7's example (`id`, `version`, `issuer`, `recipient`, `purpose`, `resources`, `allowed_actions`, `denied_actions`, `data_classes`, `approvals`, `delegation`, `expires_at`, `policy_version`, `parent_envelope`, `signature`).
- Module name: `handoffguard`. Binary name: `handoffguard`.
- Out of scope for this plan: delegation/monotonicity diff, CEL conditions, revocation, DB persistence — do not build these here.

---

### Task 1: Go module + Envelope model

**Files:**
- Create: `go.mod`
- Create: `internal/envelope/model.go`
- Test: `internal/envelope/model_test.go`

**Interfaces:**
- Produces: `envelope.Envelope`, `envelope.Party{Agent, Version}`, `envelope.Approval{Condition, RequiredRole}`, `envelope.Delegation{MaxDepth, CurrentDepth, MayExpandAuthority}`, `envelope.NewID() string`

- [ ] **Step 1: Init the Go module**

Run: `cd /Users/pratyush/projectidea && go mod init handoffguard`
Expected: creates `go.mod` with `module handoffguard` and a Go version line.

- [ ] **Step 2: Write the failing test for the model + NewID**

Create `internal/envelope/model_test.go`:

```go
package envelope

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEnvelopeJSONRoundTrip(t *testing.T) {
	e := Envelope{
		ID:      "env_test",
		Version: "1",
		Issuer:  Party{Agent: "support-agent", Version: "2.3"},
		Recipient: Party{Agent: "billing-agent"},
		Purpose: "customer_support_refund",
		Resources: map[string][]string{
			"orders":    {"48319"},
			"customers": {"cus_8291"},
		},
		AllowedActions: []string{"orders.read", "refunds.create"},
		DeniedActions:  []string{"customers.delete"},
		DataClasses:    []string{"customer_pii"},
		Approvals: []Approval{
			{Condition: "refund.amount > 500", RequiredRole: "refund_manager"},
		},
		Delegation: Delegation{MaxDepth: 2, CurrentDepth: 1, MayExpandAuthority: false},
		ExpiresAt:      time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC),
		PolicyVersion:  "refund-policy-v8",
		ParentEnvelope: "env_parent",
		Signature:      "sig123",
	}

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Envelope
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != e.ID || got.Issuer.Agent != e.Issuer.Agent || got.Delegation.MaxDepth != e.Delegation.MaxDepth {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, e)
	}
}

func TestNewIDHasPrefixAndIsUnique(t *testing.T) {
	a := NewID()
	b := NewID()
	if !strings.HasPrefix(a, "env_") {
		t.Fatalf("expected env_ prefix, got %q", a)
	}
	if a == b {
		t.Fatalf("expected unique ids, got two equal: %q", a)
	}
}
```

- [ ] **Step 2b: Run test to verify it fails**

Run: `go test ./internal/envelope/... -run TestEnvelopeJSONRoundTrip -v`
Expected: FAIL — package `envelope` / types don't exist yet.

- [ ] **Step 3: Write the model**

Create `internal/envelope/model.go`:

```go
package envelope

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Party identifies an agent by name and optional version, used for both
// the issuer and recipient of an envelope.
type Party struct {
	Agent   string `json:"agent"`
	Version string `json:"version,omitempty"`
}

// Approval describes a condition under which human sign-off is required
// before an action may proceed.
type Approval struct {
	Condition    string `json:"condition"`
	RequiredRole string `json:"required_role"`
}

// Delegation constrains how far and how loosely an envelope may be
// re-delegated to a further child.
type Delegation struct {
	MaxDepth           int  `json:"max_depth"`
	CurrentDepth       int  `json:"current_depth"`
	MayExpandAuthority bool `json:"may_expand_authority"`
}

// Envelope is a signed, tamper-evident record of delegated authority.
// Field order and JSON tags match PRD §7.
type Envelope struct {
	ID             string              `json:"id"`
	Version        string              `json:"version"`
	Issuer         Party               `json:"issuer"`
	Recipient      Party               `json:"recipient"`
	Purpose        string              `json:"purpose"`
	Resources      map[string][]string `json:"resources"`
	AllowedActions []string            `json:"allowed_actions"`
	DeniedActions  []string            `json:"denied_actions"`
	DataClasses    []string            `json:"data_classes"`
	Approvals      []Approval          `json:"approvals"`
	Delegation     Delegation          `json:"delegation"`
	ExpiresAt      time.Time           `json:"expires_at"`
	PolicyVersion  string              `json:"policy_version"`
	ParentEnvelope string              `json:"parent_envelope,omitempty"`
	Signature      string              `json:"signature,omitempty"`
}

// NewID generates a random envelope identifier prefixed "env_".
func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "env_" + hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/envelope/... -v`
Expected: PASS for both `TestEnvelopeJSONRoundTrip` and `TestNewIDHasPrefixAndIsUnique`.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/envelope/model.go internal/envelope/model_test.go
git commit -m "feat: add envelope model struct and NewID"
```

---

### Task 2: Canonical JSON serialization

**Files:**
- Create: `internal/envelope/canonical.go`
- Test: `internal/envelope/canonical_test.go`

**Interfaces:**
- Consumes: `envelope.Envelope` (Task 1)
- Produces: `envelope.CanonicalBytes(e Envelope) ([]byte, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/envelope/canonical_test.go`:

```go
package envelope

import (
	"bytes"
	"testing"
	"time"
)

func TestCanonicalBytesDeterministicAcrossMapOrder(t *testing.T) {
	base := Envelope{
		ID:      "env_1",
		Version: "1",
		Issuer:  Party{Agent: "a"},
		Recipient: Party{Agent: "b"},
		Purpose: "p",
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/envelope/... -run TestCanonicalBytes -v`
Expected: FAIL — `CanonicalBytes` undefined.

- [ ] **Step 3: Write the canonicalizer**

Create `internal/envelope/canonical.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/envelope/... -v`
Expected: PASS for all tests so far.

- [ ] **Step 5: Commit**

```bash
git add internal/envelope/canonical.go internal/envelope/canonical_test.go
git commit -m "feat: add deterministic canonical JSON encoding for envelopes"
```

---

### Task 3: Ed25519 signing and verification

**Files:**
- Create: `internal/envelope/signing.go`
- Test: `internal/envelope/signing_test.go`

**Interfaces:**
- Consumes: `envelope.CanonicalBytes` (Task 2)
- Produces: `envelope.Sign(e Envelope, priv ed25519.PrivateKey) (string, error)`, `envelope.Verify(e Envelope, pub ed25519.PublicKey) error`

- [ ] **Step 1: Write the failing test**

Create `internal/envelope/signing_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/envelope/... -run TestSignAndVerify -v`
Expected: FAIL — `Sign`/`Verify` undefined.

- [ ] **Step 3: Write signing.go**

Create `internal/envelope/signing.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/envelope/... -v`
Expected: PASS for all tests so far.

- [ ] **Step 5: Commit**

```bash
git add internal/envelope/signing.go internal/envelope/signing_test.go
git commit -m "feat: add Ed25519 sign/verify for envelopes"
```

---

### Task 4: Structural and expiration validation

**Files:**
- Create: `internal/envelope/validation.go`
- Test: `internal/envelope/validation_test.go`

**Interfaces:**
- Consumes: `envelope.Envelope` (Task 1)
- Produces: `envelope.ValidateStructure(e Envelope) error`, `envelope.ValidateExpiration(e Envelope, now time.Time) error`, sentinel errors `ErrMissingID`, `ErrMissingIssuer`, `ErrMissingRecipient`, `ErrMissingPurpose`, `ErrMissingPolicyVersion`, `ErrMissingExpiresAt`, `ErrExpired`

- [ ] **Step 1: Write the failing test**

Create `internal/envelope/validation_test.go`:

```go
package envelope

import (
	"errors"
	"testing"
	"time"
)

func validEnvelope() Envelope {
	return Envelope{
		ID:            "env_1",
		Version:       "1",
		Issuer:        Party{Agent: "support-agent"},
		Recipient:     Party{Agent: "billing-agent"},
		Purpose:       "customer_support_refund",
		PolicyVersion: "v1",
		ExpiresAt:     time.Now().Add(time.Hour).UTC(),
	}
}

func TestValidateStructurePasses(t *testing.T) {
	if err := ValidateStructure(validEnvelope()); err != nil {
		t.Fatalf("expected valid envelope to pass, got: %v", err)
	}
}

func TestValidateStructureCatchesMissingFields(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(e *Envelope)
		wantErr error
	}{
		{"missing id", func(e *Envelope) { e.ID = "" }, ErrMissingID},
		{"missing issuer", func(e *Envelope) { e.Issuer.Agent = "" }, ErrMissingIssuer},
		{"missing recipient", func(e *Envelope) { e.Recipient.Agent = "" }, ErrMissingRecipient},
		{"missing purpose", func(e *Envelope) { e.Purpose = "" }, ErrMissingPurpose},
		{"missing policy version", func(e *Envelope) { e.PolicyVersion = "" }, ErrMissingPolicyVersion},
		{"missing expires_at", func(e *Envelope) { e.ExpiresAt = time.Time{} }, ErrMissingExpiresAt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := validEnvelope()
			c.mutate(&e)
			err := ValidateStructure(e)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("expected %v, got %v", c.wantErr, err)
			}
		})
	}
}

func TestValidateExpirationRejectsPast(t *testing.T) {
	e := validEnvelope()
	e.ExpiresAt = time.Now().Add(-time.Hour).UTC()
	err := ValidateExpiration(e, time.Now())
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestValidateExpirationAcceptsFuture(t *testing.T) {
	e := validEnvelope()
	if err := ValidateExpiration(e, time.Now()); err != nil {
		t.Fatalf("expected no error for future expiry, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/envelope/... -run TestValidate -v`
Expected: FAIL — `ValidateStructure`/`ValidateExpiration` undefined.

- [ ] **Step 3: Write validation.go**

Create `internal/envelope/validation.go`:

```go
package envelope

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrMissingID            = errors.New("missing id")
	ErrMissingIssuer        = errors.New("missing issuer.agent")
	ErrMissingRecipient     = errors.New("missing recipient.agent")
	ErrMissingPurpose       = errors.New("missing purpose")
	ErrMissingPolicyVersion = errors.New("missing policy_version")
	ErrMissingExpiresAt     = errors.New("missing expires_at")
	ErrExpired              = errors.New("envelope expired")
)

// ValidateStructure checks that required fields are present. It does not
// check the signature or expiration — see Verify and ValidateExpiration.
func ValidateStructure(e Envelope) error {
	if e.ID == "" {
		return ErrMissingID
	}
	if e.Issuer.Agent == "" {
		return ErrMissingIssuer
	}
	if e.Recipient.Agent == "" {
		return ErrMissingRecipient
	}
	if e.Purpose == "" {
		return ErrMissingPurpose
	}
	if e.PolicyVersion == "" {
		return ErrMissingPolicyVersion
	}
	if e.ExpiresAt.IsZero() {
		return ErrMissingExpiresAt
	}
	return nil
}

// ValidateExpiration checks that e.ExpiresAt has not passed relative to now.
func ValidateExpiration(e Envelope, now time.Time) error {
	if e.ExpiresAt.Before(now) {
		return fmt.Errorf("%w: expired at %s", ErrExpired, e.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/envelope/... -v`
Expected: PASS for all tests so far.

- [ ] **Step 5: Commit**

```bash
git add internal/envelope/validation.go internal/envelope/validation_test.go
git commit -m "feat: add envelope structural and expiration validation"
```

---

### Task 5: Keypair generation and loading

**Files:**
- Create: `internal/keys/keys.go`
- Test: `internal/keys/keys_test.go`

**Interfaces:**
- Produces: `keys.Generate(prefix string) error`, `keys.LoadPrivate(path string) (ed25519.PrivateKey, error)`, `keys.LoadPublic(path string) (ed25519.PublicKey, error)`
- File convention: `Generate(prefix)` writes `prefix + ".priv"` and `prefix + ".pub"`, each containing the base64-encoded raw key bytes.

- [ ] **Step 1: Write the failing test**

Create `internal/keys/keys_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/keys/... -v`
Expected: FAIL — package `keys` / `Generate`/`LoadPrivate`/`LoadPublic`/`writeBase64` undefined.

- [ ] **Step 3: Write keys.go**

Create `internal/keys/keys.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/keys/... -v`
Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/keys/keys.go internal/keys/keys_test.go
git commit -m "feat: add Ed25519 keypair generation and loading"
```

---

### Task 6: CLI scaffold + `keygen` command

**Files:**
- Create: `cmd/cli/main.go`
- Create: `cmd/cli/root.go`
- Create: `cmd/cli/keygen.go`
- Test: `cmd/cli/keygen_test.go`

**Interfaces:**
- Consumes: `keys.Generate` (Task 5)
- Produces: `rootCmd *cobra.Command` (package-level in `cmd/cli`), binary `handoffguard` with `keygen` subcommand

- [ ] **Step 1: Add the cobra dependency**

Run: `cd /Users/pratyush/projectidea && go get github.com/spf13/cobra@latest`
Expected: `go.mod`/`go.sum` updated with `github.com/spf13/cobra`.

- [ ] **Step 2: Write the failing test**

Create `cmd/cli/keygen_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeygenCommandWritesKeyFiles(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "sub", "key")

	rootCmd.SetArgs([]string{"keygen", "--out", prefix})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute keygen: %v", err)
	}

	if _, err := os.Stat(prefix + ".priv"); err != nil {
		t.Fatalf("expected private key file: %v", err)
	}
	if _, err := os.Stat(prefix + ".pub"); err != nil {
		t.Fatalf("expected public key file: %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./cmd/cli/... -run TestKeygenCommand -v`
Expected: FAIL — `rootCmd` undefined / package doesn't build.

- [ ] **Step 4: Write root.go**

Create `cmd/cli/root.go`:

```go
package main

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "handoffguard",
	Short: "HandoffGuard: an authorization inheritance layer for multi-agent AI systems",
}
```

- [ ] **Step 5: Write main.go**

Create `cmd/cli/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Write keygen.go**

Create `cmd/cli/keygen.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"handoffguard/internal/keys"
)

func defaultKeyPrefix() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "key"
	}
	return filepath.Join(home, ".handoffguard", "key")
}

var keygenOut string

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate an Ed25519 signing keypair",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.MkdirAll(filepath.Dir(keygenOut), 0700); err != nil {
			return fmt.Errorf("create key directory: %w", err)
		}
		if err := keys.Generate(keygenOut); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s.priv and %s.pub\n", keygenOut, keygenOut)
		return nil
	},
}

func init() {
	keygenCmd.Flags().StringVar(&keygenOut, "out", defaultKeyPrefix(), "key file prefix (writes <prefix>.priv and <prefix>.pub)")
	rootCmd.AddCommand(keygenCmd)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./cmd/cli/... -v`
Expected: PASS for `TestKeygenCommandWritesKeyFiles`.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum cmd/cli/main.go cmd/cli/root.go cmd/cli/keygen.go cmd/cli/keygen_test.go
git commit -m "feat: add handoffguard CLI scaffold and keygen command"
```

---

### Task 7: `envelope create` command

**Files:**
- Create: `cmd/cli/envelope_create.go`
- Test: `cmd/cli/envelope_create_test.go`

**Interfaces:**
- Consumes: `envelope.NewID`, `envelope.ValidateStructure`, `envelope.Sign` (earlier tasks), `keys.LoadPrivate` (Task 5)
- Produces: `envelope create --in <draft.json> --key <priv> [--out <signed.json>]`

- [ ] **Step 1: Write the failing test**

Create `cmd/cli/envelope_create_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cli/... -run TestEnvelopeCreateCommand -v`
Expected: FAIL — `envelope create` subcommand doesn't exist.

- [ ] **Step 3: Write envelope_create.go**

Create `cmd/cli/envelope_create.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"handoffguard/internal/envelope"
	"handoffguard/internal/keys"
)

var (
	envelopeCreateIn  string
	envelopeCreateKey string
	envelopeCreateOut string
)

var envelopeCmd = &cobra.Command{
	Use:   "envelope",
	Short: "Create, sign, and verify obligation envelopes",
}

var envelopeCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Sign a draft envelope, producing a signed envelope",
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := os.ReadFile(envelopeCreateIn)
		if err != nil {
			return fmt.Errorf("read draft: %w", err)
		}

		var e envelope.Envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("parse draft: %w", err)
		}

		if e.ID == "" {
			e.ID = envelope.NewID()
		}
		if e.Version == "" {
			e.Version = "1"
		}

		if err := envelope.ValidateStructure(e); err != nil {
			return fmt.Errorf("draft is missing required fields: %w", err)
		}

		priv, err := keys.LoadPrivate(envelopeCreateKey)
		if err != nil {
			return fmt.Errorf("load signing key: %w", err)
		}

		sig, err := envelope.Sign(e, priv)
		if err != nil {
			return fmt.Errorf("sign envelope: %w", err)
		}
		e.Signature = sig

		out, err := json.MarshalIndent(e, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal signed envelope: %w", err)
		}

		if envelopeCreateOut == "" {
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}
		if err := os.WriteFile(envelopeCreateOut, out, 0644); err != nil {
			return fmt.Errorf("write signed envelope: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Wrote signed envelope to %s\n", envelopeCreateOut)
		return nil
	},
}

func init() {
	envelopeCreateCmd.Flags().StringVar(&envelopeCreateIn, "in", "", "path to draft envelope JSON (required)")
	envelopeCreateCmd.Flags().StringVar(&envelopeCreateKey, "key", "", "path to Ed25519 private key file (required)")
	envelopeCreateCmd.Flags().StringVar(&envelopeCreateOut, "out", "", "path to write signed envelope JSON (defaults to stdout)")
	_ = envelopeCreateCmd.MarkFlagRequired("in")
	_ = envelopeCreateCmd.MarkFlagRequired("key")

	envelopeCmd.AddCommand(envelopeCreateCmd)
	rootCmd.AddCommand(envelopeCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/cli/... -v`
Expected: PASS for `TestEnvelopeCreateCommandProducesSignedEnvelope` and prior CLI tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/cli/envelope_create.go cmd/cli/envelope_create_test.go
git commit -m "feat: add envelope create CLI command"
```

---

### Task 8: `envelope verify` command

**Files:**
- Create: `cmd/cli/envelope_verify.go`
- Test: `cmd/cli/envelope_verify_test.go`

**Interfaces:**
- Consumes: `envelope.ValidateStructure`, `envelope.ValidateExpiration`, `envelope.Verify` (earlier tasks), `keys.LoadPublic` (Task 5), `envelopeCmd` (Task 7)
- Produces: `envelope verify --in <signed.json> --key <pub>`, exit code 0 on VALID, 1 on INVALID

- [ ] **Step 1: Write the failing test**

Create `cmd/cli/envelope_verify_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cli/... -run TestEnvelopeVerifyCommand -v`
Expected: FAIL — `envelope verify` subcommand doesn't exist.

- [ ] **Step 3: Write envelope_verify.go**

Create `cmd/cli/envelope_verify.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"handoffguard/internal/envelope"
	"handoffguard/internal/keys"
)

var (
	envelopeVerifyIn  string
	envelopeVerifyKey string
)

var envelopeVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify a signed envelope's structure, expiration, and signature",
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := os.ReadFile(envelopeVerifyIn)
		if err != nil {
			return fmt.Errorf("read envelope: %w", err)
		}

		var e envelope.Envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("parse envelope: %w", err)
		}

		pub, err := keys.LoadPublic(envelopeVerifyKey)
		if err != nil {
			return fmt.Errorf("load verification key: %w", err)
		}

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Envelope verification: %s\n", e.ID)

		valid := true

		if err := envelope.ValidateStructure(e); err != nil {
			fmt.Fprintf(out, "Required fields: INVALID (%v)\n", err)
			valid = false
		} else {
			fmt.Fprintln(out, "Required fields: VALID")
		}

		if err := envelope.ValidateExpiration(e, time.Now()); err != nil {
			fmt.Fprintf(out, "Expiration: INVALID (%v)\n", err)
			valid = false
		} else {
			fmt.Fprintf(out, "Expiration: VALID (expires %s)\n", e.ExpiresAt.Format(time.RFC3339))
		}

		if err := envelope.Verify(e, pub); err != nil {
			fmt.Fprintf(out, "Signature: INVALID (%v)\n", err)
			valid = false
		} else {
			fmt.Fprintln(out, "Signature: VALID")
		}

		if !valid {
			fmt.Fprintln(out, "STATUS: INVALID")
			return fmt.Errorf("envelope %s failed verification", e.ID)
		}
		fmt.Fprintln(out, "STATUS: VALID")
		return nil
	},
}

func init() {
	envelopeVerifyCmd.Flags().StringVar(&envelopeVerifyIn, "in", "", "path to signed envelope JSON (required)")
	envelopeVerifyCmd.Flags().StringVar(&envelopeVerifyKey, "key", "", "path to Ed25519 public key file (required)")
	_ = envelopeVerifyCmd.MarkFlagRequired("in")
	_ = envelopeVerifyCmd.MarkFlagRequired("key")

	envelopeCmd.AddCommand(envelopeVerifyCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS for every test in the module, including all three `envelope verify` tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/cli/envelope_verify.go cmd/cli/envelope_verify_test.go
git commit -m "feat: add envelope verify CLI command"
```

---

### Task 9: End-to-end smoke test and README

**Files:**
- Create: `examples/refund-draft.json`
- Create: `README.md`

**Interfaces:**
- Consumes: the built `handoffguard` binary (Tasks 6–8)
- Produces: a manually runnable smoke test documented in `README.md`

- [ ] **Step 1: Create an example draft envelope**

Create `examples/refund-draft.json`:

```json
{
  "version": "1",
  "issuer": { "agent": "support-agent", "version": "2.3" },
  "recipient": { "agent": "billing-agent" },
  "purpose": "customer_support_refund",
  "resources": {
    "orders": ["48319"],
    "customers": ["cus_8291"]
  },
  "allowed_actions": ["orders.read", "refunds.create"],
  "denied_actions": ["customers.delete", "customers.update_identity", "payments.export"],
  "data_classes": ["customer_pii", "payment_metadata"],
  "approvals": [
    { "condition": "refund.amount > 500", "required_role": "refund_manager" }
  ],
  "delegation": { "max_depth": 2, "current_depth": 1, "may_expand_authority": false },
  "expires_at": "2099-01-01T00:00:00Z",
  "policy_version": "refund-policy-v8"
}
```

- [ ] **Step 2: Build the binary and run the smoke test manually**

Run:
```bash
cd /Users/pratyush/projectidea
go build -o handoffguard ./cmd/cli
./handoffguard keygen --out /tmp/hg-demo-key
./handoffguard envelope create --in examples/refund-draft.json --key /tmp/hg-demo-key.priv --out /tmp/hg-demo-signed.json
./handoffguard envelope verify --in /tmp/hg-demo-signed.json --key /tmp/hg-demo-key.pub
```
Expected: `envelope verify` prints `STATUS: VALID` and exits 0 (`echo $?` prints `0`).

- [ ] **Step 3: Write README.md**

Create `README.md`:

```markdown
# HandoffGuard

Authorization inheritance layer for multi-agent AI systems. See `prd.md`
for the full product spec. This is sub-project 1 of the MVP: the
Obligation Envelope core (model, canonical JSON, Ed25519 signing,
structural/expiration validation) and a CLI to create and verify
envelopes.

## Build

```bash
go build -o handoffguard ./cmd/cli
```

## Try it

```bash
./handoffguard keygen --out ~/.handoffguard/key
./handoffguard envelope create --in examples/refund-draft.json --key ~/.handoffguard/key.priv --out /tmp/signed.json
./handoffguard envelope verify --in /tmp/signed.json --key ~/.handoffguard/key.pub
```

## Test

```bash
go test ./...
```

## Status

Envelope core only. Delegation/monotonicity policy engine, server,
MCP gateway, and dashboard are separate sub-projects — see
`docs/superpowers/specs/` for their design docs as they land.
```

- [ ] **Step 4: Commit**

```bash
git add examples/refund-draft.json README.md
git commit -m "docs: add example draft envelope and README smoke test instructions"
```
