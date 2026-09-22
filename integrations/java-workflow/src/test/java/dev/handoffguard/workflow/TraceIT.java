package dev.handoffguard.workflow;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import tools.jackson.databind.JsonNode;
import org.junit.jupiter.api.Test;
import static dev.handoffguard.workflow.ControlApi.JSON;
import static org.junit.jupiter.api.Assertions.*;
import static org.junit.jupiter.api.Assumptions.assumeTrue;

class TraceIT {
    @Test void oneTraceLinksJavaMcpGatewayAndApiWithNoCredentialsInSpans() throws Exception {
        assumeTrue("1".equals(System.getenv("COUNTERSEAL_TRACE")));
        var captured = new ByteArrayOutputStream();
        var previous = System.err;
        String traceId;
        try (var output = new PrintStream(captured, true, StandardCharsets.UTF_8)) {
            System.setErr(output);
            try (var root = Trace.start("integration.trace"); var workflow = new Workflow(
                    System.getenv("HANDOFFGUARD_BINARY"), System.getenv("HANDOFFGUARD_SERVER_URL"),
                    System.getenv("HANDOFFGUARD_API_TOKEN"), 100, false)) {
                traceId = root.traceId;
                assertEquals("VALID", workflow.run().audit().path("status").asText());
                root.outcome("ok");
            } finally { System.setErr(previous); }
        }
        var spans = spans(captured.toString(StandardCharsets.UTF_8), traceId);
        var javaCalls = spans.stream().filter(s -> s.path("service").asText().equals("java") && s.path("name").asText().equals("mcp.tool")).toList();
        var gateways = spans.stream().filter(s -> s.path("service").asText().equals("gateway")).toList();
        assertEquals(3, javaCalls.size()); assertEquals(3, gateways.size());
        var api = spans(Files.readString(Path.of(System.getenv("HANDOFFGUARD_TEST_API_LOG"))), traceId);
        var evaluations = api.stream().filter(s -> s.path("name").asText().equals("POST /v1/evaluate/action")).toList();
        assertEquals(3, evaluations.size());
        for (var gateway : gateways) {
            assertTrue(javaCalls.stream().anyMatch(j -> j.path("span_id").equals(gateway.path("parent_span_id"))));
            assertTrue(evaluations.stream().anyMatch(a -> a.path("parent_span_id").equals(gateway.path("span_id"))));
        }
        spans.addAll(api);
        for (var span : spans) {
            assertTrue(span.path("duration_ms").asDouble(-1) >= 0);
            assertFalse(span.toString().contains(System.getenv("HANDOFFGUARD_API_TOKEN")));
            assertFalse(span.has("order_id"));
            assertFalse(span.has("arguments"));
        }
    }
    private static List<JsonNode> spans(String text, String traceId) {
        var result = new ArrayList<JsonNode>();
        for (String line : text.lines().toList()) {
            try { var node = JSON.readTree(line); if (node != null && "trace.span".equals(node.path("msg").asText()) && traceId.equals(node.path("trace_id").asText())) result.add(node); }
            catch (RuntimeException ignored) { /* Ignore normal startup logs. */ }
        }
        return result;
    }
}
