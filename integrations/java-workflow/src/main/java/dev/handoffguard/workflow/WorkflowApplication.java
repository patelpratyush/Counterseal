package dev.handoffguard.workflow;

import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.builder.SpringApplicationBuilder;
import static dev.handoffguard.workflow.ControlApi.JSON;

@SpringBootApplication
public class WorkflowApplication {
    public static void main(String[] args) {
        try {
            var invocation = Invocation.parse(args);
            if (invocation.help()) {
                System.out.println("Usage: java -jar handoffguard-workflow.jar [--amount=100] [--approve-demo-refund] [--scenario=workflow|gateway|server]");
                return;
            }
            String scenario = invocation.scenario();
            try (var context = new SpringApplicationBuilder(WorkflowApplication.class).logStartupInfo(false).run(args)) {
                if (scenario.equals("workflow")) {
                    try (var workflow = context.getBean(WorkflowFactory.class).create(invocation.amount(), invocation.approve())) {
                        System.out.println(JSON.writerWithDefaultPrettyPrinter().writeValueAsString(workflow.run()));
                    }
                } else {
                    var env = context.getEnvironment();
                    String base = env.getRequiredProperty("HANDOFFGUARD_SERVER_URL");
                    String token = env.getRequiredProperty("HANDOFFGUARD_API_TOKEN");
                    try (var api = new ControlApi(base, token)) {
                        if (scenario.equals("server")) DemoScenarios.server(api);
                        else DemoScenarios.gateway(env.getRequiredProperty("HANDOFFGUARD_BINARY"), base, token, api);
                    }
                }
            }
        } catch (Exception error) {
            System.err.println("Workflow failed: " + error.getMessage());
            System.exit(1);
        }
    }
}
