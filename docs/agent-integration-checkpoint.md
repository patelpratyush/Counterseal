# Agent integration checkpoint

The Python integration has been replaced on `feat/java-integration` with a Java 21 /
Spring Boot application. See [the Java guide](../integrations/java-workflow/README.md)
for architecture, commands, boundaries, and resume wording.

## Implemented

- Support → Billing → Notification orchestration using the official Java MCP SDK.
- Signed, narrowing handoffs and a dedicated gateway/tool surface per stage.
- Exact argument and receipt checks, explicit operator approval, at-most-once attempts.
- Run correlation events, audit verification, and resource cleanup.
- JUnit unit and real PostgreSQL/API/gateway integration tests.
- Bash development/CI helpers and TypeScript Playwright browser tests.
- CI with Java 21, Maven, JUnit report artifacts, and the shell policy Action.

The deterministic Java demo needs no model service. The previous optional live-model
mode and SDK-specific traces were removed; local run/stage events provide correlation.
Go remains the policy/security engine and Next.js remains the dashboard. No production
identity system, durable workflow recovery, or Docker deployment was added here.

```sh
bash scripts/test-postgres.sh bash scripts/smoke-agents.sh
```

The feature stack is merged into `main`. Hosted CI and branch rules are tracked in the [release checkpoint](release-checkpoint.md). [Docker Compose](docker.md) is implemented on `feat/docker-compose`.

## Local validation

- Six JUnit unit tests and nine integration tests passed on Java 21.
- Go race tests passed against a temporary PostgreSQL database.
- Dashboard ESLint, production build, and TypeScript Playwright checks passed.
- Shell policy Action checks and GitHub workflow syntax validation passed.
- The packaged Java gateway demo passed. An isolated launcher check seeded a Java
  workflow, started the dashboard, and cleaned up all services successfully.
