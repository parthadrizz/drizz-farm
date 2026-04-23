package ai.drizz.farm;

import java.time.Duration;

/**
 * Builder-style options for {@link DrizzFarm#createSession}. Mirrors
 * the Python client's keyword args. All fields have sensible defaults;
 * the no-arg builder creates a "just give me any free device, don't
 * capture anything" session.
 *
 * <pre>{@code
 * SessionOptions opts = SessionOptions.builder()
 *     .profile("api34_play")
 *     .recordVideo(true)
 *     .captureLogcat(true)
 *     .timeoutMinutes(30)
 *     .build();
 * }</pre>
 */
public final class SessionOptions {
    public final String profile;
    public final String deviceId;
    public final String avdName;
    public final String source;
    public final String clientName;
    public final Integer timeoutMinutes;
    public final boolean recordVideo;
    public final boolean captureLogcat;
    public final boolean captureScreenshots;
    public final boolean captureNetwork;
    public final Integer retentionHours;
    public final boolean waitUntilActive;
    public final Duration waitTimeout;

    private SessionOptions(Builder b) {
        this.profile = b.profile;
        this.deviceId = b.deviceId;
        this.avdName = b.avdName;
        this.source = b.source;
        this.clientName = b.clientName;
        this.timeoutMinutes = b.timeoutMinutes;
        this.recordVideo = b.recordVideo;
        this.captureLogcat = b.captureLogcat;
        this.captureScreenshots = b.captureScreenshots;
        this.captureNetwork = b.captureNetwork;
        this.retentionHours = b.retentionHours;
        this.waitUntilActive = b.waitUntilActive;
        this.waitTimeout = b.waitTimeout;
    }

    public static Builder builder() { return new Builder(); }

    public static final class Builder {
        String profile, deviceId, avdName, source, clientName;
        Integer timeoutMinutes, retentionHours;
        boolean recordVideo = false;
        boolean captureLogcat = false;
        boolean captureScreenshots = true;   // on-demand screenshot API works by default
        boolean captureNetwork = false;
        boolean waitUntilActive = true;
        Duration waitTimeout = Duration.ofSeconds(90);

        public Builder profile(String v)           { this.profile = v; return this; }
        public Builder deviceId(String v)          { this.deviceId = v; return this; }
        public Builder avdName(String v)           { this.avdName = v; return this; }
        public Builder source(String v)            { this.source = v; return this; }
        public Builder clientName(String v)        { this.clientName = v; return this; }
        public Builder timeoutMinutes(Integer v)   { this.timeoutMinutes = v; return this; }
        public Builder retentionHours(Integer v)   { this.retentionHours = v; return this; }
        public Builder recordVideo(boolean v)      { this.recordVideo = v; return this; }
        public Builder captureLogcat(boolean v)    { this.captureLogcat = v; return this; }
        public Builder captureScreenshots(boolean v) { this.captureScreenshots = v; return this; }
        public Builder captureNetwork(boolean v)   { this.captureNetwork = v; return this; }
        public Builder waitUntilActive(boolean v)  { this.waitUntilActive = v; return this; }
        public Builder waitTimeout(Duration v)     { this.waitTimeout = v; return this; }

        public SessionOptions build() { return new SessionOptions(this); }
    }
}
