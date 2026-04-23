package ai.drizz.farm;

/**
 * Raised for any non-2xx response from the drizz-farm daemon.
 * <p>
 * Includes the HTTP status and the server's error message so test
 * failures point at the actual cause instead of a generic
 * "request failed." The body field preserves the raw JSON in case
 * a caller wants to inspect error codes programmatically.
 */
public class DrizzError extends RuntimeException {
    private final int status;
    private final String body;

    public DrizzError(int status, String message, String body) {
        super("[" + status + "] " + message);
        this.status = status;
        this.body = body;
    }

    public int status() { return status; }
    public String body() { return body; }
}
