package dev.handoffguard.workflow;

import io.modelcontextprotocol.client.McpClient;
import io.modelcontextprotocol.client.McpSyncClient;
import io.modelcontextprotocol.client.transport.ServerParameters;
import io.modelcontextprotocol.client.transport.StdioClientTransport;
import io.modelcontextprotocol.json.McpJsonDefaults;
import io.modelcontextprotocol.spec.McpSchema.CallToolRequest;
import io.modelcontextprotocol.spec.McpSchema.CallToolResult;
import java.time.Duration;
import java.util.List;
import java.util.Map;

/** Owns one real MCP SDK session and its subprocess transport. No call retries. */
public final class GatewaySession implements AutoCloseable {
    private final McpSyncClient client;
    private boolean closed;

    public GatewaySession(String binary, List<String> arguments, Map<String, String> environment) {
        var parameters = ServerParameters.builder(binary).args(arguments).env(environment).build();
        var transport = new StdioClientTransport(parameters, McpJsonDefaults.getMapper());
        // Never forward arbitrary upstream stderr into application logs.
        transport.setStdErrorHandler(Trace::gatewayLine);
        client = McpClient.sync(transport).requestTimeout(Duration.ofSeconds(35)).build();
        try { client.initialize(); }
        catch (RuntimeException error) { close(); throw error; }
    }

    public List<String> tools() { return client.listTools().tools().stream().map(tool -> tool.name()).toList(); }
    public CallToolResult call(String name, Map<String, Object> arguments) {
        if (closed) throw new IllegalStateException("Gateway is closed");
        try (var trace = Trace.start("mcp.tool")) {
            var result = client.callTool(CallToolRequest.builder(name).arguments(arguments)
                    .meta(Map.of("traceparent", trace.header())).build());
            trace.outcome(Boolean.TRUE.equals(result.isError()) ? "error" : "ok");
            return result;
        }
    }
    public boolean isClosed() { return closed; }
    @Override public void close() {
        if (!closed) {
            closed = client.closeGracefully();
            if (!closed) throw new IllegalStateException("Gateway shutdown did not complete within the SDK timeout");
        }
    }
}
