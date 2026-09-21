package dev.handoffguard.workflow;

import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.channels.OverlappingFileLockException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.StandardOpenOption;
import java.nio.file.attribute.PosixFilePermissions;
import java.util.List;
import java.util.Map;
import java.util.Set;
import tools.jackson.databind.JsonNode;
import static dev.handoffguard.workflow.ControlApi.JSON;

/** One locally durable workflow per directory. The lock file is never replaced or deleted. */
final class WorkflowStore implements AutoCloseable {
    record Snapshot(int version, String baseUrl, int amount, boolean approve, String runId,
                    Map<Stage, JsonNode> envelopes, Map<Stage, JsonNode> receipts,
                    Set<Stage> attempted, List<Workflow.Event> events, boolean approvalGranted, String pending) {}

    private final Path directory;
    private final FileChannel channel;
    private final FileLock lock;

    WorkflowStore(Path directory) throws IOException {
        createDirectory(directory.toAbsolutePath().normalize());
        this.directory = directory.toRealPath();
        channel = FileChannel.open(this.directory.resolve("workflow.lock"),
                Set.of(StandardOpenOption.CREATE, StandardOpenOption.WRITE),
                PosixFilePermissions.asFileAttribute(PosixFilePermissions.fromString("rw-------")));
        try {
            lock = channel.tryLock();
            if (lock == null) throw new IllegalStateException("Workflow is already owned by another process");
        } catch (IOException | RuntimeException error) {
            channel.close();
            if (error instanceof OverlappingFileLockException) {
                throw new IllegalStateException("Workflow is already owned by another process", error);
            }
            throw error;
        }
    }

    private static void createDirectory(Path path) throws IOException {
        if (Files.isDirectory(path)) return;
        createDirectory(path.getParent());
        try {
            Files.createDirectory(path, PosixFilePermissions.asFileAttribute(PosixFilePermissions.fromString("rwx------")));
        } catch (java.nio.file.FileAlreadyExistsException error) {
            if (!Files.isDirectory(path)) throw error;
        }
        try (var parent = FileChannel.open(path.getParent(), StandardOpenOption.READ)) { parent.force(true); }
    }

    boolean exists() { return Files.exists(directory.resolve("workflow.json")); }

    Snapshot read() throws IOException {
        if (!exists()) throw new IOException("No checkpoint found; cannot resume this workflow");
        try {
            return JSON.readValue(directory.resolve("workflow.json").toFile(), Snapshot.class);
        } catch (RuntimeException error) {
            throw new IOException("Invalid workflow checkpoint; refusing to start a replacement run", error);
        }
    }

    void save(Snapshot snapshot) {
        Path temporary = null;
        try {
            temporary = Files.createTempFile(directory, ".checkpoint-", ".tmp",
                    PosixFilePermissions.asFileAttribute(PosixFilePermissions.fromString("rw-------")));
            try (var output = FileChannel.open(temporary, StandardOpenOption.WRITE)) {
                var bytes = ByteBuffer.wrap(JSON.writeValueAsBytes(snapshot));
                while (bytes.hasRemaining()) output.write(bytes);
                output.force(true);
            }
            // No non-atomic fallback: failing to checkpoint must prevent the next side effect.
            Files.move(temporary, directory.resolve("workflow.json"), StandardCopyOption.ATOMIC_MOVE,
                    StandardCopyOption.REPLACE_EXISTING);
            try (var folder = FileChannel.open(directory, StandardOpenOption.READ)) { folder.force(true); }
        } catch (IOException error) {
            throw new IllegalStateException("Cannot durably save workflow; no further operations are safe", error);
        } finally {
            if (temporary != null) {
                try { Files.deleteIfExists(temporary); } catch (IOException ignored) { /* Never replace the committed checkpoint. */ }
            }
        }
    }

    @Override public void close() throws IOException {
        try { lock.release(); } finally { channel.close(); }
    }
}
