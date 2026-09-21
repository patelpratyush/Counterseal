# Docker Compose

Requires Docker Engine/Desktop with Compose v2.20+ and OpenSSL. No host Go, Java,
Maven, PostgreSQL, or Python installation is needed to run the stack.

From the repository root:

```sh
./compose.sh up
./compose.sh demo
```

The first command builds the images, starts PostgreSQL, the Go API, and the
production Next.js dashboard, and waits for health checks. It prints the viewer
password and dashboard address: **http://localhost:3100**. The second command
runs the Java/Spring Boot Support → Billing → Notification demo and writes real
audit records to the container database. Refresh the dashboard to see the run.
Every demo invocation creates another run. All refunds and notifications are simulated.

The first build downloads dependencies and can take several minutes. Subsequent
builds reuse Docker layers. The Java image build runs JUnit unit tests.

```sh
./compose.sh status
./compose.sh logs
./compose.sh down
```

`down` stops and removes containers while preserving data. `./start.sh` remains the
separate, host-based development launcher; its preview data is not shared with Docker.
The Compose dashboard uses port 3100 to avoid the development server on port 3000.

## Approval demonstration

```sh
# Expected to fail: approval is missing.
./compose.sh demo --amount=825
# Explicit demo-operator approval, scoped to the exact refund.
./compose.sh demo --amount=825 --approve-demo-refund
```

These are Java command-line options: use `--amount=825`, with an equals sign.
The approval flag is an operator simulation, not an authenticated approval UI.

## Configuration and persistence

`compose.sh up` creates `.env.compose` with random database, API, and viewer
credentials. The file is private to its owner and excluded from Git and Docker
build contexts. Keep it for subsequent starts. To change the host port, edit
`HG_DASHBOARD_PORT` in that file and run `./compose.sh up` again.

Three named volumes hold state:

- `handoffguard_postgres-data`: PostgreSQL records.
- `handoffguard_signing-keys`: the Ed25519 signing key pair.
- `handoffguard_workflow-state`: Java workflow checkpoints and saved receipts.

Back up all three volumes and `.env.compose` together. Never restore an older workflow
checkpoint and blindly replay operations; reconcile it against the upstream first.
The API generates its signing
key only when both key files are absent; it refuses an incomplete pair. Container
recreation preserves the key so previously stored envelopes remain verifiable.
The PostgreSQL password in an existing volume is not changed by editing the env
file; database password rotation needs a corresponding PostgreSQL role update.

For direct Compose commands, include the environment file:

```sh
docker compose --env-file .env.compose ps
```

To intentionally erase **all Docker demo records and signing keys**:

```sh
docker compose --env-file .env.compose --profile demo down --volumes
```

This does not erase `.local-preview/` or its development database.

## Container boundaries

The API and database have no host-published ports. Only the dashboard is published,
bound to host loopback. The dashboard and optional Java demo share the API's network
namespace (`network_mode: service:api`) and call `http://127.0.0.1:8080`. This keeps
the application's HTTPS-or-loopback control API rule intact. The shared namespace
is why the dashboard's port mapping appears under the `api` service in Compose.
PostgreSQL is reached through the private Compose network.

Go, Java, and dashboard application containers run as non-root users. The dashboard
image uses Next.js standalone output with static assets. Credentials are supplied
at runtime, never as image build arguments. Development files, local secrets,
node_modules, Maven targets, and preview state are excluded from build contexts.

This configuration is a local portfolio/demo deployment. For a public deployment,
add HTTPS termination and production identity/authorization, database backups,
secret management, and operational monitoring. The dashboard's existing single-viewer
model still applies. Java supports [durable local recovery](workflow-recovery.md),
but uncertain outcomes require reconciliation and distributed recovery is not supplied. Keep using
`localhost` for this local browser URL; production session cookies require a secure
browser context.

## Verification

With host Node/npm, jq, and Chromium installed for Playwright:

```sh
npm ci --prefix dashboard
(cd dashboard && npx playwright install chromium)
bash scripts/test-compose.sh
```

The test creates a separate project and credentials, builds all images, checks a
successful Java run, rejects an unapproved refund, permits an explicitly approved
refund, recreates containers, then verifies the original run's graph and audit in
a real browser. Its temporary volumes are removed on exit. Saved developer volumes
are never selected. Set `HG_COMPOSE_TEST_PORT` if test port 3137 is busy.

The GitHub CI container job runs this same script and contributes to **CI gate**.

Local validation passed on Docker Desktop with Linux containers: all image builds,
Java approval scenarios, identical signing-key hash after recreation, saved-run
browser inspection, and audit verification. The disposable test containers and
volumes were removed afterwards. Hosted execution is tracked in GitHub Actions.
