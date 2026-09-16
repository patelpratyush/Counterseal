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

## Envelope format and keys

This CLI accepts JSON input. YAML input is not implemented. Creation defaults
only `id` and `version`; supply `issuer.agent`, `recipient.agent`, `purpose`,
`policy_version`, and `expires_at` explicitly. The model has no creation timestamp.
Verification requires an expiration strictly later than the verification time.

Key generation refuses to replace either existing key file. Choose a new prefix
for a new keypair. Private keys are base64-encoded raw Ed25519 bytes, stored with
owner-only permissions; base64 is not encryption.

The signing format is Go's compact JSON encoding of the typed envelope, with
map keys sorted and struct fields in declaration order, excluding `signature`.
It is deterministic for this model, but is not RFC 8785 JCS. Signatures use
Ed25519 over the SHA-256 digest of those bytes.
