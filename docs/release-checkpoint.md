# Integrated project checkpoint — 2026-09-19

The feature stack through `4f2ed23` is integrated into `main`, together with these
status notes. The delivered stack consists of:

- Go envelope signing, delegation policy/CEL checks, PostgreSQL API, audit, and MCP gateway.
- Java 21/Spring Boot workflow with scoped handoffs, receipt checks, and explicit approvals.
- Next.js/TypeScript dashboard and Playwright browser tests.
- Bash development tooling, reusable policy Action, GitHub CI, and Docker Compose.

There are no tracked Python programs or Python runtime dependencies. Historical
commits retain the superseded implementation.

## Review and verification

The final review checked authority narrowing and approval conditions, transactional
approval consumption/revocation/audit ordering, gateway enforcement and credential
separation, dashboard session checks, and the Java/container integration boundaries.
No merge-blocking issues were found in that review; this is not an independent
security audit or a production certification.

Validation completed during this feature stack:

- Go race tests with PostgreSQL and a final `go vet ./...` check passed.
- Six Java unit tests and nine real-API/MCP integration tests passed.
- Dashboard lint, production build, and TypeScript browser checks passed.
- Policy Action tests, shell syntax, and GitHub workflow syntax checks passed.
- Docker image builds and Java approval/denial scenarios passed. Container
  recreation preserved the exact public-key hash and stored run; browser login,
  graph inspection, and audit verification passed after recreation.
- Test containers and disposable volumes were removed; the existing local preview
  was preserved. The merge introduces no runtime changes beyond the tested stack.

## Remaining work

The repository is published at [patelpratyush/Counterseal](https://github.com/patelpratyush/Counterseal).
All six jobs passed in [hosted CI for `6da1c20`](https://github.com/patelpratyush/Counterseal/actions/runs/35475480843).
The active main-branch rules require a pull request, the **CI gate** check, and an
up-to-date branch; force pushes and deletion are blocked. Required approvals are zero
for solo development. These settings were verified through the GitHub API on 2026-09-19.

The [portfolio guide](portfolio.md) includes screenshots, an architecture diagram,
an automated browser recording, a narrated-demo script, and measured benchmarks.

Before a public production deployment: add external identity/MFA, team isolation,
distributed workflow recovery, operational monitoring, backups, and external audit
checkpoints. The service API still uses a shared control token and serialized
transactions; human approvals use individual local accounts and role checks.

The Java workflow has an optional [live-model proposal path](live-model.md); its
default remains deterministic. [Operational visibility](observability.md) provides local structured traces and
Prometheus metrics. Remaining work includes collector/alert deployment, concurrent
API benchmarks, and a polished voiceover.

Update 2026-09-21: the Java CLI now supports [durable local workflow recovery](workflow-recovery.md)
with atomic checkpoints, an exclusive process lock, saved receipts, and refusal to
replay uncertain operations. It does not yet support automatic upstream reconciliation
or distributed execution.

Update 2026-09-21: [operator accounts and approval UI](operator-accounts.md) replace
the shared viewer password and public Java self-approval flag. The console uses
revocable operator sessions; manager identity is recorded in each refund approval.

Start the local container demo with `./compose.sh up` and `./compose.sh demo`.
See [Docker setup](docker.md) and [Java integration](../integrations/java-workflow/README.md).
