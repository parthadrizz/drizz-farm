package ai.drizz.farm;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpRequest;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Objects;
import java.util.UUID;

/**
 * Active handle to one session on the farm. Every device API is a
 * method here; they map 1:1 to the HTTP endpoints under
 * {@code /api/v1/sessions/{id}/}.
 * <p>
 * Implements {@link AutoCloseable} so test framework integrations
 * can use try-with-resources — {@link #close()} just calls
 * {@link #release()}.
 */
public final class DrizzSession implements AutoCloseable {
    private final DrizzFarm farm;
    private JsonNode data;

    DrizzSession(DrizzFarm farm, JsonNode data) {
        this.farm = Objects.requireNonNull(farm);
        this.data = Objects.requireNonNull(data);
    }

    // ---- Introspection ----------------------------------------------

    public String id()         { return data.path("id").asText(); }
    public String state()      { return data.path("state").asText("unknown"); }
    public String instanceId() { return data.path("instance_id").asText(); }
    public String serial()     { return data.path("connection").path("adb_serial").asText(); }
    public String appiumUrl()  { return data.path("connection").path("appium_url").asText(null); }
    public JsonNode raw()      { return data; }

    public JsonNode refresh() {
        this.data = farm.getJson("/sessions/" + id());
        return data;
    }

    public void waitUntilActive(Duration timeout) {
        Instant deadline = Instant.now().plus(timeout);
        while (Instant.now().isBefore(deadline)) {
            refresh();
            String s = state();
            if ("active".equals(s)) return;
            if ("released".equals(s) || "timed_out".equals(s) || "error".equals(s)) {
                throw new DrizzError(0, "session ended in state=" + s + " before becoming active", null);
            }
            try { Thread.sleep(1000); } catch (InterruptedException e) { Thread.currentThread().interrupt(); break; }
        }
        throw new DrizzError(0, "session did not reach active within " + timeout + " (last state=" + state() + ")", null);
    }

    // ---- Device simulation ------------------------------------------

    public JsonNode setGps(double latitude, double longitude) {
        ObjectNode body = farm.mapper.createObjectNode();
        body.put("latitude", latitude);
        body.put("longitude", longitude);
        return post("gps", body);
    }

    /** Profiles: 2g, 3g, 4g, 5g, wifi_slow, wifi_fast, offline, flaky. */
    public JsonNode setNetwork(String profile)             { return post("network", prop("profile", profile)); }
    public JsonNode setBattery(int level)                  { return post("battery", farm.mapper.createObjectNode().put("level", level)); }
    public JsonNode setOrientation(String orientation)     { return post("orientation", prop("orientation", orientation)); }
    public JsonNode setLocale(String locale)               { return post("locale", prop("locale", locale)); }
    public JsonNode setTimezone(String timezone)           { return post("timezone", prop("timezone", timezone)); }
    public JsonNode setDarkMode(boolean dark)              { return post("appearance", farm.mapper.createObjectNode().put("dark", dark)); }
    public JsonNode setFontScale(double scale)             { return post("font-scale", farm.mapper.createObjectNode().put("scale", scale)); }
    public JsonNode setAnimations(boolean enabled)         { return post("animations", farm.mapper.createObjectNode().put("enabled", enabled)); }
    public JsonNode setVolume(int level)                   { return post("volume", farm.mapper.createObjectNode().put("level", level)); }
    public JsonNode setClipboard(String text)              { return post("clipboard", prop("text", text)); }
    public JsonNode setSensor(String name, String values)  {
        ObjectNode b = farm.mapper.createObjectNode();
        b.put("name", name);
        b.put("values", values);
        return post("sensor", b);
    }
    public JsonNode shake()                                { return post("shake", farm.mapper.createObjectNode()); }
    public JsonNode lock()                                 { return post("lock", prop("action", "lock")); }
    public JsonNode unlock()                               { return post("lock", prop("action", "unlock")); }
    public JsonNode pressKey(String keycode)               { return post("key", prop("keycode", keycode)); }

