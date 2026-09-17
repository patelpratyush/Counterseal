# Agents SDK integration

This simulated commerce workflow runs Support → Billing → Notification through the
OpenAI Agents SDK. Each handoff requests a signed child envelope from the Go API
before the target agent can start. Every tool call goes through an MCP gateway
bound to that agent and envelope. The upstream only returns simulated receipts;
it never issues a real refund or sends email.

## Setup and run

From the repository root, install Python dependencies with `uv` (Python 3.12 is
used for development). The lockfile pins the SDK and transitive dependencies.

```sh
uv sync --directory integrations/openai-agents --python 3.12 --frozen
go build -o handoffguard ./cmd/cli
```

Start the API as described in [the server guide](../../docs/server.md). In another
shell, supply the same control token and the API URL:

```sh
export HANDOFFGUARD_BINARY="$PWD/handoffguard"
export HANDOFFGUARD_SERVER_URL=http://127.0.0.1:8080
# HANDOFFGUARD_API_TOKEN must match the running server.
uv run --directory integrations/openai-agents --frozen python demo.py
```

Default execution uses a deterministic offline `Model` implementation. It still
runs the real SDK runner, handoffs, MCP subprocesses, control API, and database.
It makes no OpenAI requests and requires no OpenAI API key.

The default refund is 100. An amount above 500 is denied without an approval:

```sh
uv run --directory integrations/openai-agents --frozen python demo.py --amount 825
uv run --directory integrations/openai-agents --frozen python demo.py --amount 825 --approve-demo-refund
```

The second command explicitly authorizes the trusted demo operator to register
one approval for the exact Billing envelope and refund arguments. Approval is
never exposed as an agent tool. This is a simulation shortcut for a human approval
interface; it is not an authenticated approver integration.

For live model execution, configure `OPENAI_API_KEY` in your shell and pass
`--model YOUR_MODEL_ID`. Choose a model available to your account that supports
tool calling. Live execution makes billable API requests. The workflow still
requires successful tool receipts before advancing or reporting completion.

## Boundaries

- Each `Workflow` instance owns one run. Agent handoff inputs cannot choose an
  envelope, change a delegation policy, or supply control credentials.
- Support retains the permissions needed to delegate. Billing drops order-read
  permission; Notification retains only `email.send`. Approvals, resource scope,
  purpose, denials, and depth restrictions propagate. Expiry narrows at each hop.
- Each stage exposes only its one mapped tool, even when its envelope also carries
  authority needed by a later child. Tool arguments must match this demo's fixed
  order and chosen amount. Notification must reference the actual refund receipt.
- Tool failures and delegation denials abort the run. MCP calls are not retried.
  The adapter serializes calls and rejects a repeated successful stage.
- MCP connection owners open and close their sessions in the same asyncio task;
  the workflow context manager closes all sessions after success or failure.
- The demo replaces the default trace exporters with an in-memory processor that
  stores SDK trace/span IDs and span types only. Output includes the HandoffGuard
  run ID for correlation. No traces are uploaded. Applications importing the
  adapter must configure their own tracing policy.
- This adapter runs in a trusted host process with the control-plane token. It is
  not a sandbox for arbitrary Python code, and does not authenticate agent identity
  independently. The gateway strips control-plane credentials from its upstream.
- Authorization audit records are durable; upstream execution receipts and SDK
  traces here are local. There is no durable retry, crash recovery, or compensation
  protocol. A process failure after a refund may leave a partial workflow.

## Tests

With `initdb`, `pg_ctl`, Go, and `uv` on PATH:

```sh
python3 scripts/test-postgres.py python3 scripts/smoke-agents.py
```

The harness creates a temporary PostgreSQL cluster, builds the CLI, starts a
control API on a random loopback port, and runs the actual SDK without a model
service. It verifies successful delegation and audit, denied missing approval,
explicit operator approval, expanded-authority handoff denial, early handoff
rejection, repeated-call rejection, and cancellation cleanup. It shuts down
the API and database and removes temporary data afterwards.
