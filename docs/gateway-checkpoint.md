# MCP gateway WIP checkpoint

Paused at the user's request. Branch: `feat/mcp-gateway`, based on
`feat/server-postgres` (`5aa8431`). Do not merge this checkpoint as a completed
feature.

Implemented so far:

- Official MCP Go SDK v1.8.0 dependency.
- Gateway stdio command with fixed agent/envelope identity.
- Stdio or Streamable HTTP upstream connection; minimal subprocess environment.
- Explicit JSON tool-to-action/resource/data-class mappings.
- Input schema checks, strict JSON decoding, fail-closed HTTP authorization,
  single forwarding attempt, and argument-free structured outcome logs.
- Simulated, deliberately unguarded commerce MCP server (`demo-mcp`).
- Initial gateway tests and example mapping.

Latest verification:

`go test ./internal/gateway ./cmd/cli` compiles. CLI tests pass; gateway tests fail:

- `TestAllowAndExactArguments`: allowed call not forwarded.
- `TestUpstreamErrorNoRetryAndSafeLogs`: retry or failure missing.

Resume by inspecting the tool error results in these tests. Input decoding uses
`json.Number`; verify whether the schema validator accepts that representation
before changing it. Numeric precision and exact authorization/forwarded argument
matching must be preserved. The cause has not yet been established.

Remaining work:

1. Resolve the failures and test protocol/error/cancellation behavior.
2. Exercise actual MCP sessions through the gateway against the Postgres API,
   including deny, approval, allow, replay, revoked envelopes, and unmapped tools.
3. Test the built CLI over stdio and Streamable HTTP; check session lifetime
   after the startup context is canceled and ensure upstream cleanup.
4. Add gateway setup/security documentation and a runnable guarded demo.
5. Run race tests, vet, and the Postgres integration suite; commit the completed
   feature separately.

No gateway or database service was started for this WIP. Prior isolated Postgres
integration clusters were stopped and removed by their test helpers.