    // ---- Apps --------------------------------------------------------

    /** Upload the APK bytes and install it on the device. */
    public JsonNode installApk(byte[] apkBytes) {
        byte[] multipart = buildMultipart("apk", "app.apk",
                "application/vnd.android.package-archive", apkBytes, null);
        return postMultipart("/sessions/" + id() + "/install", multipart);
    }

    /** Install an APK that already lives on the daemon host's filesystem. */
    public JsonNode installApkPath(String hostPath) {
        return post("install", prop("path", hostPath));
    }

    public JsonNode uninstall(String pkg)                         { return post("uninstall", prop("package", pkg)); }
    public JsonNode clearData(String pkg)                         { return post("clear-data", prop("package", pkg)); }
    public JsonNode grantPermission(String pkg, String perm)      { return permissions(pkg, perm, true); }
    public JsonNode revokePermission(String pkg, String perm)     { return permissions(pkg, perm, false); }

    private JsonNode permissions(String pkg, String perm, boolean grant) {
        ObjectNode b = farm.mapper.createObjectNode();
        b.put("package", pkg);
        b.put("permission", perm);
        b.put("grant", grant);
        return post("permissions", b);
    }

    public JsonNode openDeeplink(String url) { return post("deeplink", prop("url", url)); }

    // ---- Files + media ----------------------------------------------

    /**
     * Push bytes to the device. {@code target} defaults to
     * {@code /sdcard/Download/<filename>}.
     */
    public JsonNode uploadFile(byte[] data, String filename, String target) {
        String boundaryFields = target == null ? null : "target=" + target;
        byte[] body = buildMultipart("file", filename, "application/octet-stream", data, boundaryFields);
        return postMultipart("/sessions/" + id() + "/files/upload", body);
    }

    /**
     * Drop an image into the device gallery (/sdcard/DCIM/Camera) +
     * trigger the media scanner, so the app's file picker sees it.
     */
    public JsonNode injectCameraImage(byte[] imageBytes, String filename) {
        byte[] body = buildMultipart("image", filename, "image/jpeg", imageBytes, null);
        return postMultipart("/sessions/" + id() + "/camera", body);
    }

    /** Capture a screenshot. Requires captureScreenshots=true on create. */
    public byte[] screenshot() {
        HttpRequest req = HttpRequest.newBuilder(URI.create(farm.apiRoot + "/sessions/" + id() + "/screenshot"))
                .timeout(Duration.ofSeconds(10))
                .POST(HttpRequest.BodyPublishers.noBody())
                .build();
        return farm.sendBytes(req);
    }

    // ---- Biometric + notifications ----------------------------------

    public JsonNode enrollFingerprint()                       { return post("biometric", prop("action", "enroll")); }
    public JsonNode fingerprintTouch()                        { return post("biometric", prop("action", "touch")); }
    public JsonNode fingerprintTouchFail()                    { return post("biometric", prop("action", "fail")); }

    public JsonNode pushNotification(String title, String body) {
        return pushNotification(title, body, null);
    }
    public JsonNode pushNotification(String title, String body, String tag) {
        ObjectNode b = farm.mapper.createObjectNode();
        b.put("title", title);
        b.put("body", body);
        if (tag != null) b.put("tag", tag);
        return post("push-notification", b);
    }

    // ---- Raw ADB -----------------------------------------------------

    /** Run a shell command on the device and return stdout. */
    public String shell(String command) {
        JsonNode resp = post("adb", prop("command", command));
        return resp.path("output").asText("");
    }

    // ---- Artifacts ---------------------------------------------------

    public List<ArtifactFile> artifacts() {
        JsonNode resp = farm.getJson("/sessions/" + id() + "/artifacts");
        try {
            return farm.mapper.convertValue(resp.path("artifacts"),
                    new TypeReference<List<ArtifactFile>>() {});
        } catch (IllegalArgumentException e) {
            return new ArrayList<>();
        }
    }

