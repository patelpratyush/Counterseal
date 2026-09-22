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
    private final WorkflowStore store;
    private final Map<Stage, JsonNode> envelopes = new EnumMap<>(Stage.class);
    private final Map<Stage, JsonNode> receipts = new EnumMap<>(Stage.class);
    private final Map<Stage, GatewaySession> gateways = new EnumMap<>(Stage.class);
    private final EnumSet<Stage> attempted = EnumSet.noneOf(Stage.class);
    private ToolProposer proposer;
    private final Map<Stage, JsonNode> proposals = new EnumMap<>(Stage.class);
    private final List<Event> events = new ArrayList<>();
    private String runId;
    private String pending = "";
    private boolean started, closed, failed, approvalGranted;

    public record Event(String type, String runId, Stage stage) {}
    public record Result(String runId, Map<Stage, JsonNode> receipts, JsonNode audit, List<Event> events) {}

    public Workflow(String binary, String baseUrl, String token, int amount, boolean approve) throws IOException {
        this(binary, baseUrl, token, amount, approve, null, false);
    }

    public Workflow(String binary, String baseUrl, String token, int amount, boolean approve,
                    Path stateDirectory, boolean resume) throws IOException {
        if (amount <= 0) throw new IllegalArgumentException("Amount must be a positive integer");
        if (resume && stateDirectory == null) throw new IllegalArgumentException("Resume requires a state directory");
        this.binary = Path.of(binary).toAbsolutePath().toString();
        this.baseUrl = java.net.URI.create(baseUrl).resolve("/").toString(); this.token = token;
        this.api = new ControlApi(baseUrl, token);
        WorkflowStore opened = null;
        try {
            opened = stateDirectory == null ? null : new WorkflowStore(stateDirectory);
            if (opened != null && !resume && opened.exists()) {
                throw new IllegalStateException("Checkpoint already exists; use --resume --state=PATH");
            }
            var saved = resume ? opened.read() : null;
            if (resume && saved == null) throw new IllegalStateException("Empty checkpoint; refusing to create a new run");
            this.amount = saved == null ? amount : saved.amount();
            this.approve = saved == null ? approve : saved.approve();
            if (saved != null) restore(saved);
            this.store = opened;
            checkpoint();
            this.directory = Files.createTempDirectory("hg-java-");
        } catch (IOException | RuntimeException error) {
            if (opened != null) {
                try { opened.close(); } catch (IOException closeError) { error.addSuppressed(closeError); }
            }
            api.close();
            throw error;
        }
    }

    public synchronized Result run() {
        try (var trace = Trace.start("workflow.run")) {
            start();
            trace.run(runId);
            for (var stage : Stage.values()) {
                if (receipts.containsKey(stage)) continue;
                if (!envelopes.containsKey(stage)) transition(Stage.values()[stage.ordinal() - 1], stage);
                executeProposal(stage);
            }
            var audit = api.post("/v1/audit/" + runId + "/verify", null, 200);
            if (!"VALID".equals(audit.path("status").asText())) throw new IllegalStateException("Audit verification failed");
            trace.outcome("ok");
            return new Result(runId, receipts(), audit, List.copyOf(events));
        } catch (RuntimeException error) { failed = true; throw error; }
    }

    /** Stop before attempting Billing, so an operator can approve the exact refund in the console. */
    public synchronized Map<String, Object> prepareApproval() {
        try (var trace = Trace.start("workflow.prepare")) {
            start();
            trace.run(runId);
            executeProposal(Stage.SUPPORT);
            transition(Stage.SUPPORT, Stage.BILLING);
            var proposed = proposedArguments(Stage.BILLING);
            if (!arguments(Stage.BILLING).equals(proposed)) throw new IllegalStateException("Approval proposal differs from the requested refund");
            trace.outcome("ok");
            return Map.of("status", "AWAITING_OPERATOR_APPROVAL", "run_id", runId,
                    "envelope_id", envelopes.get(Stage.BILLING).path("id").asText(),
                    "arguments", proposed);
        } catch (RuntimeException error) { failed = true; throw error; }
    }

    synchronized void start() {
        ensureOpen();
        if (proposer == null && !proposals.isEmpty()) {
            for (var stage : Stage.values()) {
                if (!receipts.containsKey(stage) && !proposals.containsKey(stage))
                    throw new IllegalStateException("Resume this model workflow with --live-model; no deterministic fallback is allowed");
            }
        }
        if (started) throw new IllegalStateException("Create a new Workflow for each run");
        started = true;
        if (runId != null) {
            // Confirm the original server still has these exact signed envelopes before continuing.
            var chain = api.get("/v1/runs/" + runId + "/chain");
            for (var envelope : envelopes.values()) {
                boolean found = false;
                for (var stored : chain.path("envelopes")) {
                    if (stored.path("envelope").equals(envelope)) { found = true; break; }
                }
                if (!found) throw new IllegalStateException("Checkpoint does not match the stored run");
            }
            events.add(new Event("resumed", runId, null));
            checkpoint();
            return;
        }
        begin("CREATE_RUN");
        var root = api.post("/v1/envelopes", JSON.createObjectNode().set("envelope", rootEnvelope()), 201);
        runId = root.path("run_id").asText();
        envelopes.put(Stage.SUPPORT, root.path("envelope"));
        events.add(new Event("started", runId, Stage.SUPPORT));
        complete();
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
        begin("DELEGATE_" + stage);
        var result = api.post("/v1/envelopes/" + envelopes.get(parentStage).path("id").asText() + "/delegate",
                JSON.createObjectNode().set("child", child(parentStage, stage)), 201);
        envelopes.put(stage, result.path("envelope"));
        events.add(new Event("handoff", runId, stage));
        complete();
    }

    public synchronized JsonNode execute(Stage stage, Map<String, Object> arguments) {
        if (!arguments(stage).equals(arguments)) throw new IllegalArgumentException("Tool arguments differ from the authorized request");
        return executeChecked(stage, arguments);
    }

    private JsonNode executeChecked(Stage stage, Map<String, Object> arguments) {
        ensureOpen();
        if (!envelopes.containsKey(stage)) throw new IllegalStateException("Stage has not been delegated");
        if (attempted.contains(stage)) throw new IllegalStateException("This workflow stage has already been attempted");
        try {
            if (!gateways.containsKey(stage)) connect(stage);
            if (stage == Stage.BILLING && approve && !approvalGranted) {
                begin("APPROVE_BILLING");
                api.post("/v1/approvals", JSON.valueToTree(Map.of("envelope_id", envelopes.get(stage).path("id").asText(),
                        "action", "refunds.create", "resource", "orders:48319", "arguments", arguments,
                        "approved_by", "demo-operator", "role", "refund_manager", "expires_at", Instant.now().plusSeconds(600).toString())), 201);
                approvalGranted = true;
                complete();
            }
            attempted.add(stage);
            begin("EXECUTE_" + stage);
            var result = callTool(stage, arguments);
            if (Boolean.TRUE.equals(result.isError())) throw new IllegalStateException("Counterseal denied the tool call or upstream execution failed");
            JsonNode receipt = JSON.valueToTree(result.structuredContent());
            validateReceipt(stage, receipt, arguments);
            receipts.put(stage, receipt);
            events.add(new Event("tool_completed", runId, stage));
            complete();
            return receipt.deepCopy();
        } catch (RuntimeException error) { failed = true; throw error; }
    }

    protected io.modelcontextprotocol.spec.McpSchema.CallToolResult callTool(Stage stage, Map<String, Object> arguments) {
        return gateways.get(stage).call(stage.tool, arguments);
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
                    "--api-url", baseUrl, "--", binary, "demo-mcp"), Map.of("HANDOFFGUARD_API_TOKEN", token, "COUNTERSEAL_TRACE", System.getenv().getOrDefault("COUNTERSEAL_TRACE", "0")));
            gateways.put(stage, gateway);
            if (!gateway.tools().equals(List.of(stage.tool))) throw new IllegalStateException("Unexpected gateway tool surface");
        } catch (RuntimeException error) { throw new IllegalStateException("Cannot prepare gateway session", error); }
    }

    synchronized void useModel(ToolProposer proposer) {
        ensureOpen();
        if (started) throw new IllegalStateException("Configure the model before starting");
        this.proposer = java.util.Objects.requireNonNull(proposer);
    }

    private Map<String, Object> proposedArguments(Stage stage) {
        var expected = arguments(stage);
        if (!proposals.containsKey(stage)) {
            if (proposer == null) return expected;
            var proposal = proposer.propose(stage, expected);
            validateProposal(expected, proposal);
            proposals.put(stage, JSON.valueToTree(proposal));
            events.add(new Event("model_proposed", runId, stage));
            checkpoint(); // Pin the exact proposal before any tool call or human approval.
        }
        @SuppressWarnings("unchecked")
        Map<String, Object> proposal = JSON.convertValue(proposals.get(stage), Map.class);
        validateProposal(expected, proposal);
        return proposal;
    }

    static void validateProposal(Map<String, Object> expected, Map<String, Object> proposal) {
        if (proposal == null || !proposal.keySet().equals(expected.keySet())
                || !(proposal.get("order_id") instanceof String order) || !order.matches("[0-9]{1,32}"))
            throw new IllegalStateException("Invalid model proposal fields");
        // Order scope is independently enforced by the gateway; business values remain pinned.
        for (var field : expected.keySet()) {
            if (!field.equals("order_id") && !expected.get(field).equals(proposal.get(field)))
                throw new IllegalStateException("Model changed the requested amount or receipt");
        }
    }

    private void executeProposal(Stage stage) {
        var proposal = proposedArguments(stage);
        if (arguments(stage).equals(proposal)) execute(stage, proposal);
        else executeChecked(stage, proposal);
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
        if (closed || failed || !pending.isEmpty() || Thread.currentThread().isInterrupted()) throw new IllegalStateException("Workflow is closed, failed, interrupted, or needs reconciliation");
    }

    private void begin(String operation) { pending = operation; checkpoint(); }
    private void complete() { pending = ""; checkpoint(); }
    private void checkpoint() {
        if (store == null) return;
        try {
            store.save(new WorkflowStore.Snapshot(1, baseUrl, amount, approve, runId, envelopes, receipts,
                    attempted, events, approvalGranted, pending, proposals));
        } catch (RuntimeException error) { failed = true; throw error; }
    }

    private void restore(WorkflowStore.Snapshot saved) {
        if (saved == null || saved.version() != 1 || !baseUrl.equals(saved.baseUrl()) || saved.amount() <= 0
                || saved.envelopes() == null || saved.receipts() == null || saved.attempted() == null
                || saved.events() == null || saved.pending() == null) {
            throw new IllegalStateException("Invalid checkpoint or different control API origin");
        }
        if (!saved.pending().isEmpty()) {
            throw new IllegalStateException("RECONCILIATION_REQUIRED: " + saved.pending()
                    + "; the outcome may be unknown. Do not retry or delete this checkpoint. Run: " + saved.runId());
        }
        runId = saved.runId();
        envelopes.putAll(saved.envelopes());
        receipts.putAll(saved.receipts());
        attempted.addAll(saved.attempted());
        events.addAll(saved.events());
        approvalGranted = saved.approvalGranted();
        if (saved.proposals() != null) proposals.putAll(saved.proposals());
        if ((runId == null) != envelopes.isEmpty() || (runId != null && !runId.matches("run_[a-zA-Z0-9_-]+"))
                || !attempted.equals(receipts.keySet()) || !envelopes.keySet().containsAll(receipts.keySet())
                || (approvalGranted && (!approve || !envelopes.containsKey(Stage.BILLING)))) {
            throw new IllegalStateException("Inconsistent checkpoint; refusing to replay operations");
        }
        for (var stage : Stage.values()) {
            if (envelopes.containsKey(stage)) {
                var envelope = envelopes.get(stage);
                if (envelope == null || !envelope.path("id").asText().matches("env_[a-zA-Z0-9_-]+")
                        || !stage.agent.equals(envelope.path("recipient").path("agent").asText())
                        || (stage.ordinal() > 0 && !receipts.containsKey(Stage.values()[stage.ordinal() - 1]))) {
                    throw new IllegalStateException("Invalid stage ordering in checkpoint");
                }
            }
            if (receipts.containsKey(stage)) validateReceipt(stage, receipts.get(stage), arguments(stage));
        }
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
        if (store != null) {
            try { store.close(); }
            catch (IOException error) {
                if (failure == null) failure = new IllegalStateException("Workflow lock cleanup failed", error);
                else failure.addSuppressed(error);
            }
        }
        if (failure != null) throw failure;
    }
}
