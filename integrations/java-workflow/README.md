# Java / Spring Boot workflow

A Java 21 application orchestrates Support → Billing → Notification through the
Go Counterseal API and the official Java MCP SDK. Spring Boot supplies application
configuration, dependency injection, executable JAR packaging, and shutdown lifecycle.
Each stage has its own gateway process, signed envelope, and one exposed MCP tool.

## Build and run

Install JDK 21 or newer, Maven 3.6.3+, and Go. Java 21 is the CI baseline.
From the repository root:

```sh
mvn -B -f integrations/java-workflow/pom.xml verify
go build -o handoffguard ./cmd/cli
export HANDOFFGUARD_BINARY="$PWD/handoffguard"
export HANDOFFGUARD_SERVER_URL=http://127.0.0.1:8080
# Export HANDOFFGUARD_API_TOKEN matching your running Go API.
java -jar integrations/java-workflow/target/handoffguard-workflow.jar
```

Start the Go API using [the server guide](../../docs/server.md), or run `./start.sh`
to start the whole local preview and seed one Java workflow in an empty database.
Existing preview data is preserved and is not seeded again.

The default amount is 100. Above 500, Billing requires approval by an authenticated
refund manager. Prepare the workflow without attempting the refund:

```sh
java -jar integrations/java-workflow/target/handoffguard-workflow.jar --amount=825 --prepare-approval --state=.workflow-state/review
# Approve the printed run/envelope, order 48319, and amount 825 in the console.
java -jar integrations/java-workflow/target/handoffguard-workflow.jar --resume --state=.workflow-state/review
```

`--prepare-approval` exits successfully with an `AWAITING_OPERATOR_APPROVAL` result.
`--approve-demo-refund` is no longer accepted. See the [operator guide](../../docs/operator-accounts.md).
Attempting a high refund without approval still fails. Refunds and notifications are simulated.
By default runs are deterministic and require no model credentials or billable
requests. The optional [live-model mode](../../docs/live-model.md) uses Java to
request proposals from OpenAI and still enforces every call through Counterseal.

## Durable recovery

The workflow CLI saves checkpoints under `.workflow-state/<UUID>` by default and
prints the path. Choose a stable path to make recovery explicit:

```sh
java -jar integrations/java-workflow/target/handoffguard-workflow.jar --state=.workflow-state/refund-demo
java -jar integrations/java-workflow/target/handoffguard-workflow.jar --resume --state=.workflow-state/refund-demo
```

Resume uses the saved amount and approval choice. Completed stages are skipped;
Notification reuses the saved refund ID. If an external operation was started but
its result was not saved, recovery stops with `RECONCILIATION_REQUIRED` instead of
retrying it. See [recovery behavior, crash tests, and limitations](../../docs/workflow-recovery.md).

## Design

- `WorkflowApplication` is a Spring Boot command-line application, not another HTTP server.
- `WorkflowFactory` is a Spring service that creates run-scoped workflows and closes
  active resources when the application context shuts down.
- `Workflow` is a sequential state machine. A handoff requires a verified receipt
  from the preceding stage. Child envelopes narrow actions, expiry, and depth while
  inheriting resources, approvals, denials, purpose, and data constraints.
- `ControlApi` uses Java's HTTP client with timeouts, no redirects, and HTTPS except
  for loopback development. The bearer token stays in the trusted host process.
- `GatewaySession` uses the official MCP SDK's stdio transport. Each stage exposes
  exactly its one mapped tool. The Go gateway makes the actual authorization decision.
- Tool arguments are fixed by the workflow. Each stage is attempted at most once;
  errors and uncertain outcomes stop progress. Durable runs retain this restriction
  across process restarts. Receipts must match the order, amount,
  and actual refund ID before the next handoff can begin.
- Output includes the run ID, receipts, verified audit result, and local correlation
  events containing only run/stage identifiers. There is no external trace exporter.

This is a trusted orchestration process, not a sandbox or independently authenticated
agent identity. Its control token cannot create approvals in the default server mode.
Approvals come from individual operator sessions. Audit decisions are durable in PostgreSQL; workflow
receipts and stage state are checkpointed on a local POSIX filesystem. This supports
single-host recovery, not distributed execution or automatic reconciliation of
uncertain outcomes. Keep checkpoints intact; do not retry an uncertain refund by
starting a new workflow. No compensation protocol or provider idempotency is supplied.
The original five-argument Java constructor remains an ephemeral library/test mode;
the CLI always uses the durable constructor.

## Tests and demos

Unit tests use JUnit and require no running services. The integration profile must
be run with a disposable API; it fails if connection settings are missing:

```sh
mvn -B -f integrations/java-workflow/pom.xml test
bash scripts/test-postgres.sh bash scripts/smoke-agents.sh
```

The shell harness builds the Go CLI, starts a temporary database and API, runs
`mvn -Pintegration verify`, and cleans up. It covers complete delegation/audit,
missing and explicit approval, expanded-authority denial, early handoffs, changed
arguments, repeated execution, interruption cleanup, hidden tools, approval replay,
and revocation. The gateway demo verifies that a denied refund never reached upstream.
Recovery tests additionally halt separate JVMs around the refund receipt commit and
verify that safe resume sends only the remaining tool call while uncertain resume
does not send a duplicate refund.

Additional demonstrations against a running API:

These legacy assertion-based scenarios require an isolated API started with
`--allow-demo-approvals`. Use the operator UI flow above for the default server.

```sh
bash scripts/demo-gateway.sh
bash scripts/demo-server.sh
```

These build and run the same Java application with `--scenario=gateway` or
`--scenario=server`. To run the gateway demo with disposable services:

```sh
bash scripts/test-postgres.sh bash scripts/smoke-gateway.sh
```

## Resume wording

> Built a Java 21/Spring Boot workflow orchestrator using MCP, signed authority
> delegation, approval enforcement, PostgreSQL audit verification, and durable local
> checkpoints; verified recovery and prevention of refund replay across process crashes
> with JUnit integration tests and GitHub Actions CI.

## Docker

Run `./compose.sh up` followed by `./compose.sh demo` from the repository root to
build and execute this application without installing a JDK or Maven on the host.
The demo image includes the Go gateway binary and runs as a non-root user. See
[the Docker guide](../../docs/docker.md) for the full stack and approval examples.

## Live model proposals

Use `--live-model` with `OPENAI_API_KEY` and `OPENAI_MODEL` to propose each stage
through OpenAI Responses. The same gateway, approval UI, and durable checkpoints
still enforce execution. See the [live-model guide](../../docs/live-model.md) for
allowed, denied, and prepare/approve/resume demonstrations and verification limits.

## Trace a workflow

Set `COUNTERSEAL_TRACE=1` on both the API and Java launcher to record JSON spans
across Java, its MCP gateways, and the control API. Correlation uses MCP metadata
and the HTTP `traceparent` header; arguments and credentials are omitted from spans.
See [operational visibility](../../docs/observability.md) for log and metrics commands.
