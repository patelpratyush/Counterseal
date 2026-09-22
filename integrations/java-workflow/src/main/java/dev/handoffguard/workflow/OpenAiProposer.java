package dev.handoffguard.workflow;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import tools.jackson.core.StreamReadFeature;
import tools.jackson.databind.DeserializationFeature;
import tools.jackson.databind.json.JsonMapper;
import tools.jackson.databind.JsonNode;
import static dev.handoffguard.workflow.ControlApi.JSON;

/** One bounded Responses API request per stage. No retries, redirects, or remote execution. */
final class OpenAiProposer implements ToolProposer, AutoCloseable {
    private static final JsonMapper STRICT = JsonMapper.builder()
            .enable(StreamReadFeature.STRICT_DUPLICATE_DETECTION)
            .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS).build();
    private final HttpClient http;
    private final URI endpoint;
    private final String key, model, task;

    OpenAiProposer(String key, String model, String task) {
        this(URI.create("https://api.openai.com/v1/responses"), key, model, task);
    }

    // Endpoint injection is package-private for loopback contract tests; the CLI pins OpenAI.
    OpenAiProposer(URI endpoint, String key, String model, String task) {
        if (key == null || key.isBlank() || key.contains("\r") || key.contains("\n"))
            throw new IllegalArgumentException("OPENAI_API_KEY is required");
        if (model == null || !model.matches("[a-zA-Z0-9._:-]{1,128}"))
            throw new IllegalArgumentException("OPENAI_MODEL must name a function-calling model");
        if (task == null || task.length() > 4096) throw new IllegalArgumentException("Model task exceeds 4096 characters");
        this.endpoint = endpoint; this.key = key; this.model = model; this.task = task;
        http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10))
                .followRedirects(HttpClient.Redirect.NEVER).build();
    }

    @Override public Map<String, Object> propose(Stage stage, Map<String, Object> request) {
        String name = stage.tool.replace('.', '_');
        var properties = new LinkedHashMap<String, Object>();
        request.forEach((field, value) -> properties.put(field, Map.of("type", value instanceof Number ? "integer" : "string")));
        var body = Map.of("model", model, "store", false, "max_output_tokens", 2048,
                "parallel_tool_calls", false, "tool_choice", Map.of("type", "function", "name", name),
                "tools", List.of(Map.of("type", "function", "name", name, "strict", true,
                        "description", "Propose the " + stage + " stage; Counterseal separately enforces authority.",
                        "parameters", Map.of("type", "object", "properties", properties,
                                "required", List.copyOf(properties.keySet()), "additionalProperties", false))),
                "instructions", "Propose exactly one tool call for the current commerce stage. Use the supplied request values unless the task asks otherwise. You cannot approve actions or change policy. Do not invent receipts.",
                "input", JSON.writeValueAsString(Map.of("stage", stage.name(), "request", request, "task", task)));
        var outbound = HttpRequest.newBuilder(endpoint).timeout(Duration.ofSeconds(60))
                .header("Authorization", "Bearer " + key).header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(JSON.writeValueAsString(body))).build();
        try {
            var response = http.send(outbound, HttpResponse.BodyHandlers.ofByteArray());
            if (response.statusCode() != 200) throw new IllegalStateException("Model provider failed: HTTP " + response.statusCode());
            if (response.body().length > 256 * 1024) throw new IllegalStateException("Model response exceeds 256 KiB");
            return parse(STRICT.readTree(response.body()), name, request);
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("Model request interrupted");
        } catch (IOException error) {
            throw new IllegalStateException("Model provider unavailable");
        } catch (tools.jackson.core.JacksonException error) {
            throw new IllegalStateException("Malformed model response");
        }
    }

    static Map<String, Object> parse(JsonNode response, String name, Map<String, Object> request) {
        if (!"completed".equals(response.path("status").asText()) || !response.path("output").isArray())
            throw new IllegalStateException("Model response incomplete or refused");
        JsonNode call = null;
        for (var item : response.path("output")) {
            if ("reasoning".equals(item.path("type").asText())) continue;
            if (!"function_call".equals(item.path("type").asText()) || call != null)
                throw new IllegalStateException("Model must propose exactly one function call");
            call = item;
        }
        if (call == null || !name.equals(call.path("name").asText()) || !call.path("arguments").isTextual())
            throw new IllegalStateException("Model proposed an unexpected tool");
        var args = STRICT.readTree(call.path("arguments").asText());
        if (!args.isObject() || args.size() != request.size()) throw new IllegalStateException("Invalid proposal fields");
        var result = new LinkedHashMap<String, Object>();
        request.forEach((field, expected) -> {
            var value = args.path(field);
            if (expected instanceof Number) {
                if (!value.isIntegralNumber() || !value.canConvertToInt() || value.asInt() <= 0)
                    throw new IllegalStateException("Invalid proposal amount");
                result.put(field, value.asInt());
            } else {
                if (!value.isTextual() || value.asText().isBlank() || value.asText().length() > 128)
                    throw new IllegalStateException("Invalid proposal identifier");
                result.put(field, value.asText());
            }
        });
        return Map.copyOf(result);
    }

    @Override public void close() { http.close(); }
}
