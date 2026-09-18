# Dashboard checkpoint

Implemented on `feat/dashboard`, based on agent integration commit `579b384`.

## Completed

- Next.js 16.3.5 App Router with TypeScript, shadcn/ui, and React Flow.
- Server-rendered overview and run pages; client graph/inspector loaded separately.
- Authenticated, paginated Go overview API with literal search and blocked-run filter.
- Signed envelope graph, denied delegation proposals, before/after constraint fields,
  authorization decision history, and explicit audit verification.
- Separate viewer password and signed HttpOnly session cookie. The Go control token
  remains server-only. No approval, revocation, or policy-editing UI is exposed.
- Local fonts, light/dark themes, responsive layout, keyboard inspection controls,
  empty/error/loading states, and documented setup/limitations.

## Validation

- Production build and TypeScript checks passed.
- ESLint, Go tests, and Go vet passed.
- PostgreSQL overview integration tests passed: empty state, counts, pagination,
  search, blocked filter, invalid input, and missing bearer token.
- Python Playwright checks against the real Go API passed: authentication, search,
  four-node graph with a denied proposal, constraint inspection, audit validation,
  decision history, theme switch, mobile overflow, and logout. No browser errors.
- Desktop and mobile screenshots were inspected. Test services shut down cleanly.

Screenshots from isolated test records are at `/tmp/handoffguard-overview.png`,
`/tmp/handoffguard-run.png`, and `/tmp/handoffguard-mobile.png`.

## Next

See [dashboard setup](../dashboard/README.md). The [CI gate](ci.md) is now implemented
on `feat/ci-gate`; hosted execution still requires a GitHub remote. Remaining work
includes Docker Compose/deployment polish and broader benchmarks. Multi-user OIDC and
production identity hardening are not implemented. This feature stack remains
unmerged into `master`.

## UI refinement

The workspace now uses a restrained dark navigation rail, neutral content surfaces,
readable table and inspector text, a compact statistics strip, and direct blocked-run
navigation. The run list includes policy versions. Promotional headings and taglines
were removed. Mobile viewers can sign out from the header, and the graph refits when
its container width changes while retaining manual pan/zoom at a fixed width.

Production build, TypeScript, ESLint, and the real-API browser suite passed after
these changes. Browser checks also cover blocked-run navigation and verify that all
graph nodes fit horizontally at mobile width. Refreshed desktop/mobile screenshots
are at the paths above.
