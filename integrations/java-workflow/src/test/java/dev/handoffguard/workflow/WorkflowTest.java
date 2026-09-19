package dev.handoffguard.workflow;

import java.util.Map;
import org.junit.jupiter.api.Test;
import org.springframework.context.annotation.AnnotationConfigApplicationContext;
import static dev.handoffguard.workflow.ControlApi.JSON;
import static org.junit.jupiter.api.Assertions.*;

class WorkflowTest {
    @Test void refusesInsecureControlOrigins() {
        for (String url : new String[]{"http://example.com", "http://localhost.evil.test", "https://a.test/path", "https://user:pass@a.test", "https://a.test?token=bad"}) {
            assertThrows(IllegalArgumentException.class, () -> new ControlApi(url, "token"));
        }
        try (var api = new ControlApi("https://example.com", "token")) { assertNotNull(api); }
        try (var api = new ControlApi("http://[::1]:8080", "token")) { assertNotNull(api); }
    }
    @Test void rejectsBadAmountBeforeCreatingResources() {
        assertThrows(IllegalArgumentException.class, () -> new Workflow("handoffguard", "http://localhost", "token", 0, false));
    }
    @Test void validatesReceiptsBeforeAllowingHandoff() {
        var arguments = Map.<String,Object>of("order_id", "48319", "amount", 100);
        var receipt = JSON.readTree("{\"order_id\":\"48319\",\"refund_id\":\"simulated_1\",\"amount\":100}");
        assertDoesNotThrow(() -> Workflow.validateReceipt(Stage.BILLING, receipt, arguments));
        assertThrows(IllegalStateException.class, () -> Workflow.validateReceipt(Stage.BILLING, receipt, Map.of("amount", 825)));
        assertThrows(IllegalStateException.class, () -> Workflow.validateReceipt(Stage.BILLING, JSON.createObjectNode(), arguments));
        var email = JSON.readTree("{\"order_id\":\"48319\",\"notification_id\":\"simulated_1\",\"refund_id\":\"other\"}");
        assertThrows(IllegalStateException.class, () -> Workflow.validateReceipt(Stage.NOTIFICATION, email, Map.of("refund_id", "simulated_1")));
    }
    @Test void springWiresTheFactoryWithoutOpeningExternalConnections() {
        try (var context = new AnnotationConfigApplicationContext(WorkflowApplication.class)) {
            assertNotNull(context.getBean(WorkflowFactory.class));
        }
    }
}
