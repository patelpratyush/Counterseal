package dev.handoffguard.workflow;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.EnumMap;
import java.util.EnumSet;
import java.util.List;
import java.util.Map;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.node.ObjectNode;
import static dev.handoffguard.workflow.ControlApi.JSON;

/** A single-use, sequential Support → Billing → Notification orchestration. */
public class Workflow implements AutoCloseable {
    private final String binary, baseUrl, token;
    private final int amount;
    private final boolean approve;
    private final ControlApi api;
    private final Path directory;
    private final Map<Stage, JsonNode> envelopes = new EnumMap<>(Stage.class);
    private final Map<Stage, JsonNode> receipts = new EnumMap<>(Stage.class);
    private final Map<Stage, GatewaySession> gateways = new EnumMap<>(Stage.class);
    private final EnumSet<Stage> attempted = EnumSet.noneOf(Stage.class);
    private final List<Event> events = new ArrayList<>();
    private String runId;
    private boolean started, closed, failed;

    public record Event(String type, String runId, Stage stage) {}
    public record Result(String runId, Map<Stage, JsonNode> receipts, JsonNode audit, List<Event> events) {}

    public Workflow(String binary, String baseUrl, String token, int amount, boolean approve) throws IOException {
        if (amount <= 0) throw new IllegalArgumentException("Amount must be a positive integer");
        this.binary = Path.of(binary).toAbsolutePath().toString();
        this.baseUrl = baseUrl; this.token = token; this.amount = amount; this.approve = approve;
        this.api = new ControlApi(baseUrl, token);
        try { this.directory = Files.createTempDirectory("hg-java-"); }
        catch (IOException error) { api.close(); throw error; }
    }

    public synchronized Result run() {
        try {
            start();
            execute(Stage.SUPPORT, arguments(Stage.SUPPORT));
            transition(Stage.SUPPORT, Stage.BILLING);
            execute(Stage.BILLING, arguments(Stage.BILLING));
            transition(Stage.BILLING, Stage.NOTIFICATION);
            execute(Stage.NOTIFICATION, arguments(Stage.NOTIFICATION));
            var audit = api.post("/v1/audit/" + runId + "/verify", null, 200);
            if (!"VALID".equals(audit.path("status").asText())) throw new IllegalStateException("Audit verification failed");
            return new Result(runId, receipts(), audit, List.copyOf(events));
        } catch (RuntimeException error) { failed = true; throw error; }
    }

    synchronized void start() {
        ensureOpen();
        if (started) throw new IllegalStateException("Create a new Workflow for each run");
        started = true;
        var root = api.post("/v1/envelopes", JSON.createObjectNode().set("envelope", rootEnvelope()), 201);
        runId = root.path("run_id").asText();
        envelopes.put(Stage.SUPPORT, root.path("envelope"));
        connect(Stage.SUPPORT);
        events.add(new Event("started", runId, Stage.SUPPORT));
    }

    static ObjectNode rootEnvelope() {
        var root = (ObjectNode) JSON.readTree("""
            {"version":"1","issuer":{"agent":"demo-operator"},"recipient":{"agent":"support-agent"},
             "purpose":"customer_refund","policy_version":"refund-v1",
             "allowed_actions":["orders.read","refunds.create","email.send"],"denied_actions":["payments.export"],
             "resources":{"orders":["48319"]},"data_classes":["payment_metadata"],
             "approvals":[{"condition":"has(refund.amount) && refund.amount > 500","required_role":"refund_manager"}],
             "delegation":{"max_depth":2,"current_depth":0,"may_expand_authority":false}}
            """);
        root.put("expires_at", Instant.now().plusSeconds(3600).toString());
        return root;
    }

    public synchronized Map<String, Object> arguments(Stage stage) {
        return switch (stage) {
            case SUPPORT -> Map.of("order_id", "48319");
            case BILLING -> Map.of("order_id", "48319", "amount", amount);
            case NOTIFICATION -> {
                if (!receipts.containsKey(Stage.BILLING)) throw new IllegalStateException("Refund receipt required");
                yield Map.of("order_id", "48319", "refund_id", receipts.get(Stage.BILLING).path("refund_id").asText());
            }
        };
    }

    protected ObjectNode child(Stage parentStage, Stage stage) {
        var parent = envelopes.get(parentStage);
        ObjectNode child = (ObjectNode) parent.deepCopy();
        child.remove(List.of("id", "signature"));
        child.set("issuer", parent.path("recipient"));
        child.set("recipient", JSON.createObjectNode().put("agent", stage.agent));
        child.put("parent_envelope", parent.path("id").asText());
        child.set("allowed_actions", JSON.valueToTree(stage == Stage.BILLING
                ? List.of("refunds.create", "email.send") : List.of("email.send")));
        ((ObjectNode) child.path("delegation")).put("current_depth", parent.path("delegation").path("current_depth").asInt() + 1);
        child.put("expires_at", Instant.parse(parent.path("expires_at").asText()).minusSeconds(60).toString());
        return child;
    }

