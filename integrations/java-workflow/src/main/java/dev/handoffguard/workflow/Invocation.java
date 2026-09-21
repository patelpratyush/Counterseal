package dev.handoffguard.workflow;

import java.util.Set;
import org.springframework.boot.DefaultApplicationArguments;

record Invocation(int amount, boolean approve, String scenario, boolean help, String state, boolean resume, boolean prepareApproval) {
    static Invocation parse(String... args) {
        var options = new DefaultApplicationArguments(args);
        for (String name : options.getOptionNames()) {
            if (!Set.of("amount", "prepare-approval", "help", "scenario", "state", "resume").contains(name)) {
                throw new IllegalArgumentException("Unknown option: " + name);
            }
        }
        if (!options.getNonOptionArgs().isEmpty()) throw new IllegalArgumentException("Use --name=value options");
        for (String flag : Set.of("prepare-approval", "help", "resume")) {
            if (options.containsOption(flag) && !options.getOptionValues(flag).isEmpty()) {
                throw new IllegalArgumentException("--" + flag + " does not take a value");
            }
        }
        var amounts = options.getOptionValues("amount");
        if (amounts != null && amounts.size() != 1) throw new IllegalArgumentException("Use --amount=INTEGER");
        int amount = amounts == null ? 100 : Integer.parseInt(amounts.getFirst());
        if (amount <= 0) throw new IllegalArgumentException("Amount must be a positive integer");
        var scenarios = options.getOptionValues("scenario");
        if (scenarios != null && scenarios.size() != 1) throw new IllegalArgumentException("Use --scenario=workflow|gateway|server");
        String scenario = scenarios == null ? "workflow" : scenarios.getFirst();
        if (!Set.of("workflow", "gateway", "server").contains(scenario)) throw new IllegalArgumentException("Unknown scenario");
        var states = options.getOptionValues("state");
        if (states != null && (states.size() != 1 || states.getFirst().isBlank())) {
            throw new IllegalArgumentException("Use --state=PATH");
        }
        String state = states == null ? null : states.getFirst();
        boolean resume = options.containsOption("resume");
        boolean prepare = options.containsOption("prepare-approval");
        if ((resume || state != null || prepare) && !scenario.equals("workflow")) {
            throw new IllegalArgumentException("Recovery options apply only to --scenario=workflow");
        }
        if (resume && (state == null || amounts != null || prepare)) {
            throw new IllegalArgumentException("Use --resume --state=PATH without amount or approval overrides");
        }
        return new Invocation(amount, false, scenario, options.containsOption("help"), state, resume, prepare);
    }
}
