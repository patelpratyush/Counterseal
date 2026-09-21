package dev.handoffguard.workflow;

import java.io.IOException;
import java.nio.file.Path;
import java.util.UUID;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import org.springframework.core.env.Environment;
import org.springframework.stereotype.Service;

/** Spring owns active workflow resources, including shutdown cleanup. */
@Service
public final class WorkflowFactory implements AutoCloseable {
    private final Environment environment;
    private final Set<Workflow> workflows = ConcurrentHashMap.newKeySet();
    public WorkflowFactory(Environment environment) { this.environment = environment; }
    public Workflow create(int amount, boolean approve) throws IOException {
        var workflow = new Workflow(required("HANDOFFGUARD_BINARY"), required("HANDOFFGUARD_SERVER_URL"),
                required("HANDOFFGUARD_API_TOKEN"), amount, approve);
        workflows.add(workflow);
        return workflow;
    }
    public Workflow create(Invocation invocation) throws IOException {
        Path state = invocation.state() == null
                ? Path.of(environment.getProperty("HANDOFFGUARD_WORKFLOW_STATE_DIR", ".workflow-state"), UUID.randomUUID().toString())
                : Path.of(invocation.state());
        var workflow = new Workflow(required("HANDOFFGUARD_BINARY"), required("HANDOFFGUARD_SERVER_URL"),
                required("HANDOFFGUARD_API_TOKEN"), invocation.amount(), invocation.approve(), state, invocation.resume());
        workflows.add(workflow);
        System.err.println("Workflow checkpoint: " + state.toAbsolutePath().normalize());
        return workflow;
    }
    private String required(String name) {
        String value = environment.getProperty(name);
        if (value == null || value.isBlank()) throw new IllegalArgumentException("Missing " + name);
        return value;
    }
    @Override public void close() {
        RuntimeException failure = null;
        for (var workflow : workflows) {
            try { workflow.close(); }
            catch (RuntimeException error) {
                if (failure == null) failure = error;
                else failure.addSuppressed(error);
            }
        }
        workflows.clear();
        if (failure != null) throw failure;
    }
}
