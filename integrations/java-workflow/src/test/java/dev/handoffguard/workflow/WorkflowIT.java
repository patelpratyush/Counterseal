package dev.handoffguard.workflow;

import java.nio.file.Files;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import tools.jackson.databind.node.ObjectNode;
import static org.junit.jupiter.api.Assertions.*;

@Timeout(value = 90, unit = TimeUnit.SECONDS)
class WorkflowIT {
    private String env(String name) { return java.util.Objects.requireNonNull(System.getenv(name), "Integration test requires " + name); }
    private Workflow workflow(int amount, boolean approve) throws Exception {
        return new Workflow(env("HANDOFFGUARD_BINARY"), env("HANDOFFGUARD_SERVER_URL"), env("HANDOFFGUARD_API_TOKEN"), amount, approve);
    }
    private ControlApi api() { return new ControlApi(env("HANDOFFGUARD_SERVER_URL"), env("HANDOFFGUARD_API_TOKEN")); }

    @Test void completeChainAndAudit() throws Exception {
        var workflow = workflow(100, false);
        try (workflow; var api = api()) {
            var result = workflow.run();
            assertEquals(Set.of(Stage.SUPPORT, Stage.BILLING, Stage.NOTIFICATION), result.receipts().keySet());
            var chain = api.get("/v1/runs/" + workflow.runId() + "/chain");
            assertEquals(3, chain.path("envelopes").size());
            assertEquals(2, chain.path("handoffs").size());
            assertEquals(3, chain.path("actions").size());
            assertEquals("email.send", workflow.envelopes().get(Stage.NOTIFICATION).path("allowed_actions").get(0).asText());
            assertEquals(1, workflow.envelopes().get(Stage.NOTIFICATION).path("allowed_actions").size());
            assertEquals("VALID", result.audit().path("status").asText());
            assertEquals(2, result.events().stream().filter(e -> e.type().equals("handoff")).count());
            assertFalse(ControlApi.JSON.writeValueAsString(result.events()).contains(env("HANDOFFGUARD_API_TOKEN")));
        }
        assertTrue(workflow.sessionsClosed());
        assertFalse(Files.exists(workflow.temporaryDirectory()));
    }
    @Test void missingApprovalStopsBeforeNotification() throws Exception {
        var workflow = workflow(825, false);
        try (workflow; var api = api()) {
            assertThrows(IllegalStateException.class, workflow::run);
            assertEquals(Set.of(Stage.SUPPORT), workflow.receipts().keySet());
            assertFalse(workflow.envelopes().containsKey(Stage.NOTIFICATION));
            var chain = api.get("/v1/runs/" + workflow.runId() + "/chain");
            assertEquals(2, chain.path("actions").size());
            assertEquals(2, chain.path("envelopes").size());
        }
        assertTrue(workflow.sessionsClosed());
    }
    @Test void explicitOperatorApproval() throws Exception {
        try (var workflow = workflow(825, true)) {
            workflow.run();
            assertEquals(825, workflow.receipts().get(Stage.BILLING).path("amount").asInt());
            assertTrue(workflow.receipts().containsKey(Stage.NOTIFICATION));
        }
    }
    @Test void expandedHandoffNeverStartsChild() throws Exception {
        try (var workflow = new Workflow(env("HANDOFFGUARD_BINARY"), env("HANDOFFGUARD_SERVER_URL"), env("HANDOFFGUARD_API_TOKEN"), 100, false) {
            @Override protected ObjectNode child(Stage parent, Stage stage) {
                var child = super.child(parent, stage);
                child.withArray("allowed_actions").add("payments.export");
                return child;
            }
        }; var api = api()) {
            assertEquals(403, assertThrows(ControlApi.Rejected.class, workflow::run).status());
            assertEquals(Set.of(Stage.SUPPORT), workflow.envelopes().keySet());
            var chain = api.get("/v1/runs/" + workflow.runId() + "/chain");
            assertEquals(1, chain.path("envelopes").size());
            assertEquals(1, chain.path("actions").size());
            assertEquals(1, chain.path("handoffs").size());
        }
    }
    @Test void earlyHandoffAndChangedArgumentsNeverReachGateway() throws Exception {
        try (var workflow = workflow(100, false); var api = api()) {
            workflow.start();
            assertThrows(IllegalStateException.class, () -> workflow.transition(Stage.SUPPORT, Stage.BILLING));
            assertThrows(IllegalArgumentException.class, () -> workflow.execute(Stage.SUPPORT, Map.of("order_id", "other")));
            var chain = api.get("/v1/runs/" + workflow.runId() + "/chain");
            assertEquals(0, chain.path("actions").size());
            assertEquals(0, chain.path("handoffs").size());
        }
    }
    @Test void repeatedStageAndRunAreBlocked() throws Exception {
        try (var workflow = workflow(100, false); var api = api()) {
            workflow.start();
            workflow.execute(Stage.SUPPORT, workflow.arguments(Stage.SUPPORT));
            assertThrows(IllegalStateException.class, () -> workflow.execute(Stage.SUPPORT, workflow.arguments(Stage.SUPPORT)));
            assertThrows(IllegalStateException.class, workflow::start);
            assertEquals(1, api.get("/v1/runs/" + workflow.runId() + "/chain").path("actions").size());
        }
    }
    @Test void interruptionClosesSessions() throws Exception {
        var workflow = workflow(100, false);
        try (workflow) {
            workflow.start();
            Thread.currentThread().interrupt();
            try { assertThrows(IllegalStateException.class, () -> workflow.execute(Stage.SUPPORT, workflow.arguments(Stage.SUPPORT))); }
            finally { Thread.interrupted(); }
        }
        assertTrue(workflow.sessionsClosed());
        assertFalse(Files.exists(workflow.temporaryDirectory()));
    }
    @Test void gatewayDemoCoversApprovalReplayRevocationAndHiddenTools() {
        try (var api = api()) {
            DemoScenarios.gateway(env("HANDOFFGUARD_BINARY"), env("HANDOFFGUARD_SERVER_URL"), env("HANDOFFGUARD_API_TOKEN"), api);
        }
    }
    @Test void serverDemoCoversApprovalConsumption() {
        try (var api = api()) { DemoScenarios.server(api); }
    }
}
