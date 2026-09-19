# MCP gateway

The gateway exposes a selected set of upstream MCP tools and forwards each call
only after the control API commits an ALLOW decision. It uses the
[official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) for MCP
sessions, discovery, cancellation, and transports.

Implemented transports:

- Agent → gateway: stdio, one fixed agent and envelope per gateway process.
- Gateway → upstream: stdio subprocess or Streamable HTTP.
- Gateway → Counterseal: authenticated HTTP API from the [server slice](server.md).

This is a tool gateway. Upstream prompts, resources, sampling, elicitation,
interactive continuations, and tasks are not proxied. Downstream protocol
versions supported are 2025-11-25, 2025-06-18, 2025-03-26, and 2024-11-05.

## Run

Build the binary and start the control API as described in the server guide.
Create a signed envelope whose recipient matches the gateway's fixed agent.
Provide its ID and a trusted mapping file:

```bash
go build -o handoffguard ./cmd/cli

# HANDOFFGUARD_API_TOKEN must be set for the control API.
./handoffguard gateway \
  --config examples/gateway-tools.json \
  --agent billing --envelope env_YOUR_ENVELOPE_ID \
  --api-url http://127.0.0.1:8080 \
  -- ./handoffguard demo-mcp
```

An MCP client launches this command and communicates using stdin/stdout. All
logging goes to stderr; stdout contains only MCP messages. `demo-mcp` is a
simulated commerce server with intentionally absent approval enforcement. It
never contacts a payment service or changes real customer data.

For an existing HTTP MCP server:

```bash
export HANDOFFGUARD_UPSTREAM_TOKEN='separate-upstream-credential'
./handoffguard gateway \
  --config examples/gateway-tools.json \
  --agent billing --envelope env_YOUR_ENVELOPE_ID \
  --upstream-url https://mcp.example.com/mcp
```

The upstream token is optional for public upstreams and is distinct from the
control API token. URLs must use HTTPS, except HTTP on localhost or a loopback IP.
Redirects are not followed. Gateway calls have a 30-second total deadline by
default (`--timeout`); control API requests have an additional 15-second limit.
Startup discovery has a 15-second deadline.

## Trusted mapping

```json
{
  "tools": {
    "refund.create": {
      "action": "refunds.create",
      "resources": {"orders": "/order_id"},
      "data_classes": ["payment_metadata"]
    }
  }
}
```

The key is the exact upstream MCP tool name. `action` is the exact permission
identifier in the envelope. Each resource category maps to a JSON pointer into
the tool arguments. A pointer can select a string ID or a nonempty array of
string IDs. Nested objects, array indices, and `~0`/`~1` pointer escapes are
supported. IDs must be nonblank concrete strings, not wildcards or numbers.
Data classes come from the mapping, never a caller-supplied security context.

Examples:

| Mapping | Tool arguments | Authorized resources |
| --- | --- | --- |
| `"orders": "/order_id"` | `{"order_id":"48319"}` | `orders:48319` |
| `"orders": "/order_ids"` | `{"order_ids":["48319","48320"]}` | Both orders |
| `"customers": "/customer/id"` | `{"customer":{"id":"cus_8291"}}` | `customers:cus_8291` |

Mappings are administrator-controlled policy adapters. They must identify every
resource the tool can affect and every data class it can access. The gateway
cannot infer hidden side effects or determine whether the upstream actually
honors its argument semantics. Resource-free tools are not supported yet.

Only mapped tools are exposed by `tools/list`; unknown tools cannot be invoked.
Configured tools missing upstream fail startup. Discovery supports pagination
and rejects repeated cursors, duplicate tool names, more than 4096 discovered
tools, or more than 100 pages with continuation cursors. Configuration supports
up to 256 mapped tools and 64 KiB. The discovered tool set and input schemas are
held fixed until restart; tool-list changes are not automatically adopted.

## Per-call enforcement

1. Parse one argument object, rejecting duplicate keys and preserving numeric
   precision/spelling. Reject arguments over 256 KiB or nesting beyond 64 levels.
2. Validate against the discovered JSON Schema without inserting defaults or
   coercing types. External schema references are rejected. Schemas are limited
   to 256 KiB; numeric literals to 128 characters and exponent magnitude 308.
