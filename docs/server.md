# PostgreSQL control API

This slice adds durable envelope issuance, delegation, authorization decisions,
scoped approvals, revocation, run history, and audit verification. It builds on
the [policy engine](design/policy-engine.md). It does not execute tools.

## Start locally

Use Go and PostgreSQL (integration-tested with PostgreSQL 18). Create a dedicated
empty database with your local PostgreSQL credentials, then configure:

```bash
createdb handoffguard
export HANDOFFGUARD_DATABASE_URL='postgresql://localhost/handoffguard?sslmode=disable'
export HANDOFFGUARD_API_TOKEN="$(openssl rand -hex 32)"
go build -o /tmp/handoffguard ./cmd/cli
/tmp/handoffguard keygen --out "$HOME/.handoffguard/server"
/tmp/handoffguard server --key "$HOME/.handoffguard/server.priv"
```

Reuse the key file on subsequent starts; keygen refuses to overwrite it. The
server binds to `127.0.0.1:8080` by default. `--addr` changes the binding. Database
credentials and the API token are environment variables rather than CLI flags.
Startup applies embedded migrations through version 2 transactionally and pins the public
signing key in the database. A different key fails startup instead of making
existing envelopes unverifiable. SIGINT/SIGTERM triggers graceful shutdown.

In a shell with the same token:

```bash
/tmp/handoffguard audit verify run_YOUR_RUN_ID
```

`HANDOFFGUARD_SERVER_URL` changes the demo's URL. The audit CLI accepts `--url`.
For approvals, use the [operator prepare/approve/resume guide](operator-accounts.md).
Legacy assertion-based demos require the explicit `--allow-demo-approvals` server
flag and should run only in isolated test environments.

## Trust model

This is a single-tenant API with separate service and operator credentials.
The service token (`HANDOFFGUARD_API_TOKEN`, at least 32 bytes) grants envelope
issuance, delegation, evaluation, and revocation, but cannot approve refunds by
default. Individual operator sessions grant read access; only refund managers can
approve. `/healthz` and `/v1/operators/login` are public. All other routes require
a suitable bearer credential. Never give the service token to an untrusted agent.

The server signs envelopes as the authority issuing them on behalf of the named
agents. Agent identity remains a trusted control-plane assertion. Human approvers
are authenticated through [local operator accounts](operator-accounts.md); identity
and role come from the session, not request fields. Agent inventory is populated
from envelope parties. Service-token holders remain trusted policy issuers.

Action requests use normalized `resources`, `data_classes`, and `arguments`.
The trusted caller must derive the first two from the actual tool operation.
They are not independently discovered by this server. CEL roots `refund`,
`order`, `customer`, and `request` all refer to the same arguments object, so a
second condition context cannot disagree with those arguments. The MCP gateway
will supply tool-specific mappings and forward allowed calls in the next slice.

For nonlocal use, put HTTP behind TLS and restrict access to the trusted control
plane. Multi-tenant isolation, external identity-provider integration, MFA,
policy registries, and signing-key rotation are
not implemented in this slice.

## API

All request bodies are JSON. Unknown fields, duplicate keys (including case
aliases), multiple documents, and bodies over 1 MiB are rejected. Decisions are
returned only after the database transaction commits. Missing rows return 404;
conflicts return 409; malformed input returns 400. Authorization denials return
403 with a structured result. Internal failures return 500 without SQL details.

| Endpoint | Body / result |
| --- | --- |
| `POST /v1/envelopes` | `{ "run_id": "optional", "envelope": {...} }`; returns signed root and run ID, 201 |
| `GET /v1/envelopes/{id}` | Stored envelope, run ID, and direct revocation timestamp |
| `POST /v1/envelopes/{id}/delegate` | `{ "child": {...} }`; persists a signed narrowing on ALLOW, 201 |
| `POST /v1/evaluate/handoff` | `{ "parent_envelope_id": "...", "child": {...} }`; records decision without issuing child |
| `POST /v1/evaluate/action` | Normalized action request below; 200 ALLOW or 403 DENY |
| `POST /v1/approvals` | Refund-manager session; scoped approval below; returns attributed approval ID, 201 |
| `GET /v1/runs/{runId}/approvals` | Most recent 100 attributed approvals, consumption state, server timestamp |
| `POST /v1/envelopes/{id}/revoke` | No body; idempotently revokes envelope and effectively its descendants |
| `GET /v1/runs/{runId}/chain` | Envelopes, handoffs, and action decisions |
| `GET /v1/runs/{runId}/violations` | Ordered policy violation records |
| `POST /v1/policies/diff` | `{ "parent": {...}, "child": {...} }`; unsigned content analysis, explicitly labeled |
| `POST /v1/audit/{runId}/verify` | Integrity report with `VALID`/`INVALID`, counts, failures, and head hash |

Root envelopes use the existing envelope schema. The server defaults absent ID,
version, and run ID. Supply issuer, recipient, constraints, purpose, policy
version, and expiration explicitly. A root has no parent, depth zero, and maximum
depth at most 128. A run has exactly one root. Invalid root constraints return
422 and are not stored.

Delegation uses a **complete child envelope** rather than a partial patch.
ID/version/parent reference can be omitted; everything else must be explicit.
The child issuer must match the parent's recipient, its current depth must be
one greater, and all policy-engine narrowing rules apply. Both accepted and
rejected handoffs are recorded; rejected children are never stored. An accepted
evaluation-only handoff also does not store a child. Incoming signatures are
replaced on issuance with the server signature.

