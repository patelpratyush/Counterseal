package dev.handoffguard.workflow;

import java.nio.file.Files;
import java.time.Instant;
import java.util.List;
import java.util.Map;
import java.util.Set;
import static dev.handoffguard.workflow.ControlApi.JSON;

/** Simulated comparison demos, also exercised by the integration suite. */
final class DemoScenarios {
    private static final Map<String,Object> REFUND = Map.of("order_id", "48319", "amount", 825);
    private DemoScenarios() {}
    static void require(boolean condition, String message) { if (!condition) throw new IllegalStateException(message); }
    static void approve(ControlApi api, String envelopeId) {
        api.post("/v1/approvals", JSON.valueToTree(Map.of("envelope_id", envelopeId, "action", "refunds.create",
                "resource", "orders:48319", "arguments", REFUND, "approved_by", "demo-operator",
                "role", "refund_manager", "expires_at", Instant.now().plusSeconds(600).toString())), 201);
    }
    static void server(ControlApi api) {
        var root = api.post("/v1/envelopes", JSON.createObjectNode().set("envelope", Workflow.rootEnvelope()), 201);
        String id = root.path("envelope").path("id").asText();
        var action = JSON.valueToTree(Map.of("envelope_id", id, "agent_id", "support-agent", "tool", "refunds.create",
                "resources", Map.of("orders", List.of("48319")), "data_classes", List.of("payment_metadata"), "arguments", REFUND));
        api.post("/v1/evaluate/action", action, 403);
        approve(api, id);
        api.post("/v1/evaluate/action", action, 200);
        api.post("/v1/evaluate/action", action, 403);
        require("VALID".equals(api.post("/v1/audit/" + root.path("run_id").asText() + "/verify", null, 200).path("status").asText()), "Audit invalid");
        System.out.println("PASS: server approval required, consumed once, replay denied, audit VALID");
    }
    static void gateway(String binary, String base, String token, ControlApi api) {
        try (var direct = new GatewaySession(binary, List.of("demo-mcp"), Map.of())) {
            require(!Boolean.TRUE.equals(direct.call("refund.create", REFUND).isError()), "Direct simulated refund failed");
        }
        System.out.println("Unsafe upstream: simulated 825 refund succeeds without approval");
        var root = api.post("/v1/envelopes", JSON.createObjectNode().set("envelope", Workflow.rootEnvelope()), 201);
        String id = root.path("envelope").path("id").asText();
        try {
            var config = Files.createTempFile("hg-gateway-demo-", ".json");
            try {
                JSON.writeValue(config.toFile(), Map.of("tools", Map.of(
                    "refund.create", Map.of("action", "refunds.create", "resources", Map.of("orders", "/order_id"), "data_classes", List.of("payment_metadata")),
                    "orders.get", Map.of("action", "orders.read", "resources", Map.of("orders", "/order_id"), "data_classes", List.of("payment_metadata")))));
                try (var guarded = new GatewaySession(binary, List.of("gateway", "--config", config.toString(), "--agent", "support-agent",
                        "--envelope", id, "--api-url", base, "--", binary, "demo-mcp"), Map.of("HANDOFFGUARD_API_TOKEN", token))) {
                    require(Set.copyOf(guarded.tools()).equals(Set.of("refund.create", "orders.get")), "Unexpected tool surface");
                    boolean hidden = false;
                    try { hidden = Boolean.TRUE.equals(guarded.call("payment.export", Map.of()).isError()); }
                    catch (RuntimeException denied) { hidden = true; }
                    require(hidden, "Unmapped tool was accessible");
                    require(Boolean.TRUE.equals(guarded.call("refund.create", REFUND).isError()), "Missing approval allowed");
                    approve(api, id);
                    var result = guarded.call("refund.create", REFUND);
                    require(!Boolean.TRUE.equals(result.isError()), "Approved refund denied");
                    require("simulated_1".equals(JSON.valueToTree(result.structuredContent()).path("refund_id").asText()), "Denied call reached upstream");
                    require(Boolean.TRUE.equals(guarded.call("refund.create", REFUND).isError()), "Approval replay allowed");
                    require(!Boolean.TRUE.equals(guarded.call("orders.get", Map.of("order_id", "48319")).isError()), "Order read failed");
                    api.post("/v1/envelopes/" + id + "/revoke", null, 200);
                    require(Boolean.TRUE.equals(guarded.call("orders.get", Map.of("order_id", "48319")).isError()), "Revocation ignored");
                }
            } finally { Files.deleteIfExists(config); }
        } catch (java.io.IOException error) { throw new IllegalStateException("Demo configuration failed", error); }
        require("VALID".equals(api.post("/v1/audit/" + root.path("run_id").asText() + "/verify", null, 200).path("status").asText()), "Audit invalid");
        System.out.println("PASS: gateway approval, replay, revocation, hidden tools, audit VALID");
    }
}
