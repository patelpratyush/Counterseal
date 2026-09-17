# Agent integration checkpoint

Paused at the user's request on branch `feat/agent-integration`.

## Completed

- Created the Python project under `integrations/openai-agents` and resolved dependencies into `uv.lock`.
- Installed OpenAI Agents SDK 0.22.2 in a local Python 3.12 environment.
- Added `.venv/` to Git ignores. The virtual environment is not committed.
- Scoped a cryptography version constraint to Intel macOS to use an available binary wheel.

## Remaining

The integration adapter, agent workflow, notification demo tool, and integration tests have not been implemented. No live model call was made; `OPENAI_API_KEY` was not configured during setup.

Next implement a Support → Billing → Notification workflow using actual Agents SDK handoffs. Each handoff must obtain a signed child envelope from HandoffGuard before switching agents, and each agent must use a gateway session bound to its own envelope. Notification must require a successful simulated refund receipt. Human approval must remain outside model-accessible tools.

Provide a deterministic offline model for testing the real SDK, MCP gateway, and PostgreSQL path, plus an optional live model mode. Keep traces local during offline tests and exclude sensitive payloads. Ensure MCP sessions close in the task that opened them.

## Resume

Restore the environment with:

```sh
uv sync --directory integrations/openai-agents --python 3.12 --frozen
```

The completed MCP gateway is inherited from commit `192875b`. This branch and its predecessor feature branches have not been merged into `master`. Only dependency installation has been checked for this integration; workflow tests remain to be written and run.
