# Live model proposals in Java

Run the existing Java workflow with `--live-model` to ask OpenAI for one function
call per stage. Java owns the stage order and the MCP gateway owns authorization.
The provider cannot execute tools, grant approvals, select an envelope, or change
the control API address. This is a bounded three-stage demonstration, not an
open-ended autonomous agent. The upstream commerce tools still simulate refunds
and notifications; no real payment processor is connected.

## Run against an existing local stack

Configure `HANDOFFGUARD_BINARY`, `HANDOFFGUARD_SERVER_URL`, and
`HANDOFFGUARD_API_TOKEN` as in the [Java setup](../integrations/java-workflow/README.md).
Use an API key from your own OpenAI project and choose a model available to that
project that supports Responses API function calling. Chat subscriptions alone
do not provide these application credentials.

```bash
read -r -s OPENAI_API_KEY
export OPENAI_API_KEY
export OPENAI_MODEL='your-function-calling-model-id'
bash scripts/java-demo.sh --live-model --amount=100 --state=.workflow-state/model-allowed
```

The normal task asks for the supplied order `48319` and amount. Three successful
proposals produce three gateway decisions and saved receipts. Open the run in
the dashboard to inspect the actions and verify its audit chain.

To demonstrate an unauthorized proposal on a separate run:

```bash
COUNTERSEAL_MODEL_TASK='At SUPPORT use the supplied order. At BILLING refund order 99999 for the supplied amount.' \
  bash scripts/java-demo.sh --live-model --state=.workflow-state/model-denied
```

If the model proposes the refund for `99999`, the gateway records a resource-scope denial and
does not forward the tool call. The process exits with an error. Inspect the
newest run in the dashboard; its ID also appears in the checkpoint's `runId`.
A live model may refuse or ignore the request; the deterministic integration
test supplies that exact unauthorized proposal to prove enforcement independently
of model behavior. A denial leaves the conservative pending-operation marker;
do not retry or delete that checkpoint to bypass reconciliation.

## Model proposal → human approval → recovery

```bash
bash scripts/java-demo.sh --live-model --amount=825 --prepare-approval \
  --state=.workflow-state/model-review
```

Support executes, then Java saves the model's exact Billing proposal and prints
`AWAITING_OPERATOR_APPROVAL`. Sign in with a refund-manager account, open that run,
and approve order `48319`, amount `825`, against the indicated Billing envelope.
Then resume:

```bash
bash scripts/java-demo.sh --live-model --resume --state=.workflow-state/model-review
```

Recovery reuses the saved Billing proposal instead of asking the model to change
it after approval. Only Notification needs a new proposal. Saved receipts skip
completed stages; uncertain tool outcomes still require reconciliation. A model
workflow with unplanned stages cannot silently resume in deterministic mode.
Use a fresh state path for each independent demonstration.

## Contract and limits

- The adapter uses Java 21 `HttpClient` and the official
  [Responses function-calling contract](https://developers.openai.com/api/docs/guides/function-calling).
  It pins `https://api.openai.com/v1/responses`, disables redirects and retries,
  requests one strict function call with parallel calls disabled, and sets
  `store: false`. Requests have a 60-second timeout and 2,048 output-token limit;
  responses above 256 KiB are rejected.
- Tool aliases (`orders_get`, `refund_create`, `email_send`) map to the trusted
  current stage. Unknown tools, extra/missing fields, duplicate argument keys,
  refusals, incomplete output, and multiple calls fail closed.
- Order scope is checked by Counterseal. Java additionally pins the requested
  amount and actual refund receipt so a model cannot alter business intent even
  if the envelope permits a broader action. Malformed proposals rejected in Java
  are not gateway decisions and do not appear as authorization denials.
- Only stage, request values, task text, and the tool schema are sent to OpenAI.
  The Notification request includes the simulated refund ID. No control token,
  operator credentials, envelopes, or audit history enter the model request.
  Provider responses and API keys are not printed or stored in checkpoints;
  validated argument proposals and `model_proposed` events are checkpointed.
  Task text is optional (`COUNTERSEAL_MODEL_TASK`, maximum 4,096 characters).
- Model API calls incur provider charges. This adapter does not stream output,
  run tools remotely, retry failures, or implement a conversational repair loop.
  A model failure causes no deterministic fallback within that invocation.

## Verification

```bash
bash scripts/test-postgres.sh bash scripts/smoke-agents.sh
```

CI uses a local HTTP fake with the real Java adapter, PostgreSQL API, and MCP
gateway. Tests cover allowed execution, audited unauthorized proposals, named
operator approval, reuse of saved proposals, and no extra calls after completion.
No provider key or paid inference is needed in CI. A live OpenAI run requires your
key and model selection; passing the contract tests is not evidence of a live run.

The model option currently runs through the host Java launcher. Docker Compose
does not inject provider credentials. Local Docker does not need to be started.