    public byte[] downloadArtifact(String filename) {
        HttpRequest req = HttpRequest.newBuilder(URI.create(farm.apiRoot + "/sessions/" + id() + "/artifacts/" + filename))
                .timeout(Duration.ofSeconds(60))
                .GET()
                .build();
        return farm.sendBytes(req);
    }

    // ---- Release -----------------------------------------------------

    /** Release the session. Idempotent — safe to call from cleanup/finally. */
    public void release() {
        try {
            farm.deleteJson("/sessions/" + id());
        } catch (DrizzError e) {
            // 404 = already released; anything else is best-effort —
            // we don't want a cleanup hiccup to mask the real test
            // failure.
        }
    }

    @Override public void close() { release(); }

    // ---- Internals ---------------------------------------------------

    private JsonNode post(String path, JsonNode body) {
        return farm.postJson("/sessions/" + id() + "/" + path, body);
    }

    private ObjectNode prop(String k, String v) {
        ObjectNode n = farm.mapper.createObjectNode();
        n.put(k, v);
        return n;
    }

    private JsonNode postMultipart(String path, byte[] body) {
        String boundary = "----drizz" + UUID.randomUUID().toString().replace("-", "");
        // The body passed in was built with a fresh boundary — keep a
        // simple shape and regenerate so caller doesn't have to know.
        return farm.send(HttpRequest.newBuilder(URI.create(farm.apiRoot + path))
                .header("Content-Type", "multipart/form-data; boundary=" + extractBoundary(body, boundary))
                .POST(HttpRequest.BodyPublishers.ofByteArray(body))
                .timeout(Duration.ofMinutes(2))
                .build());
    }

    /** Peek at the first line of the multipart body to recover its
     *  boundary — buildMultipart baked it in. */
    private String extractBoundary(byte[] body, String fallback) {
        // The body starts with "--<boundary>\r\n"
        int nl = -1;
        for (int i = 0; i < Math.min(body.length, 256); i++) {
            if (body[i] == '\r') { nl = i; break; }
        }
        if (nl <= 2) return fallback;
        String first = new String(body, 0, nl);
        if (first.startsWith("--")) return first.substring(2);
        return fallback;
    }

    /**
     * Build a multipart/form-data body with one file field and optional
     * extra text fields. Kept self-contained so we don't pull in
     * Apache HttpClient or OkHttp just for this.
     *
     * {@code extraFields} is urlencoded-style "k=v&k2=v2" or null.
     */
    private byte[] buildMultipart(String fieldName, String filename, String mime,
                                   byte[] data, String extraFields) {
        String boundary = "----drizz" + UUID.randomUUID().toString().replace("-", "");
        ByteArrayOutputStream out = new ByteArrayOutputStream(data.length + 512);
        try {
            if (extraFields != null && !extraFields.isEmpty()) {
                for (String kv : extraFields.split("&")) {
                    int eq = kv.indexOf('=');
                    if (eq < 0) continue;
                    writeLine(out, "--" + boundary);
                    writeLine(out, "Content-Disposition: form-data; name=\"" + kv.substring(0, eq) + "\"");
                    writeLine(out, "");
                    writeLine(out, kv.substring(eq + 1));
                }
            }
            writeLine(out, "--" + boundary);
            writeLine(out, "Content-Disposition: form-data; name=\"" + fieldName + "\"; filename=\"" + filename + "\"");
            writeLine(out, "Content-Type: " + mime);
            writeLine(out, "");
            out.write(data);
            writeLine(out, "");
            writeLine(out, "--" + boundary + "--");
        } catch (IOException e) {
            throw new DrizzError(0, "multipart build: " + e.getMessage(), null);
        }
        return out.toByteArray();
    }

    private void writeLine(ByteArrayOutputStream out, String line) throws IOException {
        out.write(line.getBytes());
        out.write(0x0D); out.write(0x0A);
    }
}
