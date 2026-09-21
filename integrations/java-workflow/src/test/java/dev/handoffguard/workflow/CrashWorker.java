package dev.handoffguard.workflow;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Map;
import tools.jackson.databind.JsonNode;
import io.modelcontextprotocol.spec.McpSchema.CallToolResult;

/** Separate JVM killed at an actual refund boundary, without running close/shutdown hooks. */
public final class CrashWorker {
    public static void main(String[] args) throws Exception {
        Path state = Path.of(args[0]);
        boolean uncertain = args[1].equals("uncertain");
        try (var workflow = new Workflow(System.getenv("HANDOFFGUARD_BINARY"), System.getenv("HANDOFFGUARD_SERVER_URL"),
                System.getenv("HANDOFFGUARD_API_TOKEN"), 825, true, state, false) {
            @Override protected CallToolResult callTool(Stage stage, Map<String, Object> arguments) {
                var result = super.callTool(stage, arguments);
                if (stage == Stage.BILLING && uncertain) {
                    if (Boolean.TRUE.equals(result.isError())) throw new IllegalStateException("Expected successful refund");
                    haltWithReceipt(ControlApi.JSON.valueToTree(result.structuredContent()));
                }
                return result;
            }
            @Override public synchronized JsonNode execute(Stage stage, Map<String, Object> arguments) {
                var receipt = super.execute(stage, arguments);
                if (stage == Stage.BILLING) haltWithReceipt(receipt);
                return receipt;
            }
            private void haltWithReceipt(JsonNode receipt) {
                try {
                    ControlApi.JSON.writeValue(state.resolve("observed-refund.json").toFile(), receipt);
                    Files.writeString(state.resolve("temporary-directory.txt"), temporaryDirectory().toString());
                } catch (Exception error) { throw new IllegalStateException(error); }
                Runtime.getRuntime().halt(17);
            }
        }) { workflow.run(); }
        throw new IllegalStateException("Crash boundary was not reached");
    }
}
