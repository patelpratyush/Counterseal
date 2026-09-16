# HandoffGuard

Authorization inheritance layer for multi-agent AI systems. See `prd.md`
for the full product spec. Implemented slices include the Obligation Envelope
core, the monotonic-delegation policy engine with CEL approval conditions,
and a PostgreSQL-backed control API with scoped approvals and audit records.

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

Envelope core, policy engine, and server/PostgreSQL slices are implemented.
MCP gateway, agent integration, dashboard, and CI integration remain. See
[policy engine design](docs/design/policy-engine.md) and the
[server guide](docs/server.md) for rules, setup, and limitations.

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
The server verifies stored approvals and consumes them on authorization.
Tool-call forwarding and enforcement at the MCP boundary are the next slice.

```bash
go test -race ./...
go test ./internal/policy -run '^$' -fuzz '^FuzzDelegationCannotExpandAuthority$' -fuzztime 5s
go test ./internal/policy -run '^$' -fuzz '^FuzzApprovalThresholdCannotWeaken$' -fuzztime 5s
go test ./internal/policy -run '^$' -bench BenchmarkDiff -benchmem
```

## Run the server

See the [server guide](docs/server.md) for PostgreSQL setup and the API contract.
The API requires a control-plane bearer token and a persistent signing key.

```bash
export HANDOFFGUARD_DATABASE_URL='postgresql://localhost/handoffguard?sslmode=disable'
export HANDOFFGUARD_API_TOKEN="$(openssl rand -hex 32)"
./handoffguard keygen --out "$HOME/.handoffguard/server"
./handoffguard server --key "$HOME/.handoffguard/server.priv"
```

Run `python3 scripts/demo-server.py` in a shell with the same token to exercise
DENY → approval → ALLOW → replay DENY and verify the audit chain. This API is for
trusted control-plane callers; agent and approver identity are supplied by that
caller until identity integration is added.

Run `python3 scripts/test-postgres.py` for the integration suite against a
disposable local PostgreSQL cluster (requires `initdb` and `pg_ctl` on PATH).
