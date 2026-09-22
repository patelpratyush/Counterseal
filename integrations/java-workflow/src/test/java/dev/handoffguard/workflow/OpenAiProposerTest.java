package dev.handoffguard.workflow;

import java.util.Map;
import org.junit.jupiter.api.Test;
import static dev.handoffguard.workflow.ControlApi.JSON;
import static org.junit.jupiter.api.Assertions.*;

class OpenAiProposerTest {
    @Test void providerFailuresAreNotRetriedOrLeaked() throws Exception {
        for (int status : new int[]{401, 429, 500, 302}) {
            try (var server = new FakeModel(status, request -> JSON.valueToTree(Map.of("error", "private-provider-detail")));
                 var model = server.client()) {
                var error = assertThrows(IllegalStateException.class, () -> model.propose(Stage.SUPPORT, Map.of("order_id", "48319")));
                assertEquals("Model provider failed: HTTP " + status, error.getMessage());
                assertEquals(1, server.requests.size());
            }
        }
        assertThrows(IllegalArgumentException.class, () -> new OpenAiProposer("", "model", "task"));
        assertThrows(IllegalArgumentException.class, () -> new OpenAiProposer("key", null, "task"));
    }

    @Test void realHttpContractUsesOneStrictToolAndDoesNotSendAuthority() throws Exception {
        try (var server = new FakeModel(FakeModel::echo); var model = server.client()) {
            var args = Map.<String, Object>of("order_id", "48319", "amount", 825);
            assertEquals(args, model.propose(Stage.BILLING, args));
            var request = server.requests.getFirst();
            assertFalse(request.path("store").asBoolean());
            assertFalse(request.path("parallel_tool_calls").asBoolean());
            assertTrue(request.path("tools").get(0).path("strict").asBoolean());
            assertEquals("refund_create", request.path("tool_choice").path("name").asText());
            assertFalse(request.toString().contains("test-key"));
            assertFalse(request.toString().contains("envelope_id"));
        }
    }

    @Test void rejectsRefusalsIncompleteMultipleUnknownAndMalformedCalls() throws Exception {
        var valid = FakeModel.call("orders_get", JSON.valueToTree(Map.of("order_id", "48319")));
        var incomplete = valid.deepCopy(); ((tools.jackson.databind.node.ObjectNode) incomplete).put("status", "incomplete");
        var multiple = valid.deepCopy(); ((tools.jackson.databind.node.ArrayNode) multiple.path("output")).add(valid.path("output").get(0));
        var duplicate = valid.deepCopy(); ((tools.jackson.databind.node.ObjectNode) duplicate.path("output").get(0))
                .put("arguments", "{\"order_id\":\"48319\",\"order_id\":\"99999\"}");
        for (var response : java.util.List.of(incomplete, multiple, duplicate,
                FakeModel.call("refund_create", JSON.valueToTree(Map.of("order_id", "48319"))),
                FakeModel.call("orders_get", JSON.valueToTree(Map.of("order_id", "48319", "role", "refund_manager"))),
                JSON.readTree("{\"status\":\"completed\",\"output\":[{\"type\":\"message\"}]}"))) {
            try (var server = new FakeModel(request -> response); var model = server.client()) {
                assertThrows(IllegalStateException.class, () -> model.propose(Stage.SUPPORT, Map.of("order_id", "48319")));
                assertEquals(1, server.requests.size(), "No automatic retries");
            }
        }
    }

    @Test void cannotRewriteBusinessAmountOrReceipt() {
        assertThrows(IllegalStateException.class, () -> Workflow.validateProposal(
                Map.of("order_id", "48319", "amount", 825), Map.of("order_id", "48319", "amount", 100)));
        assertThrows(IllegalStateException.class, () -> Workflow.validateProposal(
                Map.of("order_id", "48319", "refund_id", "original"), Map.of("order_id", "48319", "refund_id", "invented")));
        assertTrue(Invocation.parse("--live-model", "--prepare-approval").liveModel());
        assertThrows(IllegalArgumentException.class, () -> Invocation.parse("--live-model=true"));
        assertThrows(IllegalArgumentException.class, () -> Invocation.parse("--live-model", "--scenario=gateway"));
    }
}
