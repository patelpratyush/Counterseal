package dev.handoffguard.workflow;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Comparator;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.io.TempDir;
import static dev.handoffguard.workflow.ControlApi.JSON;
import static org.junit.jupiter.api.Assertions.*;

@Timeout(value = 120, unit = TimeUnit.SECONDS)
class RecoveryIT {
    @TempDir Path directory;
    private String env(String name) { return java.util.Objects.requireNonNull(System.getenv(name)); }
    private ControlApi api() { return new ControlApi(env("HANDOFFGUARD_SERVER_URL"), env("HANDOFFGUARD_API_TOKEN")); }
    private Workflow workflow(boolean resume) throws Exception {
        return new Workflow(env("HANDOFFGUARD_BINARY"), env("HANDOFFGUARD_SERVER_URL"),
                env("HANDOFFGUARD_API_TOKEN"), 100, false, directory, resume);
    }
    private WorkflowStore.Snapshot saved() throws Exception {
        try (var store = new WorkflowStore(directory)) { return store.read(); }
    }

    @Test void abruptCrashAfterSavedRefundResumesWithoutAnotherRefundOrApproval() throws Exception {
        crash("saved");
        var before = saved();
        assertEquals(825, before.amount());
        assertTrue(before.approvalGranted());
        assertEquals("", before.pending());
        var refund = JSON.readTree(directory.resolve("observed-refund.json").toFile());
        try (var api = api(); var resumed = workflow(true)) {
            assertEquals(2, api.get("/v1/runs/" + before.runId() + "/chain").path("actions").size());
            var result = resumed.run();
            assertEquals(before.runId(), result.runId());
            assertEquals(refund, result.receipts().get(Stage.BILLING));
            assertEquals(refund.path("refund_id"), result.receipts().get(Stage.NOTIFICATION).path("refund_id"));
            var chain = api.get("/v1/runs/" + result.runId() + "/chain");
            assertEquals(3, chain.path("actions").size(), "Only Notification may execute after recovery");
            assertEquals(3, chain.path("envelopes").size());
            assertEquals("VALID", result.audit().path("status").asText());
        }
        // A completed checkpoint is safe to inspect/resume repeatedly, with no new tool calls.
        try (var resumed = workflow(true); var api = api()) {
            resumed.run();
            assertEquals(3, api.get("/v1/runs/" + before.runId() + "/chain").path("actions").size());
        }
    }

    @Test void abruptCrashAfterRefundBeforeReceiptCommitRefusesReplay() throws Exception {
        crash("uncertain");
        var before = saved();
        assertEquals("EXECUTE_BILLING", before.pending());
        assertFalse(before.receipts().containsKey(Stage.BILLING));
        assertTrue(Files.exists(directory.resolve("observed-refund.json")), "Upstream succeeded before the crash");
        var error = assertThrows(IllegalStateException.class, () -> workflow(true));
        assertTrue(error.getMessage().contains("RECONCILIATION_REQUIRED"));
        assertEquals(before, saved(), "Refusing recovery must not erase the uncertainty marker");
        try (var api = api()) {
            var chain = api.get("/v1/runs/" + before.runId() + "/chain");
            assertEquals(2, chain.path("actions").size());
            assertEquals(2, chain.path("envelopes").size());
        }
    }

    @Test void checkpointBeforeFirstRunAndAfterDelegationCanResume() throws Exception {
        try (var initial = workflow(false)) { assertNull(initial.runId()); }
        try (var resumed = workflow(true)) {
            resumed.start();
            resumed.execute(Stage.SUPPORT, resumed.arguments(Stage.SUPPORT));
            resumed.transition(Stage.SUPPORT, Stage.BILLING);
        }
        String run = saved().runId();
        try (var resumed = workflow(true); var api = api()) {
            assertEquals(run, resumed.run().runId());
            assertEquals(3, api.get("/v1/runs/" + run + "/chain").path("actions").size());
        }
    }

    @Test void packagedCliResumesAndConcurrentProcessCannotAcquireCheckpoint() throws Exception {
        Workflow.Result original;
        try (var owner = workflow(false)) {
            original = owner.run();
            assertEquals(1, cliResume());
            assertTrue(Files.readString(directory.resolve("cli-error.log")).contains("already owned"));
        }
        assertEquals(0, cliResume(), () -> read(directory.resolve("cli-error.log")));
        var output = JSON.readTree(directory.resolve("cli-result.json").toFile());
        assertEquals(original.runId(), output.path("runId").asText());
        assertEquals(JSON.valueToTree(original.receipts()), output.path("receipts"));
        assertEquals("VALID", output.path("audit").path("status").asText());
        assertFalse(Files.readString(directory.resolve("workflow.json")).contains(env("HANDOFFGUARD_API_TOKEN")));
        try (var api = api()) {
            assertEquals(3, api.get("/v1/runs/" + original.runId() + "/chain").path("actions").size());
        }
    }