Every action and delegation checks stored signatures, expiration, policy
structure, parent references, and every ancestor's revocation status. Revocation
is evaluated through ancestry, so revoking one branch does not revoke siblings.
A GET returns stored records; it is not an authorization check. The action API
checks recipient identity, explicit denials before grants, resource scope, data
classes, and CEL approval obligations.

Example action:

```json
{
  "envelope_id": "env_...",
  "agent_id": "billing",
  "tool": "refunds.create",
  "resources": {"orders": ["48319"]},
  "data_classes": ["payment_metadata"],
  "arguments": {"order_id": "48319", "amount": 825}
}
```

Resources must contain at least one concrete identifier. Resource category names
cannot contain `:`; approval resources use `category:id` with the first colon
separating the two fields. Resource-free tools
need a future explicit mapping; omission is denied. Argument numbers retain
JSON precision for hashing; CEL receives checked int64, uint64, or double values.
Unrepresentable numbers are rejected and CEL errors deny authorization.

Example approval:

```json
{
  "envelope_id": "env_...",
  "action": "refunds.create",
  "resource": "orders:48319",
  "arguments": {"order_id": "48319", "amount": 825},
  "expires_at": "2098-01-01T00:00:00Z"
}
```

Choose an approval expiry in the future and no later than the envelope expiry.
Use a refund-manager session token. Do not supply `approved_by` or `role`: the API
derives them from the session. Operator approvals currently cover `refunds.create`
with exact `order_id` and positive integer `amount` arguments; the same scope
cannot be approved twice, even after consumption.
Approval arguments must exactly match the authorized arguments, including
critical parameters such as amount. The server stores only their SHA-256 hash.
This initial implementation uses exact arguments rather than the PRD's optional
`max_amount` range: changing an argument requires a new approval. Object key
ordering does not affect hashes; numeric spelling such as `825` versus `825.0`
can produce a different hash and requires an exact match.

Approvals are matched for every required role and concrete resource. They are
not inherited across envelopes. On ALLOW, matching approvals are consumed once
inside the same transaction as the action decision and audit event. On DENY,
none are consumed. Concurrent requests cannot reuse a single approval. An ALLOW
reserves/consumes approval for one attempt; it does not prove tool execution or
provide end-to-end exactly-once behavior if a client loses the response.

## Storage and audit integrity

Migration creates agents, runs, envelopes, handoffs, approvals, tool_actions,
violations, audit_events, server_metadata, schema_migrations, operators,
operator_sessions, and login_limits. Envelopes retain
the full signed JSON. Handoffs retain decisions; actions retain argument hashes
and decisions, never raw tool arguments. Audit action payloads retain IDs,
resource/data-class metadata, argument hashes, decisions, and consumed approval
IDs. Operator approval entries record the refund amount and attributed operator
identity alongside the argument hash; arbitrary raw tool arguments are not retained.

Service transactions use PostgreSQL's
[transaction-scoped advisory lock](https://www.postgresql.org/docs/18/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS)
to serialize authorization, approval consumption, revocation, and audit appends
across processes. This prioritizes correctness for the MVP; it limits throughput.
The implementation uses [pgx connection pooling](https://github.com/jackc/pgx/wiki/Getting-started-with-pgx).
Each request has a 15-second database timeout. List endpoints return the entire
run; pagination and per-run concurrency are future scaling work.

Each audit event hashes its payload, previous event hash, event ID, run ID, type,
and UTC timestamp. Verification checks the links and hashes, stored envelope
signatures and parent references, and their creation snapshots. Expiration or
revocation does not invalidate a historical signature. The returned `head_hash`
can be retained outside the database as a checkpoint.

A hash chain without an external checkpoint cannot detect deletion of its tail
or a database administrator rewriting the entire chain. Audit verification does
not reconcile every approval/action table field against its audit snapshot;
it checks the audit records and signed envelopes. Independent signed checkpoints
and full database-to-audit reconciliation remain future hardening work.

## Tests

```bash
# Unit tests; PostgreSQL tests skip unless a test URL is set.
go test ./...

# Creates an isolated temporary cluster, runs race-enabled tests, stops it,
# and removes only the cluster it created. Requires initdb and pg_ctl on PATH.
bash scripts/test-postgres.sh

# Or use a dedicated existing test database. Each test creates and drops
# its own randomly named schema; the DB role must be allowed to create schemas.
HANDOFFGUARD_TEST_DATABASE_URL='postgresql://localhost/handoffguard_test?sslmode=disable' \
  go test -race ./internal/server
```

Integration tests cover persistence across service instances, key pinning,
invalid input/authentication, approval scoping/expiry/replay, concurrent use,
transaction rollback after an injected audit failure, delegation, ancestor
revocation, sibling isolation, and audit/envelope tampering.

### Dashboard overview

`GET /v1/dashboard/overview?page=1&q=refund&filter=blocked` returns aggregate
workspace `stats`, a `runs` list, filtered `total`, `page`, and `page_size` (20).
`filter` is omitted/empty for all runs or `blocked` for runs with any denied
handoff/tool authorization. Search is a case-insensitive literal substring of
run ID or root purpose, up to 100 bytes; pages range from 1 to 100000. Runs sort by
creation time descending, then ID descending. Pagination is an offset view, so
newly created runs may shift later pages between requests.

Statistics include runs, evaluated handoffs, blocked handoffs, blocked tool actions,
and distinct policy versions referenced by stored envelopes. They cover the whole
workspace regardless of search/filter. Policy versions are not an active-policy
registry. The endpoint accepts an operator session or the service bearer token.
