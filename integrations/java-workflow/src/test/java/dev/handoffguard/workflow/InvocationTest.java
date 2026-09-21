package dev.handoffguard.workflow;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class InvocationTest {
    @Test void approvalComesFromAnOperatorAndPreparationIsExplicit() {
        assertFalse(Invocation.parse().approve());
        assertTrue(Invocation.parse("--amount=825", "--prepare-approval").prepareApproval());
        assertThrows(IllegalArgumentException.class, () -> Invocation.parse("--approve-demo-refund"));
        assertThrows(IllegalArgumentException.class, () -> Invocation.parse("--approve-demo-refund=false"));
        assertThrows(IllegalArgumentException.class, () -> Invocation.parse("--approve-demo-refund=true"));
        assertThrows(IllegalArgumentException.class, () -> Invocation.parse("--prepare-approval=false"));
    }
    @Test void rejectsInvalidAndAmbiguousRequests() {
        for (String[] args : new String[][]{{"--amount=0"}, {"--amount=-1"}, {"--amount=1.5"}, {"--amount"},
                {"--amount=1", "--amount=2"}, {"--amount", "100"}, {"--model=anything"}, {"--scenario=invalid"}}) {
            assertThrows(IllegalArgumentException.class, () -> Invocation.parse(args));
        }
    }
    @Test void recoveryRequiresExplicitStateAndCannotChangeTheSavedRequest() {
        var invocation = Invocation.parse("--resume", "--state=/tmp/workflow");
        assertTrue(invocation.resume());
        assertEquals("/tmp/workflow", invocation.state());
        for (String[] args : new String[][]{{"--resume"}, {"--state"}, {"--state="}, {"--resume=false"},
                {"--state=a", "--state=b"}, {"--resume", "--state=a", "--amount=825"},
                {"--resume", "--state=a", "--approve-demo-refund"}, {"--state=a", "--scenario=gateway"}}) {
            assertThrows(IllegalArgumentException.class, () -> Invocation.parse(args));
        }
    }
}
