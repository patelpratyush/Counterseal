# Counterseal dashboard

Next.js App Router, TypeScript, shadcn/ui (Base UI), and React Flow. Run pages render
on the server; the graph, inspector, and approval form use client components.
Individual operator accounts replace the shared viewer password.

## One-command local preview

```sh
./start.sh
```

The launcher prepares a private PostgreSQL cluster, the Go API, an initial Java
workflow, and an initial `operator` account. It prints the URL, username, and initial
password. Ctrl+C stops the services; `.local-preview/` preserves data and credentials.
Existing accounts are never reset on startup.

Requires Go, Node/npm, PostgreSQL tools, Java 21+, Maven, Bash, jq, curl, and OpenSSL.
Options: `--no-open`, `--no-seed`, `--port 3001`, or `--check`. Logs and saved data live
in `.local-preview/`; only one local preview per checkout should run at a time.

## Manual setup

Start the Go API and [provision an operator](../docs/operator-accounts.md). Then:

```sh
cd dashboard
npm ci
cp .env.example .env.local
npm run dev -- --hostname 127.0.0.1
```

Set only `HANDOFFGUARD_SERVER_URL` in `.env.local` (HTTPS or loopback HTTP).
The dashboard does not need `HANDOFFGUARD_API_TOKEN`, a shared dashboard password,
or a session signing secret. Remove obsolete values from manually maintained
configuration. Never use `NEXT_PUBLIC_` for credentials.

## Console capabilities

- Searchable runs, blocked-run filtering, pagination, and workspace counts.
- Interactive delegation graph, inherited constraints, before/after differences,
  and denied handoff proposals.
- Audit verification, authorization history, light/dark themes, and mobile layouts.
- Refund-manager approval of an exact order/amount with a named signer.
- Approval history showing the operator, timestamp, amount, and consumption state.
- Read-only viewer accounts; server-enforced role restrictions.

The approval form does not execute the refund. Use the Java prepare/approve/resume
flow in the [operator guide](../docs/operator-accounts.md). An ALLOW decision records
authorization, not successful execution; audit verification checks stored integrity.

## Authentication

The Go API owns accounts and opaque, hashed sessions in PostgreSQL. Next.js stores
the token in an HttpOnly, SameSite=Strict cookie (Secure in production), and forwards
it server-side. Sessions expire after eight hours. Logout, password reset, and account
disablement revoke sessions on the server. Every protected request revalidates identity.

Deploy behind HTTPS outside local development. Next.js Server Actions enforce origin
checks; do not broaden allowed origins casually. This is a single-tenant local-account
system, without SSO, MFA, or self-service password recovery. Provision users with the
Go CLI using host database access. Login throttling is shared in PostgreSQL.

## Verification

```sh
npm run lint
npm run build
npx playwright install chromium
cd ..
bash scripts/test-postgres.sh bash scripts/smoke-dashboard.sh
```

The TypeScript browser test uses disposable accounts and a real Go API/database.
It covers login, search, graph inspection, approval attribution and duplicate rejection,
viewer restrictions, audit verification, themes, mobile layout, logout, and rejection
of a copied logged-out cookie. Port 4173 is used by default (`HG_E2E_PORT` overrides it).

## Docker

`./compose.sh up` starts the console at http://localhost:3100. Sign in as `operator`
with the printed initial password. See [Docker setup](../docs/docker.md) and the
[operator approval guide](../docs/operator-accounts.md) for named accounts and demos.
