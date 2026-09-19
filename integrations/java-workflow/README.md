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

The default amount is 100. Above 500, Billing is denied unless the trusted demo
operator explicitly grants approval for the exact envelope and refund arguments:

```sh
java -jar integrations/java-workflow/target/handoffguard-workflow.jar --amount=825
java -jar integrations/java-workflow/target/handoffguard-workflow.jar --amount=825 --approve-demo-refund
```

The first command exits nonzero. The approval flag is a simulation shortcut for an
operator; it is never exposed as an agent tool. Refunds and notifications are simulated.
All runs are deterministic, with no LLM, API key, or billable request. The former
Python integration's optional live-model mode is not implemented in Java.

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
  errors and uncertain outcomes stop progress. Receipts must match the order, amount,
  and actual refund ID before the next handoff can begin.
- Output includes the run ID, receipts, verified audit result, and local correlation
  events containing only run/stage identifiers. There is no external trace exporter.

This is a trusted orchestration process, not a sandbox or independently authenticated
agent identity. The control token can create approvals. Public deployments need an
actual operator identity system. Audit decisions are durable in PostgreSQL; workflow
receipts and stage state are local. There is no durable retry, crash recovery, or
compensation protocol. Do not automatically retry a refund after an uncertain outcome.

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

Additional demonstrations against a running API:

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
> delegation, approval enforcement, and PostgreSQL audit verification; validated
> success and denial paths with JUnit integration tests and GitHub Actions CI.

## Docker

Run `./compose.sh up` followed by `./compose.sh demo` from the repository root to
build and execute this application without installing a JDK or Maven on the host.
The demo image includes the Go gateway binary and runs as a non-root user. See
[the Docker guide](../../docs/docker.md) for the full stack and approval examples.
