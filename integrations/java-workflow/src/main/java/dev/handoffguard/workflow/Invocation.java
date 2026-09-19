package dev.handoffguard.workflow;

import java.util.Set;
import org.springframework.boot.DefaultApplicationArguments;

record Invocation(int amount, boolean approve, String scenario, boolean help) {
    static Invocation parse(String... args) {
        var options = new DefaultApplicationArguments(args);
        for (String name : options.getOptionNames()) {
            if (!Set.of("amount", "approve-demo-refund", "help", "scenario").contains(name)) {
                throw new IllegalArgumentException("Unknown option: " + name);
            }
        }
        if (!options.getNonOptionArgs().isEmpty()) throw new IllegalArgumentException("Use --name=value options");
        for (String flag : Set.of("approve-demo-refund", "help")) {
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
        return new Invocation(amount, options.containsOption("approve-demo-refund"), scenario, options.containsOption("help"));
    }
}
