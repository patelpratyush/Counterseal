# Operator accounts and refund approvals

The console uses individual accounts stored in PostgreSQL. A `viewer` can inspect
runs and verify audit history; a `refund_manager` can also approve exact refunds.
The API derives the approver's username, stable account ID, and role from their
server-side session. Supplying `approved_by` or `role` in an operator approval is
rejected. The service token cannot create approvals in the default configuration.

## First account

`./start.sh` and `./compose.sh up` provision an initial `operator` account with the
`refund_manager` role and display its initial password. Provisioning is idempotent:
restarting does not reset an existing password or reactivate a disabled account.
The generated initial password remains in the launcher's private credentials file;
if you reset the password, use the new one rather than the printed initial value.

Create named accounts using host database access, not the service API. For example,
with `HANDOFFGUARD_DATABASE_URL` configured and the CLI built:

```bash
read -r -s HANDOFFGUARD_OPERATOR_PASSWORD
export HANDOFFGUARD_OPERATOR_PASSWORD
./handoffguard operator create --username pratyush --name 'Pratyush Patel' --role refund_manager
unset HANDOFFGUARD_OPERATOR_PASSWORD
```

Enter a password of 16–256 bytes at the hidden prompt. Usernames are lowercase,
3–64 characters, and use letters, digits, `.`, `_`, or `-`. Use `--role viewer`
for a read-only account. There is no public registration or service-token account
administration endpoint. Database/host administrators provision roles.

To reset a password or disable an account:

```bash
read -r -s HANDOFFGUARD_OPERATOR_PASSWORD
export HANDOFFGUARD_OPERATOR_PASSWORD
./handoffguard operator reset-password --username pratyush
unset HANDOFFGUARD_OPERATOR_PASSWORD
./handoffguard operator disable --username former-operator
```

Both operations immediately delete that account's sessions. Password reset does
not reactivate a disabled account. Disabling an account does not retroactively
remove decisions it made; an existing approval remains subject to its original
expiry and consumption rules. For Docker administration, run the same CLI inside
the API container with its database environment; pass new passwords through the
container environment rather than command-line arguments.

## Prepare → approve → resume

The Java CLI no longer accepts `--approve-demo-refund`. Prepare the workflow before
attempting Billing:

```bash
java -jar integrations/java-workflow/target/handoffguard-workflow.jar \
  --amount=825 --prepare-approval --state=.workflow-state/refund-review
```

The CLI prints `AWAITING_OPERATOR_APPROVAL`, the run and Billing envelope IDs,
and the exact `{ "order_id": "48319", "amount": 825 }` arguments. Only Support has
executed at this point. The checkpoint is safe to resume.

1. Sign in to the console with a refund-manager account.
2. Open that run and scroll to **Refund approvals**.
3. Select its Billing envelope, choose order `48319`, enter amount `825`, and
   confirm the exact request.
4. Click **Approve exact refund**. The record shows who approved it and when.
5. Resume the same workflow:

```bash
java -jar integrations/java-workflow/target/handoffguard-workflow.jar \
  --resume --state=.workflow-state/refund-review
```

For Docker, use `./compose.sh demo --amount=825 --prepare-approval
--state=/data/workflows/refund-review` on one line, approve at http://localhost:3100,
then `./compose.sh demo --resume --state=/data/workflows/refund-review`.

The form grants a single-use approval for exact integer units, not a range or a
currency conversion. It expires within 15 minutes and never outlives its envelope.
Changing the amount or order does not match the approval. Submitting the same
scope twice is rejected, even after consumption, so a double click cannot create
a second approval. If an approval expires, create a reviewed new workflow only
after establishing that the old refund was not executed; never bypass an uncertain
checkpoint. See [recovery limits](workflow-recovery.md).

Approving in the UI does not execute a tool or automatically resume Java. A workflow
that already attempted an unapproved refund remains conservatively blocked by its
pending-operation marker. Start with `--prepare-approval` for the human review path.

## Authentication and audit

- Passwords use salted Argon2id (19 MiB, two iterations, one lane); plaintext
  passwords are never stored in the accounts table.
- Random 256-bit session tokens expire after eight hours; only their SHA-256 hashes
  are stored in PostgreSQL. The browser receives an HttpOnly, SameSite=Strict cookie,
  with Secure enabled for production. Use HTTPS outside loopback development.
- Every API request checks the current account and expiry. Logout deletes the
  session row, so copying an old cookie does not keep a logged-out session alive.
- Login limits are shared in PostgreSQL: ten attempts per username and sixty total
  per minute. The global limit is conservative; public deployments should add
  edge-level abuse controls rather than trusting arbitrary forwarded IP headers.
- The dashboard uses only the signed-in operator token. Its production container
  has no control-plane token, database credentials, shared password, or signing secret.
- Approval audit events retain stable operator ID, username, display name at decision
  time, role, envelope, resource, exact argument hash, and refund amount. History
  shows the most recent 100 approvals for a run and whether they were consumed.

Existing approval records are preserved and labeled **Legacy demo identity** when
not linked to an operator account. Existing shared-viewer cookies are not accepted;
users must sign in with an account after upgrading. The launcher keeps existing
credentials files and uses their old generated password only to bootstrap the
initial account if it does not exist.

This is a single-tenant local-account system, without SSO, MFA, self-service password
recovery, or per-team data isolation. Host and database administrators remain trusted.
Use personal accounts for attribution; sharing the bootstrap account defeats that purpose.

## API and compatibility

| Endpoint | Authentication and behavior |
| --- | --- |
| `POST /v1/operators/login` | Public; username/password → operator, opaque token, expiry |
| `GET /v1/operators/me` | Operator session → current identity and role |
| `POST /v1/operators/logout` | Operator session → revoke this session |
| `POST /v1/approvals` | Refund-manager session; no caller-supplied identity or role |
| `GET /v1/runs/{runId}/approvals` | Operator or service; attributed history and server timestamp |

Operator sessions cannot issue/delegate/revoke envelopes or evaluate tool calls.
Those operations still require the trusted service token. Approval consumption is
transactional with action authorization and audit, as before.

The explicit server flag `--allow-demo-approvals` preserves caller-asserted approvals
for isolated legacy policy and gateway tests. It is **off by default**, never enabled
by the local preview or Compose launcher, and must not be enabled for operator-based
enforcement. Java internal test fixtures still use this compatibility mode to test
legacy approval consumption and crash windows. The public workflow CLI never
self-approves refunds.

## Verification

```bash
bash scripts/test-postgres.sh go test -race ./internal/server
bash scripts/test-postgres.sh bash scripts/smoke-agents.sh
npm run lint --prefix dashboard
npm run build --prefix dashboard
bash scripts/test-postgres.sh bash scripts/smoke-dashboard.sh
```

Backend tests cover session expiry/revocation, disabled accounts, role enforcement,
forged identities, service-token rejection, exact arguments, duplicate submission,
approval consumption, and attributed audit history. Browser tests cover manager
approval, viewer restrictions, and replaying a logged-out cookie. Java integration
tests cover preparing, approving as a named operator, and resuming the original run.
The Docker test approves through the browser after container recreation, then
resumes the prepared Java workflow successfully.
