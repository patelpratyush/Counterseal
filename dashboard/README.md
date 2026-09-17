# HandoffGuard dashboard

Next.js App Router, TypeScript, shadcn/ui (Base UI), and React Flow. The overview
and run pages render on the server; the graph and inspector load on the client.
Fonts are bundled locally. The Go control token never enters browser props or storage.

## Run locally

Start the Go API first, following [the server guide](../docs/server.md). Then:

```sh
cd dashboard
npm ci
cp .env.example .env.local
```

Set these values in `.env.local`:

- `HANDOFFGUARD_SERVER_URL`: Go API URL; HTTPS or loopback HTTP.
- `HANDOFFGUARD_API_TOKEN`: the same token used by the Go server.
- `HANDOFFGUARD_DASHBOARD_PASSWORD`: a separate viewer password, at least 16 characters.
- `HANDOFFGUARD_DASHBOARD_SECRET`: a random session signing secret, at least 32 characters.

Generate a random password/secret with `openssl rand -hex 32`. Never use
`NEXT_PUBLIC_` for credentials. `.env.local` is ignored by Git.

```sh
npm run dev -- --hostname 127.0.0.1
```

Open http://localhost:3000 and sign in with the viewer password. Run the agent
integration demo to populate real records. Empty workspaces remain empty; there
is no fallback to fabricated metrics.

## What it shows

- Counts of runs, handoffs, blocked decisions, and referenced policy versions.
- Run search by ID/purpose, a blocked-run filter, and pagination (20 rows/page).
- Issued envelopes and denied handoff proposals in an interactive graph.
- Constraints, before/after field comparisons, violations, and authorization history.
- On-demand verification of the stored audit chain and envelope signatures.
- Light/dark themes, keyboard-accessible inspection controls, and responsive layout.

A policy version count is not a count of active policies. An ALLOW tool decision
is authorization, not confirmation of successful upstream execution. An audit
check verifies stored integrity, not current authority or external tamper checkpoints.
The UI does not create approvals, revoke authority, or edit policies.

## Deployment and authentication

`npm run build` creates a standalone-capable build; `npm start` runs the server.
Deploy behind HTTPS: production viewer cookies are Secure, HttpOnly, SameSite=Strict,
and expire after eight hours. Both password and signing-secret rotation invalidate
existing sessions. Sign-out removes the browser cookie; a previously copied cookie
remains valid until expiry or rotation. No credentials are sent to the client bundle.

This is a single-operator viewer with a process-local login limit (10 attempts per
minute), not a multi-user identity service. Put shared/public deployments behind
an authenticated access proxy or add OIDC and durable rate limiting. The dashboard
server holds the powerful Go control token, so restrict access to its host and environment.
Next.js Server Actions apply origin checks; do not broaden allowed origins casually.

## Verify

```sh
npm run lint
npm run build
cd ..
python3 scripts/test-postgres.py go test ./internal/server -run TestDashboardOverview
python3 scripts/test-postgres.py python3 scripts/smoke-dashboard.py
```

Browser tests require Python `playwright` and `python3 -m playwright install chromium`.
They seed an isolated Go API, start the production dashboard with temporary viewer
credentials, check login/search/graph/denials/audit/theme/mobile/logout, and shut down.
Screenshots are written to `/tmp/handoffguard-{overview,run,mobile}.png`.
