package dev.handoffguard.workflow;

import java.security.SecureRandom;
import java.time.Instant;
import java.util.HexFormat;
import java.util.Map;
import static dev.handoffguard.workflow.ControlApi.JSON;

/** Local spans with version-00 W3C context. Correlation never grants authority. */
final class Trace implements AutoCloseable {
    private static final SecureRandom RANDOM = new SecureRandom();
    private static final ThreadLocal<Trace> CURRENT = new ThreadLocal<>();
    private static final boolean ENABLED = "1".equals(System.getenv("COUNTERSEAL_TRACE"));
    final String traceId, spanId, parentId;
    private final String name;
    private final Trace parent;
    private final long started = System.nanoTime();
    private String outcome = "error";
    private String runId = "";
    private Trace(String name) {
        this.name = name; parent = CURRENT.get();
        traceId = parent == null ? id(16) : parent.traceId;
        parentId = parent == null ? "" : parent.spanId;
        spanId = id(8); CURRENT.set(this);
    }
    static Trace start(String name) { return new Trace(name); }
    private static String id(int size) { var bytes = new byte[size]; RANDOM.nextBytes(bytes); return HexFormat.of().formatHex(bytes); }
    String header() { return "00-" + traceId + "-" + spanId + "-01"; }
    void outcome(String value) { outcome = value; }
    void run(String value) { if (value != null && value.matches("run_[a-zA-Z0-9_-]+")) runId = value; }
    @Override public void close() {
        if (parent == null) CURRENT.remove(); else CURRENT.set(parent);
        if (ENABLED) System.err.println(JSON.writeValueAsString(Map.of("time", Instant.now().toString(),
                "msg", "trace.span", "service", "java", "name", name, "trace_id", traceId,
                "span_id", spanId, "parent_span_id", parentId,
                "duration_ms", (System.nanoTime() - started) / 1_000_000.0, "outcome", outcome, "run_id", runId)));
    }

    // Never relay raw upstream stderr. Accept only the gateway's bounded span shape.
    static void gatewayLine(String line) {
        if (!ENABLED || line.length() > 2048) return;
        try {
            var node = JSON.readTree(line);
            if (!"trace.span".equals(node.path("msg").asText()) || !"gateway".equals(node.path("service").asText())
                    || !"tools/call".equals(node.path("name").asText())) return;
            String trace = node.path("trace_id").asText(), span = node.path("span_id").asText(), parent = node.path("parent_span_id").asText();
            String outcome = node.path("outcome").asText();
            double duration = node.path("duration_ms").asDouble(-1);
            if (!trace.matches("[0-9a-f]{32}") || !span.matches("[0-9a-f]{16}") || !parent.matches("([0-9a-f]{16})?")
                    || !java.util.Set.of("ok", "error", "deny", "rejected").contains(outcome)
                    || !Double.isFinite(duration) || duration < 0) return;
            String time = Instant.parse(node.path("time").asText()).toString();
            System.err.println(JSON.writeValueAsString(Map.of("time", time, "msg", "trace.span",
                    "service", "gateway", "name", "tools/call", "trace_id", trace, "span_id", span,
                    "parent_span_id", parent, "duration_ms", duration, "outcome", outcome)));
        } catch (RuntimeException ignored) { /* Untrusted stderr is discarded. */ }
    }
}