    @Test void operatorApprovesPreparedRefundBeforeTheWorkflowResumes() throws Exception {
        String run, envelope;
        try (var workflow = new Workflow(env("HANDOFFGUARD_BINARY"), env("HANDOFFGUARD_SERVER_URL"),
                env("HANDOFFGUARD_API_TOKEN"), 825, false, directory, false)) {
            var prepared = workflow.prepareApproval();
            assertEquals("AWAITING_OPERATOR_APPROVAL", prepared.get("status"));
            run = (String) prepared.get("run_id");
            envelope = (String) prepared.get("envelope_id");
            assertFalse(workflow.receipts().containsKey(Stage.BILLING));
        }
        try (var api = api()) {
            assertEquals(1, api.get("/v1/runs/" + run + "/chain").path("actions").size());
            var login = api.post("/v1/operators/login", JSON.valueToTree(java.util.Map.of(
                    "username", "morgan", "password", env("HANDOFFGUARD_OPERATOR_PASSWORD"))), 200);
            try (var operator = new ControlApi(env("HANDOFFGUARD_SERVER_URL"), login.path("token").asText())) {
                var approved = operator.post("/v1/approvals", JSON.valueToTree(java.util.Map.of(
                        "envelope_id", envelope, "action", "refunds.create", "resource", "orders:48319",
                        "arguments", java.util.Map.of("order_id", "48319", "amount", 825),
                        "expires_at", java.time.Instant.now().plusSeconds(600).toString())), 201);
                assertEquals("morgan", approved.path("approved_by").asText());
            }
        }
        try (var resumed = workflow(true); var api = api()) {
            assertEquals(run, resumed.run().runId());
            assertEquals(3, api.get("/v1/runs/" + run + "/chain").path("actions").size());
            var history = api.get("/v1/runs/" + run + "/approvals").path("approvals");
            assertEquals("morgan", history.get(0).path("username").asText());
            assertFalse(history.get(0).path("consumed_at").isNull());
        }
    }

    private int cliResume() throws Exception {
        Path jar = Path.of(Workflow.class.getProtectionDomain().getCodeSource().getLocation().toURI())
                .getParent().resolve("handoffguard-workflow.jar");
        var process = new ProcessBuilder(Path.of(System.getProperty("java.home"), "bin", "java").toString(),
                "-jar", jar.toString(), "--resume", "--state=" + directory)
                .redirectOutput(directory.resolve("cli-result.json").toFile())
                .redirectError(directory.resolve("cli-error.log").toFile()).start();
        try {
            assertTrue(process.waitFor(45, TimeUnit.SECONDS), "CLI resume timed out");
            return process.exitValue();
        } finally {
            if (process.isAlive()) {
                process.descendants().forEach(ProcessHandle::destroyForcibly);
                process.destroyForcibly();
                process.waitFor(5, TimeUnit.SECONDS);
            }
        }
    }

    private void crash(String mode) throws Exception {
        Path log = directory.resolve("worker.log");
        var process = new ProcessBuilder(Path.of(System.getProperty("java.home"), "bin", "java").toString(),
                "-cp", System.getProperty("java.class.path"), CrashWorker.class.getName(), directory.toString(), mode)
                .redirectErrorStream(true).redirectOutput(log.toFile()).start();
        try {
            assertTrue(process.waitFor(90, TimeUnit.SECONDS), "Crash worker timed out");
            assertEquals(17, process.exitValue(), () -> "Worker did not reach crash boundary: " + read(log));
        } finally {
            if (process.isAlive()) {
                process.descendants().forEach(ProcessHandle::destroyForcibly);
                process.destroyForcibly();
                process.waitFor(5, TimeUnit.SECONDS);
            }
            Path marker = directory.resolve("temporary-directory.txt");
            if (Files.exists(marker)) {
                Path temporary = Path.of(Files.readString(marker));
                try (var paths = Files.walk(temporary)) {
                    for (var path : paths.sorted(Comparator.reverseOrder()).toList()) Files.deleteIfExists(path);
                }
            }
        }
    }
    private static String read(Path path) {
        try { return Files.readString(path); } catch (Exception error) { return error.toString(); }
    }
}
