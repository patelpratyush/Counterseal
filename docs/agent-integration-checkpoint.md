# Agent integration checkpoint

The agent integration is implemented on `feat/agent-integration`, atop MCP gateway
commit `192875b`. The earlier setup checkpoint was `a8a8adc`.

## Implemented

- Real OpenAI Agents SDK Support → Billing → Notification workflow.
- Signed child-envelope issuance before each SDK handoff and a dedicated MCP
  gateway bound to each stage's identity and envelope.
- Narrowing permissions, inherited approval constraints, fixed resource scope,
  and a notification step gated on a successful refund receipt.
- Deterministic offline model, optional explicit live model, local sanitized SDK
  traces, and simulated refund/notification tools.
- Trusted operator approval option for the exact simulated refund, outside model
  tools; denial and upstream errors stop the workflow.
- Session ownership and cleanup, cancellation handling, and duplicate-stage guards.
- Locked Python dependencies and documented setup, trust boundaries, and limitations.

## Validation

`go test ./...` and `go vet ./...` passed. Seven integration tests passed against a
temporary PostgreSQL database and real API/gateway subprocesses:

```sh
python3 scripts/test-postgres.py python3 scripts/smoke-agents.py
```

These cover the complete chain and audit, missing approval, explicit approval,
expanded-authority handoff denial, premature handoff, repeated calls, and cancellation.
The documented offline `demo.py` command also completed with a valid audit and
graceful server shutdown. Live model execution has not been tested; no OpenAI API key was configured.

## Next

See [the integration guide](../integrations/openai-agents/README.md) for usage.
The remaining planned slice is production polish, including dashboard, CI gate,
Docker Compose, and benchmarks. This feature branch and its predecessor feature
branches are not merged into `master`.
