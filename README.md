# HandoffGuard

Authorization inheritance layer for multi-agent AI systems. See `prd.md`
for the full product spec. Implemented slices include the Obligation Envelope
core and the monotonic-delegation policy engine, with CEL approval conditions
and CLI commands to create, verify, validate, and compare envelopes.

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

Envelope core and policy engine are implemented. Server, persistence, MCP
gateway, agent integration, dashboard, and CI integration remain. See
[policy engine design](docs/design/policy-engine.md) for rules and limitations.

## Envelope format and keys

`envelope create` and `envelope verify` accept JSON input. Creation defaults
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

## Check delegated authority

```bash
./handoffguard policy validate examples/policy-parent.json
./handoffguard policy diff examples/policy-parent.json examples/policy-child-allow.json
./handoffguard policy diff examples/policy-parent.json examples/policy-child-deny.json --format json
```

The first diff returns ALLOW. The second returns DENY with `ACTION_EXPANDED`
and `APPROVAL_WEAKENED`, and exits nonzero. Policy commands accept JSON and YAML
envelopes. These examples are unsigned content checks. To verify signatures too:

```bash
./handoffguard policy diff parent-signed.json child-signed.json \
  --parent-key parent.pub --child-key child.pub
```

Children must link to their parent, retain its purpose and policy version, and
identify its recipient as their issuer. Actions, resources, data classes, expiry,
and maximum depth may narrow; denials and approvals must be preserved. Lowering
an approval threshold from `refund.amount > 500` to `refund.amount > 300` is allowed.
Unproven changes to complex conditions are denied; retain the inherited condition
and add another requirement instead.

The Go API `policy.Conditions.RequiredRoles` evaluates CEL conditions for request
data and returns required roles. Any evaluation error must result in denial.
Actual approval verification and tool-call enforcement are later work.

```bash
go test -race ./...
go test ./internal/policy -run '^$' -fuzz '^FuzzDelegationCannotExpandAuthority$' -fuzztime 5s
go test ./internal/policy -run '^$' -fuzz '^FuzzApprovalThresholdCannotWeaken$' -fuzztime 5s
go test ./internal/policy -run '^$' -bench BenchmarkDiff -benchmem
```
