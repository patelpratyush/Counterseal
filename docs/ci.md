# CI and delegated policy checks

`.github/workflows/ci.yml` runs for every pull request, pushes to `master` or
`main`, and manual dispatches. Five jobs feed a single **CI gate** result:

| Job | Checks |
| --- | --- |
| Backend | Go formatting, vet, race tests with PostgreSQL, bounded policy fuzzing, CLI build |
| Agent and gateway integration | Guarded/unguarded MCP demo and Java/Spring Boot workflow and JUnit tests |
| Dashboard | Locked npm install, ESLint, production build, real-API Chromium browser checks |
| Policy Action gate | Allowed and denied delegation fixtures, adapter failures, signing and tampering |
| Docker Compose integration | Container builds, Java approval paths, persisted data/keys, browser login and audit |

Database jobs each use their own disposable PostgreSQL 18 service. Tests require
no OpenAI API key or repository secrets. Browser screenshots are uploaded for
seven days. External Actions are pinned to commit SHAs; Dependabot checks them
weekly. The aggregate gate fails if any required job fails or is skipped.

## Enable on GitHub

Push this branch to your GitHub repository and open a pull request. After the
first successful workflow run, make **CI gate** a required status check in the
branch rules for your default branch. Workflow files alone do not prevent merges.
The repository is [patelpratyush/Counterseal](https://github.com/patelpratyush/Counterseal).
Local validation does not replace the first Ubuntu-hosted run or configuration of
required status checks.

## Reusable policy Action

The root `action.yml` builds the CLI and checks an actual parent-to-child
**delegation**. It is not a general comparison between revisions of the same
policy: IDs, issuer/recipient, depth, expiration, and inherited constraints must
form a valid delegation.

For a local demonstration in this repository, after checkout:

```yaml
- name: Check delegation
  uses: ./
  with:
    parent: examples/policy-parent.json
    child: examples/policy-child-allow.json
```

The Action requires Bash and jq (available on GitHub's Ubuntu runners),
installs Go from its own `go.mod`, and accepts JSON or YAML paths relative to
`GITHUB_WORKSPACE`. Optional `parent-key` and `child-key` inputs must be supplied
together to verify signatures. Without keys, the check evaluates policy content
only; it does not authenticate either envelope. No private signing keys are needed.

Only `ALLOW` succeeds. Expanded actions, weakened approvals, malformed inputs,
invalid signatures, and checker errors fail the step. A successful checker
invocation produces a `decision` output (`ALLOW`, `DENY`, or `ERROR`), a `report`
output containing the runner-local JSON path, and a job summary. Setup or build
failures can occur before these outputs exist. Reports are not uploaded unless
another step uploads them.

For enforcement against untrusted pull requests, use a reviewed, immutable
HandoffGuard commit via `uses: OWNER/REPOSITORY@FULL_COMMIT_SHA` after publishing
this repository (replace both placeholders). Obtain the parent envelope and public
keys from a trusted base revision or protected policy source; the proposed child
can come from the pull request. A PR must not be able to expand its own parent
or replace the checker. The local `uses: ./` example tests the checked-out Action
implementation and is not an independent trust boundary. Protect changes to the
workflow that defines the gate as well.

Do not use `continue-on-error` on an enforcing check. Our CI uses it solely on the
intentionally denied fixture and then asserts that it failed with `DENY`.

## Local equivalents

With Go, PostgreSQL tools, Java 21+, Maven, Bash, jq, and Node/npm installed:

```bash
go vet ./...
bash scripts/test-postgres.sh go test -race -count=1 ./...
bash scripts/test-policy-action.sh
mvn -B -f integrations/java-workflow/pom.xml verify
bash scripts/test-postgres.sh bash scripts/smoke-gateway.sh
bash scripts/test-postgres.sh bash scripts/smoke-agents.sh
npm ci --prefix dashboard
npm run lint --prefix dashboard
npm run build --prefix dashboard
(cd dashboard && npx playwright install chromium)
bash scripts/test-postgres.sh bash scripts/smoke-dashboard.sh
```

The PostgreSQL helper creates and removes an isolated database and Unix socket;
it does not use your preview database. CI uses its service database directly.