3. Derive concrete resources and data classes from the trusted mapping.
4. Request authorization with the process's fixed agent/envelope and the exact
   arguments. A well-formed HTTP 200 ALLOW receipt with no violations is required.
5. Forward the same argument object once to the original upstream tool name.
6. Return the upstream result and log its outcome and SHA-256 hash with the
   control API action ID. Arguments and result contents are not logged by the
   gateway.

Missing approvals, invalid scope, revocation, expired envelopes, malformed
responses, API failures, and deadline expiration prevent forwarding. The
control API persists evaluated decisions and their audit records. Calls rejected
locally before authorization produce stderr events rather than database action
rows. Upstream execution outcomes are currently correlated stderr records, not
additional durable database audit events. Configure log collection if those
outcomes must be retained.

There are no automatic retries, including the SDK's multi-round-trip retries.
An upstream failure after authorization may mean the tool executed but its
response was lost. The gateway returns `UPSTREAM_OUTCOME_UNKNOWN` with an action
ID; reconcile that action before retrying. A lost authorization response can
consume an approval without forwarding the tool. Approvals are not automatically
restored. This is not end-to-end exactly-once execution.

Revocation is checked when authorization commits. It cannot undo a tool already
in flight or revoke an ALLOW between that decision and the external side effect.
The gateway enforces approved attempts, not atomic transactions across external
systems.

## Identity and credentials

A trusted launcher supplies `--agent`, `--envelope`, the mapping file, and the
control API token. The agent's MCP arguments and metadata cannot replace them.
Use a separate process for a different identity or envelope. There is no remote
multi-user gateway authentication endpoint in this slice.

The control token has administrator powers; do not expose the gateway's
configuration/environment or the control API to untrusted agent code. Also deny
agents direct access to the underlying upstream credentials and transport, or
they can bypass the gateway. Local stdio plus environment filtering is not an OS
sandbox; use process/container isolation where needed.

Subprocess upstreams are launched without a shell and inherit only PATH, HOME,
temporary-directory, locale, and platform home variables. To pass a separate
upstream credential, use `--upstream-env NAME`. `HANDOFFGUARD_*` names are reserved
and cannot be passed this way. HTTP upstreams receive only their separately
configured upstream bearer token, never the control token. Tool metadata and
parameter-header annotations are not republished by the gateway. Upstream
processes are still responsible for the contents of their own stderr logs.

For mixed read/refund tools, the demo approval condition is
`has(refund.amount) && refund.amount > 500`. The refund input schema requires an
amount; the read tool does not. Choose conditions and schemas together so missing
fields cannot bypass a required approval.

## Demo and verification

With a running control API and its token in the environment:

```bash
go build -o handoffguard ./cmd/cli
export HANDOFFGUARD_BINARY="$PWD/handoffguard"
export HANDOFFGUARD_SERVER_URL=http://127.0.0.1:8080
bash scripts/demo-gateway.sh
```

The demo issues a temporary-lived envelope and shows:

- Direct upstream: simulated $825 refund succeeds without approval.
- Gateway: refund denied before approval; payment export is hidden.
- Matching approval: exactly the first guarded refund reaches upstream.
- Reused approval: denied.
- Read tool: allowed, then denied after envelope revocation.
- Audit verification: VALID.

To build and run the entire demo against a disposable local Postgres cluster:

```bash
bash scripts/test-postgres.sh bash scripts/smoke-gateway.sh
```

The smoke helper builds into a temporary directory, starts a temporary API
server, runs real stdio gateway/upstream subprocesses, and cleans them up. The
Postgres helper requires `initdb` and `pg_ctl` on PATH. The demo uses Java 21+
and Maven; see the [Java integration guide](../integrations/java-workflow/README.md).

```bash
go test -race ./...
bash scripts/test-postgres.sh go test -race ./...
go vet ./...
```

Tests additionally cover Streamable HTTP, schema bounds above JavaScript's safe
integer range, duplicate-key rejection, identity spoofing, cancellation,
credential separation, redirect rejection, unsupported continuations, and
upstream errors without retries.
