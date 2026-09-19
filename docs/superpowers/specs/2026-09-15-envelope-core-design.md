# Envelope Core — Design Spec

**Sub-project 1 of 6** in the Counterseal MVP (see `prd.md` §35 six-week plan). Status: approved for implementation.

## Purpose

Provide the foundational data structure and cryptographic primitives for Counterseal's Obligation Envelope: a signed, tamper-evident record of delegated authority. All later sub-projects (policy engine, server, MCP gateway) build on this package.

## Scope

In scope:
- Envelope Go struct matching the JSON shape in PRD §7
- Deterministic canonical JSON serialization
- Ed25519 signing and verification over SHA-256(canonical JSON)
- Structural validation: required fields present, `expires_at` not in the past, signature valid
- CLI: `handoffguard keygen`, `handoffguard envelope create`, `handoffguard envelope verify`

Out of scope (deferred to later sub-projects):
- Delegation/monotonicity diff logic (parent vs. child authority comparison) — sub-project 2 (Policy Engine)
- CEL-based approval conditions — sub-project 2
- Revocation state, resolving `parent_envelope` against a real store — sub-project 3 (Server + Postgres)
- Persistence of any kind

## Architecture

```
cmd/cli/          — cobra root command, wires subcommands
internal/envelope/
  model.go         — Envelope struct + nested types (Issuer, Recipient, Approval, Delegation)
  canonical.go      — deterministic JSON serialization (sorted map keys, no insignificant whitespace)
  signing.go        — Ed25519 sign/verify over SHA-256(canonical JSON)
  validation.go     — required-field checks, expiration check, signature check
keys/              — keygen helpers: write raw Ed25519 priv/pub key bytes to files
```

No new third-party dependency for cryptography or canonicalization — `crypto/ed25519` and `crypto/sha256` are stdlib. Canonicalizer is hand-written for this struct's shape rather than pulling in a general JCS library. `cobra` is the one new dependency, for the CLI.

## Envelope struct

Mirrors PRD §7:

```go
type Envelope struct {
    ID              string            `json:"id"`
    Version         string            `json:"version"`
    Issuer          Party             `json:"issuer"`
    Recipient       Party             `json:"recipient"`
    Purpose         string            `json:"purpose"`
    Resources       map[string][]string `json:"resources"`
    AllowedActions  []string          `json:"allowed_actions"`
    DeniedActions   []string          `json:"denied_actions"`
    DataClasses     []string          `json:"data_classes"`
    Approvals       []Approval        `json:"approvals"`
    Delegation      Delegation        `json:"delegation"`
    ExpiresAt       time.Time         `json:"expires_at"`
    PolicyVersion   string            `json:"policy_version"`
    ParentEnvelope  string            `json:"parent_envelope,omitempty"`
    Signature       string            `json:"signature,omitempty"`
}

type Party struct {
    Agent   string `json:"agent"`
    Version string `json:"version,omitempty"`
}

type Approval struct {
    Condition    string `json:"condition"`
    RequiredRole string `json:"required_role"`
}

type Delegation struct {
    MaxDepth           int  `json:"max_depth"`
    CurrentDepth       int  `json:"current_depth"`
    MayExpandAuthority bool `json:"may_expand_authority"`
}
```

## CLI Commands

- `handoffguard keygen --out ~/.handoffguard/key`
  Writes `key.priv` and `key.pub` (raw Ed25519 key bytes, base64-encoded in the file).

- `handoffguard envelope create --in draft.json --key key.priv [--out signed.json]`
  Reads an unsigned draft envelope, validates required fields, assigns `id` (`env_` + random suffix) and `issuer`/timestamps if absent, canonicalizes, signs, writes the signed envelope to stdout or `--out`.

- `handoffguard envelope verify --in signed.json --key key.pub`
  Strips `signature`, recanonicalizes, recomputes SHA-256, verifies against the stored signature. Also checks `expires_at` has not passed and required fields are present. Prints a PASS/FAIL report in the style of PRD §12 FR-010 (`audit verify` output), e.g.:
  ```
  Envelope verification: env_01K4R92
  Signature: VALID
  Expiration: VALID (expires 2026-09-15T01:00:00Z)
  Required fields: VALID
  STATUS: VALID
  ```

## Data Flow

```
draft.json → validate required fields → assign id/timestamps
   → canonicalize → SHA-256 → Ed25519 sign → signed envelope JSON
```

Verify reverses it: strip `signature` field → recanonicalize → hash → verify against stored signature.

## Testing

- Canonicalization determinism: same struct built with different map key insertion order produces identical output bytes.
- Sign/verify roundtrip: created envelope verifies successfully.
- Tamper test: mutate a field post-signing (without resigning) → verify returns `SIGNATURE_INVALID`.
- Expired envelope test: `expires_at` in the past → verify returns expiration failure even with a valid signature.
- Missing required field test → verify returns a structural failure.

Target: table-driven unit tests covering the above; no adversarial/fuzz suite yet (that's part of sub-project 2's monotonicity property tests per PRD §32).
