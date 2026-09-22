package dev.handoffguard.workflow;

import java.nio.file.Path;
import java.time.Instant;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import static dev.handoffguard.workflow.ControlApi.JSON;
import static org.junit.jupiter.api.Assertions.*;

class ModelIT {
    @TempDir Path state;
    private String env(String name) { return java.util.Objects.requireNonNull(System.getenv(name)); }
    private ControlApi api() { return new ControlApi(env("HANDOFFGUARD_SERVER_URL"), env("HANDOFFGUARD_API_TOKEN")); }
    private Workflow workflow(int amount, boolean resume) throws Exception {
        return new Workflow(env("HANDOFFGUARD_BINARY"), env("HANDOFFGUARD_SERVER_URL"),
                env("HANDOFFGUARD_API_TOKEN"), amount, false, state, resume);
    }

    @Test void modelProposalsExecuteThroughRealGatewayAndCompletedRecoveryDoesNotCallModel() throws Exception {
        String run;
        try (var server = new FakeModel(FakeModel::echo); var model = server.client(); var workflow = workflow(100, false)) {
            workflow.useModel(model);
            var result = workflow.run(); run = result.runId();
            assertEquals(3, result.receipts().size());
            assertEquals(3, server.requests.size());
            assertEquals(3, result.events().stream().filter(e -> e.type().equals("model_proposed")).count());
        }
        try (var resumed = workflow(100, true); var api = api()) {
            resumed.useModel((stage, args) -> { throw new AssertionError("Must not propose completed stages again"); });
            assertEquals(run, resumed.run().runId());
            assertEquals(3, api.get("/v1/runs/" + run + "/chain").path("actions").size());
        }
    }

    @Test void unauthorizedModelRefundIsDeniedAndAuditedByCounterseal() throws Exception {
        try (var server = new FakeModel(request -> request.path("tool_choice").path("name").asText().equals("refund_create")
                ? FakeModel.call("refund_create", JSON.valueToTree(Map.of("order_id", "99999", "amount", 100))) : FakeModel.echo(request));
             var model = server.client(); var workflow = workflow(100, false); var api = api()) {
            workflow.useModel(model);
            assertThrows(IllegalStateException.class, workflow::run);
            assertEquals(java.util.Set.of(Stage.SUPPORT), workflow.receipts().keySet());
            var chain = api.get("/v1/runs/" + workflow.runId() + "/chain");
            assertEquals(2, chain.path("actions").size());
            assertTrue(chain.path("actions").toString().contains("DENY"));
            assertTrue(chain.path("actions").toString().contains("RESOURCE"));
            assertEquals("VALID", api.post("/v1/audit/" + workflow.runId() + "/verify", null, 200).path("status").asText());
        }
        assertThrows(IllegalStateException.class, () -> workflow(100, true), "Denial is not silently retried");
    }

    @Test void preparedProposalSurvivesOperatorApprovalAndResumesWithoutReplanningRefund() throws Exception {
        String run, envelope;
        try (var server = new FakeModel(FakeModel::echo); var model = server.client(); var workflow = workflow(825, false)) {
            workflow.useModel(model);
            var prepared = workflow.prepareApproval(); run = (String) prepared.get("run_id"); envelope = (String) prepared.get("envelope_id");
            assertEquals(2, server.requests.size());
            assertEquals(Map.of("order_id", "48319", "amount", 825), prepared.get("arguments"));
        }
        try (var resumed = workflow(100, true); var api = api()) {
            assertThrows(IllegalStateException.class, resumed::run, "No silent deterministic fallback");
            assertEquals(1, api.get("/v1/runs/" + run + "/chain").path("actions").size());
        }
        try (var api = api()) {
            var login = api.post("/v1/operators/login", JSON.valueToTree(Map.of("username", "morgan", "password", env("HANDOFFGUARD_OPERATOR_PASSWORD"))), 200);
            try (var operator = new ControlApi(env("HANDOFFGUARD_SERVER_URL"), login.path("token").asText())) {
                operator.post("/v1/approvals", JSON.valueToTree(Map.of("envelope_id", envelope, "action", "refunds.create",
                        "resource", "orders:48319", "arguments", Map.of("order_id", "48319", "amount", 825),
                        "expires_at", Instant.now().plusSeconds(600).toString())), 201);
            }
        }
        try (var server = new FakeModel(FakeModel::echo); var model = server.client(); var resumed = workflow(100, true); var api = api()) {
            resumed.useModel(model);
            assertEquals(run, resumed.run().runId());
            assertEquals(1, server.requests.size());
            assertEquals("email_send", server.requests.getFirst().path("tool_choice").path("name").asText());
            assertEquals(3, api.get("/v1/runs/" + run + "/chain").path("actions").size());
            assertEquals("morgan", api.get("/v1/runs/" + run + "/approvals").path("approvals").get(0).path("username").asText());
        }
    }
}
