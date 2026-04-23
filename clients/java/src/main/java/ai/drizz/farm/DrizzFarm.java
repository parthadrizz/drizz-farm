package ai.drizz.farm;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.Map;
import java.util.Objects;

/**
 * Entry point to a running drizz-farm daemon.
 * <p>
 * One {@code DrizzFarm} wraps the base URL + a shared {@link HttpClient}
 * so connection pooling happens transparently. {@link #createSession}
 * returns a {@link DrizzSession} bound to this farm.
 *
 * <pre>{@code
 * try (DrizzFarm farm = new DrizzFarm("http://farm.local:9401")) {
 *     DrizzSession sess = farm.createSession(
 *         SessionOptions.builder()
 *             .recordVideo(true)
 *             .captureLogcat(true)
 *             .build());
 *     try {
 *         sess.setGps(37.7749, -122.4194);
 *         sess.installApk(Files.readAllBytes(Paths.get("build/app.apk")));
 *         // ... run your test ...
 *     } finally {
 *         sess.release();
 *     }
 * }
 * }</pre>
 */
public final class DrizzFarm implements AutoCloseable {
    private final String baseUrl;
    final String apiRoot;
    final HttpClient http;
    final ObjectMapper mapper = new ObjectMapper();

    /** Connects to the farm at {@code baseUrl}, e.g. {@code http://farm.local:9401}. */
    public DrizzFarm(String baseUrl) {
        this(baseUrl, Duration.ofSeconds(30));
    }

    public DrizzFarm(String baseUrl, Duration connectTimeout) {
        this.baseUrl = trimRight(Objects.requireNonNull(baseUrl, "baseUrl"), "/");
        this.apiRoot = this.baseUrl + "/api/v1";
        this.http = HttpClient.newBuilder()
                .connectTimeout(connectTimeout)
                .version(HttpClient.Version.HTTP_1_1)
                .build();
    }

    public String baseUrl() { return baseUrl; }

    // ---- Health / discovery ------------------------------------------

    public JsonNode health() {
        return getJson("/node/health");
    }

    public JsonNode pool() {
        return getJson("/pool");
    }

    /**
     * List devices the daemon knows about. Pass filter params:
     * {@code free=true}, {@code profile=api34_play}, etc.
     */
    public JsonNode devices(Map<String, String> filters) {
        StringBuilder q = new StringBuilder();
        if (filters != null) {
            for (Map.Entry<String, String> e : filters.entrySet()) {
                q.append(q.length() == 0 ? "?" : "&");
                q.append(e.getKey()).append("=").append(e.getValue());
            }
        }
        return getJson("/devices" + q);
    }

    // ---- Session lifecycle -------------------------------------------

    /** Create a session with {@link SessionOptions}. Blocks until the
     *  session is active (or throws on failure). */
    public DrizzSession createSession(SessionOptions opts) {
        ObjectNode body = mapper.createObjectNode();
        body.put("source", opts.source == null ? "java-client" : opts.source);
        if (opts.profile != null) body.put("profile", opts.profile);
        if (opts.deviceId != null) body.put("device_id", opts.deviceId);
        if (opts.avdName != null) body.put("avd_name", opts.avdName);
        if (opts.timeoutMinutes != null) body.put("timeout_minutes", opts.timeoutMinutes);
        if (opts.clientName != null) body.put("client_name", opts.clientName);

        ObjectNode caps = body.putObject("capabilities");
        caps.put("record_video", opts.recordVideo);
        caps.put("capture_logcat", opts.captureLogcat);
        caps.put("capture_screenshots", opts.captureScreenshots);
        caps.put("capture_network", opts.captureNetwork);
        if (opts.retentionHours != null) caps.put("retention_hours", opts.retentionHours);

        JsonNode resp = postJson("/sessions", body);
        DrizzSession sess = new DrizzSession(this, resp);
        if (opts.waitUntilActive && !"active".equals(sess.state())) {
            sess.waitUntilActive(opts.waitTimeout);
        }
        return sess;
    }

    /** Convenience — create a session with default capture settings. */
    public DrizzSession createSession() {
        return createSession(SessionOptions.builder().build());
    }

    // ---- HTTP helpers used by DrizzSession ---------------------------

    JsonNode getJson(String path) {
        HttpRequest req = HttpRequest.newBuilder(URI.create(apiRoot + path))
                .GET()
                .build();
        return send(req);
    }

    JsonNode postJson(String path, JsonNode body) {
        try {
            byte[] bytes = mapper.writeValueAsBytes(body);
            HttpRequest req = HttpRequest.newBuilder(URI.create(apiRoot + path))
                    .header("Content-Type", "application/json")
                    .POST(HttpRequest.BodyPublishers.ofByteArray(bytes))
                    .build();
            return send(req);
        } catch (IOException e) {
            throw new DrizzError(0, "serialize body: " + e.getMessage(), null);
        }
    }

    JsonNode deleteJson(String path) {
        HttpRequest req = HttpRequest.newBuilder(URI.create(apiRoot + path))
                .DELETE()
                .build();
        return send(req);
    }

    JsonNode send(HttpRequest req) {
        try {
            HttpResponse<byte[]> resp = http.send(req, HttpResponse.BodyHandlers.ofByteArray());
            byte[] body = resp.body() == null ? new byte[0] : resp.body();
            if (resp.statusCode() < 200 || resp.statusCode() >= 300) {
                throw decodeError(resp.statusCode(), body);
            }
            if (body.length == 0) return mapper.createObjectNode();
            return mapper.readTree(body);
        } catch (IOException | InterruptedException e) {
            throw new DrizzError(0, "request failed: " + e.getMessage(), null);
        }
    }

    byte[] sendBytes(HttpRequest req) {
        try {
            HttpResponse<byte[]> resp = http.send(req, HttpResponse.BodyHandlers.ofByteArray());
            byte[] body = resp.body() == null ? new byte[0] : resp.body();
            if (resp.statusCode() < 200 || resp.statusCode() >= 300) {
                throw decodeError(resp.statusCode(), body);
            }
            return body;
        } catch (IOException | InterruptedException e) {
            throw new DrizzError(0, "request failed: " + e.getMessage(), null);
        }
    }

    private DrizzError decodeError(int status, byte[] body) {
        String raw = new String(body);
        String msg = "HTTP " + status;
        try {
            JsonNode n = mapper.readTree(body);
            if (n.has("message")) msg = n.get("message").asText(msg);
            else if (n.has("error")) msg = n.get("error").asText(msg);
        } catch (IOException ignored) {
            if (!raw.isEmpty()) msg = raw.length() > 200 ? raw.substring(0, 200) : raw;
        }
        return new DrizzError(status, msg, raw);
    }

    @Override
    public void close() {
        // java.net.http.HttpClient has no close() — the underlying
        // connections are released when the client is GC'd. This
        // method is here purely so callers can use try-with-resources.
    }

    private static String trimRight(String s, String suffix) {
        while (s.endsWith(suffix)) s = s.substring(0, s.length() - suffix.length());
        return s;
    }
}
