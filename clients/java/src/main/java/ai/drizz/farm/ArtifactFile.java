package ai.drizz.farm;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * One persisted artifact for a session — video.mp4, logcat.txt,
 * a screenshot, or network.har. Returned by {@link DrizzSession#artifacts()}.
 * <p>
 * The {@code url} is a path relative to the daemon's base URL;
 * combine with {@link DrizzFarm#baseUrl()} to build a full
 * download link.
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public final class ArtifactFile {
    @JsonProperty("type")     public String type;
    @JsonProperty("filename") public String filename;
    @JsonProperty("size")     public long size;
    @JsonProperty("url")      public String url;

    @Override public String toString() {
        return type + " " + filename + " (" + size + " bytes)";
    }
}
