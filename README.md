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
