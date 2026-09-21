package dev.handoffguard.workflow;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.Set;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import static org.junit.jupiter.api.Assertions.*;

class WorkflowStoreTest {
    @TempDir Path directory;

    private WorkflowStore.Snapshot snapshot(String pending) {
        return new WorkflowStore.Snapshot(1, "http://localhost/", 100, false, null,
                Map.of(), Map.of(), Set.of(), List.of(), false, pending);
    }

    @Test void replacementSurvivesReopenAndLockExcludesOtherOwners() throws Exception {
        try (var store = new WorkflowStore(directory)) {
            store.save(snapshot(""));
            assertThrows(IllegalStateException.class, () -> new WorkflowStore(directory));
            store.save(snapshot("CREATE_RUN"));
            assertEquals("CREATE_RUN", store.read().pending());
            assertEquals("rw-------", java.nio.file.attribute.PosixFilePermissions.toString(
                    Files.getPosixFilePermissions(directory.resolve("workflow.json"))));
        }
        try (var store = new WorkflowStore(directory)) {
            assertEquals("CREATE_RUN", store.read().pending());
        }
    }

    @Test void resumeNeverCreatesAReplacementForMissingCorruptOrUncertainState() throws Exception {
        assertThrows(java.io.IOException.class, () -> resume());
        for (String invalid : List.of("broken json", "null", "{}")) {
            Files.writeString(directory.resolve("workflow.json"), invalid);
            assertThrows(Exception.class, () -> resume());
            assertEquals(invalid, Files.readString(directory.resolve("workflow.json")));
        }
        try (var store = new WorkflowStore(directory)) { store.save(snapshot("EXECUTE_BILLING")); }
        var error = assertThrows(IllegalStateException.class, () -> resume());
        assertTrue(error.getMessage().contains("RECONCILIATION_REQUIRED"));
        try (var store = new WorkflowStore(directory)) { assertEquals("EXECUTE_BILLING", store.read().pending()); }
    }

    @Test void newWorkflowCannotOverwriteExistingStateAndResumeBindsOrigin() throws Exception {
        try (var store = new WorkflowStore(directory)) { store.save(snapshot("")); }
        assertThrows(IllegalStateException.class, () -> new Workflow("unused", "http://localhost", "token", 100, false, directory, false));
        assertThrows(IllegalStateException.class, () -> new Workflow("unused", "http://localhost:9999", "token", 100, false, directory, true));
        try (var workflow = resume()) { assertNotNull(workflow); }
    }

    @Test void failedCheckpointPreventsExternalOperation() throws Exception {
        try (var workflow = new Workflow("unused", "http://localhost:1", "token", 100, false, directory, false)) {
            Files.delete(directory.resolve("workflow.json"));
            Files.createDirectory(directory.resolve("workflow.json"));
            var error = assertThrows(IllegalStateException.class, workflow::run);
            assertTrue(error.getMessage().contains("Cannot durably save"));
            assertNull(workflow.runId());
        }
    }

    private Workflow resume() throws Exception {
        return new Workflow("unused", "http://localhost", "token", 100, false, directory, true);
    }
}
