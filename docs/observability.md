# Operational visibility

Counterseal emits opt-in structured spans across Java → MCP gateway → HTTP API,
and exposes authenticated Prometheus metrics. No collector or Docker VM is
required to inspect either. These are local span records with trace context;
there is no OTLP exporter, tracing UI, retention service, or automatic alerting.

## Trace a workflow

Enable tracing before starting the API and Java process:

```bash
COUNTERSEAL_TRACE=1 ./start.sh
```

The local launcher's API writes `.local-preview/api.log`. If an existing preview
is already running, stop its launcher normally before restarting with the flag.
This preserves its database. With the normal Java connection environment configured
(see [Java setup](../integrations/java-workflow/README.md)), run a fresh workflow:

```bash
COUNTERSEAL_TRACE=1 bash scripts/java-demo.sh --state=.workflow-state/traced \
  > workflow-result.json 2> java-trace.log
```

The Java workflow passes the trace setting to its gateway processes. Java relays
only validated gateway span fields; arbitrary upstream stderr remains discarded.
The `workflow.run` or `workflow.prepare` span contains the run ID to connect a
trace to the workflow result and console. Resuming creates a new invocation trace
for the same run; completed actions are not replayed just to reconstruct a trace.

Compose also accepts `COUNTERSEAL_TRACE=1` for its API and demo services. For an
existing container deployment, set it on both `./compose.sh up` and
`./compose.sh demo`, then read the API logs through `./compose.sh logs api` and
the demo's stderr. The container API stays loopback-only within its shared network
namespace; a metrics collector must have access to that namespace. Tracing does
not expose an additional port or automatically start Docker.

Extract spans from the two logs with `jq`:

```bash
jq -R 'fromjson? | select(.msg == "trace.span") |
  {time, service, name, trace_id, span_id, parent_span_id, duration_ms, outcome, run_id}' \
  java-trace.log .local-preview/api.log
```

Match `trace_id`, then connect each `parent_span_id` to its parent's `span_id`.
For a tool call the chain is:

```text
java: workflow.run (run_id)
  java: mcp.tool
    gateway: tools/call
      api: POST /v1/evaluate/action
```

Java control requests (issue, delegate, verify) also have child API spans. The
API span measures authentication, transaction waiting, evaluation, and commit;
the gateway span also includes upstream execution; the Java span includes the
MCP round trip. `time` is the completion timestamp and `duration_ms` is elapsed
time measured by the local monotonic clock. Cross-host timestamps require clock
synchronization for timeline comparisons.

Trace context uses the version-00
[W3C traceparent format](https://www.w3.org/TR/trace-context/): Java passes it in
MCP `_meta.traceparent`, the gateway creates a child and forwards it as an HTTP
header, and the API creates its child and returns the header. Invalid, zero, or
unsupported-version IDs start a fresh trace. Other metadata, baggage and
tracestate are not forwarded. Trace IDs are caller-controlled correlation data,
never identity, authority, or proof that two operations belong together.

Context propagation and trace IDs in action audit payloads remain active with
span logging disabled. New `action.evaluated` audit events include `trace_id` and
`span_id`; they are covered by the existing audit hash chain. Old audit events
remain valid. Logging is enabled only by `COUNTERSEAL_TRACE=1`; it records every
instrumented span, without a sampling system. Model-provider traffic and upstream
commerce internals are outside this trace boundary.

## Scrape metrics

`GET /metrics` requires the existing service bearer token. Operator sessions and
unauthenticated requests cannot scrape it. Metrics are in memory, reset on API
restart, and do not need a database connection to be read.

With the connection environment configured, pass the token to curl over stdin
rather than placing it in process arguments:

```bash
printf 'header = "Authorization: Bearer %s"\n' "$HANDOFFGUARD_API_TOKEN" |
  curl --config - --fail --silent --show-error "$HANDOFFGUARD_SERVER_URL/metrics"
```

| Metric | Meaning |
| --- | --- |
| `counterseal_http_requests_total{status_class}` | Known API endpoint requests, grouped by 1xx–5xx; excludes health, scrape requests, and unmatched routes |
| `counterseal_authorization_requests_total{outcome}` | Action evaluation requests: `allow`, `deny`, `rejected`, or `error` |
| `counterseal_authorization_duration_seconds` | Cumulative histogram of total action endpoint latency, including rejected and failed requests; excludes upstream execution |

`allow` and `deny` count only successfully committed policy decisions. Authentication
and malformed-input failures count as `rejected`; service/transaction failures
count as `error`, not denials. HTTP 403 alone does not imply a policy denial.
The total histogram includes database lock/queue and commit time, not just the
in-process policy comparison. It is instrumentation, not a new performance benchmark.

Metrics use fixed label sets. Tokens, usernames, run IDs, trace IDs, order IDs,
tool arguments, and arbitrary request paths never become labels. Span records
also exclude credentials, tool arguments, model prompts, and upstream results.
Gateway/upstream failures appear as `error` spans; the API metrics do not count
failures that occur before reaching the API or after its authorization completes.

Example configuration for an existing Prometheus installation:

```yaml
scrape_configs:
  - job_name: counterseal
    metrics_path: /metrics
    scheme: https
    authorization:
      type: Bearer
      credentials_file: /run/secrets/counterseal-service-token
    static_configs:
      - targets: ['your-counterseal-api.example:443']
```

For a collector on the same machine using a loopback API, use `scheme: http` and
its loopback address/port. The collector holding the service token is trusted;
this change does not create a separate metrics-only credential.

Useful PromQL expressions:

```promql
# Approximate p95 authorization endpoint latency over five minutes, in seconds.
histogram_quantile(0.95, sum by (le) (rate(counterseal_authorization_duration_seconds_bucket[5m])))

# Policy denials per second.
sum(rate(counterseal_authorization_requests_total{outcome="deny"}[5m]))

# Authorization service errors per second, separate from policy decisions.
sum(rate(counterseal_authorization_requests_total{outcome="error"}[5m]))
```

The endpoint follows the classic
[Prometheus text exposition format](https://prometheus.io/docs/instrumenting/exposition_formats/),
including cumulative buckets, `+Inf`, sum and count. No monitoring services are
automatically installed or started. Log retention/rotation, OTLP export, sampling,
collector deployment and alert thresholds remain deployment work.

## Verification

```bash
bash scripts/test-postgres.sh go test -race ./...
bash scripts/test-postgres.sh bash scripts/smoke-agents.sh
```

Go tests cover invalid trace context, MCP-to-HTTP propagation, metadata isolation,
audit correlation, scrape authentication, exact allow/deny/rejected/error counts,
and concurrent cumulative histograms. The Java harness enables tracing and verifies
actual Java → gateway → API parent-child spans through a real workflow. Normal
gateway, model, operator-approval and crash-recovery tests run alongside it.
