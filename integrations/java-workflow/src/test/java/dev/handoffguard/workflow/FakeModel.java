package dev.handoffguard.workflow;

import com.sun.net.httpserver.HttpServer;
import java.net.InetSocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.function.Function;
import tools.jackson.databind.JsonNode;
import static dev.handoffguard.workflow.ControlApi.JSON;

/** Exercises the real HTTP adapter without provider credentials or nondeterministic output. */
final class FakeModel implements AutoCloseable {
    final HttpServer server;
    final List<JsonNode> requests = new ArrayList<>();
    final Function<JsonNode, JsonNode> respond;
    FakeModel(Function<JsonNode, JsonNode> respond) throws Exception {
        this(200, respond);
    }
    FakeModel(int status, Function<JsonNode, JsonNode> respond) throws Exception {
        this.respond = respond;
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/v1/responses", exchange -> {
            var body = JSON.readTree(exchange.getRequestBody().readAllBytes());
            synchronized (requests) { requests.add(body); }
            byte[] response = JSON.writeValueAsBytes(respond.apply(body));
            exchange.getResponseHeaders().set("Content-Type", "application/json");
            exchange.sendResponseHeaders(status, response.length);
            try (var out = exchange.getResponseBody()) { out.write(response); }
        });
        server.start();
    }
    OpenAiProposer client() {
        return new OpenAiProposer(URI.create("http://127.0.0.1:" + server.getAddress().getPort() + "/v1/responses"),
                "test-key", "test-model", "Complete the demo task");
    }
    static JsonNode echo(JsonNode request) {
        return call(request.path("tool_choice").path("name").asText(),
                JSON.readTree(request.path("input").asText()).path("request"));
    }
    static JsonNode call(String name, JsonNode arguments) {
        return JSON.valueToTree(Map.of("status", "completed", "output", List.of(Map.of(
                "type", "function_call", "name", name, "arguments", JSON.writeValueAsString(arguments)))));
    }
    @Override public void close() { server.stop(0); }
}
