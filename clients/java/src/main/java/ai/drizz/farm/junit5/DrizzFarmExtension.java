package ai.drizz.farm.junit5;

import ai.drizz.farm.DrizzFarm;
import ai.drizz.farm.DrizzSession;
import ai.drizz.farm.SessionOptions;

import org.junit.jupiter.api.extension.AfterEachCallback;
import org.junit.jupiter.api.extension.BeforeEachCallback;
import org.junit.jupiter.api.extension.ExtensionContext;
import org.junit.jupiter.api.extension.ExtensionContext.Namespace;
import org.junit.jupiter.api.extension.ExtensionContext.Store;
import org.junit.jupiter.api.extension.ParameterContext;
import org.junit.jupiter.api.extension.ParameterResolutionException;
import org.junit.jupiter.api.extension.ParameterResolver;
import org.junit.jupiter.api.extension.TestWatcher;

import java.util.Optional;

/**
 * JUnit 5 extension — one {@link DrizzSession} per test method,
 * auto-released at teardown.
 *
 * <pre>{@code
 * @ExtendWith(DrizzFarmExtension.class)
 * class LoginTest {
 *     @Test
 *     void testLogin(DrizzSession sess) {
 *         sess.setGps(37.7749, -122.4194);
 *         sess.installApk(Files.readAllBytes(Paths.get("build/app.apk")));
 *         // ... drive your test ...
 *     }
 * }
 * }</pre>
 *
 * Configuration is taken from system properties / env variables
 * (same keys as the Python client):
 * <ul>
 *   <li>{@code DRIZZ_URL} / {@code drizz.url} — daemon URL
 *       (default {@code http://localhost:9401})</li>
 *   <li>{@code DRIZZ_PROFILE} / {@code drizz.profile}</li>
 *   <li>{@code DRIZZ_DEVICE_ID} / {@code drizz.device_id}</li>
 *   <li>{@code DRIZZ_RECORD_VIDEO} / {@code drizz.record_video}</li>
 *   <li>{@code DRIZZ_CAPTURE_LOGCAT} / {@code drizz.capture_logcat}</li>
 *   <li>{@code DRIZZ_CAPTURE_SCREENSHOTS} / {@code drizz.capture_screenshots}</li>
 *   <li>{@code DRIZZ_CAPTURE_NETWORK} / {@code drizz.capture_network}</li>
 * </ul>
 *
 * On test failure, the extension stashes one final screenshot on the
 * test context so reports can pick it up.
 */
public class DrizzFarmExtension
        implements BeforeEachCallback, AfterEachCallback, ParameterResolver, TestWatcher {

    private static final Namespace NS = Namespace.create(DrizzFarmExtension.class);

    @Override
    public void beforeEach(ExtensionContext ctx) {
        DrizzFarm farm = new DrizzFarm(config("URL", "url", "http://localhost:9401"));
        SessionOptions opts = SessionOptions.builder()
                .profile(config("PROFILE", "profile", null))
                .deviceId(config("DEVICE_ID", "device_id", null))
                .recordVideo(bool("RECORD_VIDEO", "record_video", true))
                .captureLogcat(bool("CAPTURE_LOGCAT", "capture_logcat", true))
                .captureScreenshots(bool("CAPTURE_SCREENSHOTS", "capture_screenshots", true))
                .captureNetwork(bool("CAPTURE_NETWORK", "capture_network", false))
                .clientName(ctx.getDisplayName())
                .build();

        DrizzSession sess = farm.createSession(opts);
        Store store = ctx.getStore(NS);
        store.put("farm", farm);
        store.put("session", sess);
    }

    @Override
    public void afterEach(ExtensionContext ctx) {
        Store store = ctx.getStore(NS);
        DrizzSession sess = store.get("session", DrizzSession.class);
        DrizzFarm farm = store.get("farm", DrizzFarm.class);
        if (sess != null) {
            try { sess.release(); } catch (Exception ignored) {}
            // Print artifact URLs so CI logs contain clickable links —
            // mirrors what the Python plugin does at teardown.
            try {
                sess.artifacts().forEach(a ->
                        System.out.println("  drizz-farm " + a.type + ": " + farm.baseUrl() + a.url));
            } catch (Exception ignored) {}
        }
    }

    @Override
    public boolean supportsParameter(ParameterContext pctx, ExtensionContext ectx)
            throws ParameterResolutionException {
        return pctx.getParameter().getType().equals(DrizzSession.class);
    }

    @Override
    public Object resolveParameter(ParameterContext pctx, ExtensionContext ectx)
            throws ParameterResolutionException {
        return ectx.getStore(NS).get("session", DrizzSession.class);
    }

    @Override
    public void testFailed(ExtensionContext ctx, Throwable cause) {
        DrizzSession sess = ctx.getStore(NS).get("session", DrizzSession.class);
        if (sess == null) return;
        try {
            byte[] png = sess.screenshot();
            ctx.publishReportEntry("drizz_failure_screenshot_bytes", String.valueOf(png.length));
        } catch (Exception ignored) {
            // best-effort: don't mask the real test failure
        }
    }

    // ---- config helpers ---------------------------------------------

    private static String config(String envKey, String propKey, String dflt) {
        String v = System.getenv("DRIZZ_" + envKey);
        if (v != null && !v.isEmpty()) return v;
        v = System.getProperty("drizz." + propKey);
        if (v != null && !v.isEmpty()) return v;
        return dflt;
    }

    private static boolean bool(String envKey, String propKey, boolean dflt) {
        String v = Optional.ofNullable(System.getenv("DRIZZ_" + envKey))
                .orElse(System.getProperty("drizz." + propKey, String.valueOf(dflt)));
        return "true".equalsIgnoreCase(v) || "1".equals(v) || "yes".equalsIgnoreCase(v);
    }
}
