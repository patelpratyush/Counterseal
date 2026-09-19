# MCP gateway checkpoint — resumed and resolved

The paused checkpoint at `8bb0a30` has been completed on `feat/mcp-gateway`.
The two failing tests were caused by schema validation classifying `json.Number`
as a string. Validation now uses a schema engine that accepts precision-preserving
JSON numbers, while authorization and upstream forwarding retain the original
argument values.

Verified through unit/race tests, real MCP sessions against the PostgreSQL API,
Streamable HTTP forwarding, and the built CLI stdio demo. See
[gateway setup and limitations](gateway.md) for the completed implementation.

The gateway, server, and policy slices are now integrated into `master` with the
Java workflow, dashboard, CI, and Docker stack. See the [release checkpoint](release-checkpoint.md).