    public synchronized void transition(Stage parentStage, Stage stage) {
        ensureOpen();
        if (stage.ordinal() != parentStage.ordinal() + 1 || !receipts.containsKey(parentStage) || envelopes.containsKey(stage)) {
            throw new IllegalStateException("Handoff requires a completed preceding stage and a fresh child");
        }
        var result = api.post("/v1/envelopes/" + envelopes.get(parentStage).path("id").asText() + "/delegate",
                JSON.createObjectNode().set("child", child(parentStage, stage)), 201);
        envelopes.put(stage, result.path("envelope"));
        if (stage == Stage.BILLING && approve) {
            api.post("/v1/approvals", JSON.valueToTree(Map.of("envelope_id", result.path("envelope").path("id").asText(),
                    "action", "refunds.create", "resource", "orders:48319", "arguments", arguments(stage),
                    "approved_by", "demo-operator", "role", "refund_manager", "expires_at", Instant.now().plusSeconds(600).toString())), 201);
        }
        connect(stage);
        events.add(new Event("handoff", runId, stage));
    }

    public synchronized JsonNode execute(Stage stage, Map<String, Object> arguments) {
        ensureOpen();
        if (!gateways.containsKey(stage)) throw new IllegalStateException("Stage has not been delegated");
        if (!arguments(stage).equals(arguments)) throw new IllegalArgumentException("Tool arguments differ from the authorized request");
        if (!attempted.add(stage)) throw new IllegalStateException("This workflow stage has already been attempted");
        try {
            var result = gateways.get(stage).call(stage.tool, arguments);
            if (Boolean.TRUE.equals(result.isError())) throw new IllegalStateException("HandoffGuard denied the tool call or upstream execution failed");
            JsonNode receipt = JSON.valueToTree(result.structuredContent());
            validateReceipt(stage, receipt, arguments);
            receipts.put(stage, receipt);
            events.add(new Event("tool_completed", runId, stage));
            return receipt.deepCopy();
        } catch (RuntimeException error) { failed = true; throw error; }
    }

    static void validateReceipt(Stage stage, JsonNode receipt, Map<String, Object> arguments) {
        if (receipt == null || !receipt.isObject() || !receipt.path(stage.receiptKey).isTextual()
                || receipt.path(stage.receiptKey).asText().isBlank() || !"48319".equals(receipt.path("order_id").asText())) {
            throw new IllegalStateException("Upstream returned no valid simulated receipt");
        }
        if (stage == Stage.BILLING && (!receipt.path("amount").isIntegralNumber()
                || receipt.path("amount").asLong() != ((Number) arguments.get("amount")).longValue())) {
            throw new IllegalStateException("Refund receipt amount does not match the request");
        }
        if (stage == Stage.NOTIFICATION && !receipt.path("refund_id").asText().equals(arguments.get("refund_id"))) {
            throw new IllegalStateException("Notification receipt does not match the refund");
        }
    }

    private void connect(Stage stage) {
        try {
            Path config = directory.resolve(stage.name() + ".json");
            JSON.writeValue(config.toFile(), Map.of("tools", Map.of(stage.tool, Map.of("action", stage.action,
                    "resources", Map.of("orders", "/order_id"), "data_classes", List.of("payment_metadata")))));
            var gateway = new GatewaySession(binary, List.of("gateway", "--config", config.toString(),
                    "--agent", stage.agent, "--envelope", envelopes.get(stage).path("id").asText(),
                    "--api-url", baseUrl, "--", binary, "demo-mcp"), Map.of("HANDOFFGUARD_API_TOKEN", token));
            gateways.put(stage, gateway);
            if (!gateway.tools().equals(List.of(stage.tool))) throw new IllegalStateException("Unexpected gateway tool surface");
        } catch (RuntimeException error) { throw new IllegalStateException("Cannot prepare gateway session", error); }
    }

    public synchronized String runId() { return runId; }
    public synchronized Map<Stage, JsonNode> receipts() { return copies(receipts); }
    public synchronized Map<Stage, JsonNode> envelopes() { return copies(envelopes); }
    public synchronized List<Event> events() { return List.copyOf(events); }
    synchronized boolean sessionsClosed() { return gateways.values().stream().allMatch(GatewaySession::isClosed); }
    synchronized Path temporaryDirectory() { return directory; }
    private static Map<Stage, JsonNode> copies(Map<Stage, JsonNode> source) {
        var result = new EnumMap<Stage, JsonNode>(Stage.class);
        source.forEach((stage, json) -> result.put(stage, json.deepCopy()));
        return Map.copyOf(result);
    }
    private void ensureOpen() {
        if (closed || failed || Thread.currentThread().isInterrupted()) throw new IllegalStateException("Workflow is closed, failed, or interrupted");
    }
    @Override public synchronized void close() {
        if (closed) return;
        closed = true;
        RuntimeException failure = null;
        for (var session : gateways.values()) {
            try { session.close(); }
            catch (RuntimeException error) {
                if (failure == null) failure = error;
                else failure.addSuppressed(error);
            }
        }
        try { api.close(); }
        catch (RuntimeException error) {
            if (failure == null) failure = error;
            else failure.addSuppressed(error);
        }
        try (var paths = Files.walk(directory)) {
            for (var path : paths.sorted(Comparator.reverseOrder()).toList()) Files.deleteIfExists(path);
        } catch (IOException error) {
            if (failure == null) failure = new IllegalStateException("Temporary gateway cleanup failed", error);
            else failure.addSuppressed(error);
        }
        if (failure != null) throw failure;
    }
}
