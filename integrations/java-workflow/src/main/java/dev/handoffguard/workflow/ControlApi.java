package dev.handoffguard.workflow;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.Set;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.json.JsonMapper;

/** Trusted control-plane client. Credentials never enter tool arguments. */
public final class ControlApi implements AutoCloseable {
    static final JsonMapper JSON = JsonMapper.builder().build();
    private final URI base;
    private final String token;
    private final HttpClient http;

    public ControlApi(String url, String token) {
        this.base = URI.create(url);
        boolean secure = "https".equals(base.getScheme());
        boolean local = "http".equals(base.getScheme()) && base.getHost() != null
                && Set.of("localhost", "127.0.0.1", "[::1]").contains(base.getHost());
        if ((!secure && !local) || base.getHost() == null || base.getUserInfo() != null
                || base.getQuery() != null || base.getFragment() != null
                || !(base.getPath().isEmpty() || base.getPath().equals("/"))) {
            throw new IllegalArgumentException("Control API requires an HTTPS origin or loopback HTTP origin");
        }
        if (token == null || token.isBlank() || token.contains("\n") || token.contains("\r")) {
            throw new IllegalArgumentException("Control API token is required");
        }
        this.token = token;
        this.http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10))
                .followRedirects(HttpClient.Redirect.NEVER).build();
    }

    public JsonNode request(String method, String path, JsonNode body, int expected) {
        if (!path.startsWith("/v1/") || path.contains("..")) throw new IllegalArgumentException("Invalid API path");
        var builder = HttpRequest.newBuilder(base.resolve(path)).timeout(Duration.ofSeconds(20))
                .header("Authorization", "Bearer " + token).header("Content-Type", "application/json");
        builder.method(method, body == null ? HttpRequest.BodyPublishers.noBody()
                : HttpRequest.BodyPublishers.ofString(JSON.writeValueAsString(body)));
        try {
            var response = http.send(builder.build(), HttpResponse.BodyHandlers.ofString());
            if (response.statusCode() != expected) throw new Rejected(response.statusCode());
            return JSON.readTree(response.body());
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("Workflow interrupted", error);
        } catch (IOException error) {
            throw new IllegalStateException("Control API unavailable", error);
        }
    }

    public JsonNode post(String path, JsonNode body, int expected) { return request("POST", path, body, expected); }
    public JsonNode get(String path) { return request("GET", path, null, 200); }
    @Override public void close() { http.close(); }

    public static final class Rejected extends RuntimeException {
        private final int status;
        Rejected(int status) { super("HandoffGuard request denied or failed: HTTP " + status); this.status = status; }
        public int status() { return status; }
    }
}
