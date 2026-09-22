package dev.handoffguard.workflow;

import java.util.Map;

/** Proposals are untrusted input; this interface has no execution or approval capability. */
@FunctionalInterface
interface ToolProposer {
    Map<String, Object> propose(Stage stage, Map<String, Object> request);
}
