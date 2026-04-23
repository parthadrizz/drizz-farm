package ai.drizz.farm.testng;

import ai.drizz.farm.DrizzFarm;
import ai.drizz.farm.DrizzSession;
import ai.drizz.farm.SessionOptions;

import org.testng.IInvokedMethod;
import org.testng.IInvokedMethodListener;
import org.testng.ITestContext;
import org.testng.ITestListener;
import org.testng.ITestResult;

import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;

/**
 * TestNG integration — same pattern as the JUnit 5 extension. One
 * {@link DrizzSession} per test method, stashed as an attribute on
 * the test context so test code can reach it:
 *
 * <pre>{@code
 * @Listeners(DrizzFarmListener.class)
 * public class LoginTest {
 *     @Test
 *     public void testLogin(ITestContext ctx) {
 *         DrizzSession sess = DrizzFarmListener.session(ctx);
 *         sess.setGps(37.7749, -122.4194);
 *         sess.installApk(Files.readAllBytes(Paths.get("build/app.apk")));
 *         // ...
 *     }
 * }
 * }</pre>
 *
 * Configuration uses the same DRIZZ_* env vars / drizz.* system
 * properties as the JUnit 5 extension.
 */
public class DrizzFarmListener implements IInvokedMethodListener, ITestListener {
    private static final ConcurrentMap<String, DrizzSession> sessions = new ConcurrentHashMap<>();
    private static final ConcurrentMap<String, DrizzFarm> farms = new ConcurrentHashMap<>();

    /** Retrieve the current test's session from any {@link ITestContext}. */
    public static DrizzSession session(ITestContext ctx) {
        return sessions.get(key(ctx));
    }

    /** Retrieve the {@link DrizzFarm} used for this test run. */
    public static DrizzFarm farm(ITestContext ctx) {
        return farms.get(key(ctx));
    }

    @Override
    public void beforeInvocation(IInvokedMethod method, ITestResult result) {
        if (!method.isTestMethod()) return;
        DrizzFarm farm = new DrizzFarm(config("URL", "url", "http://localhost:9401"));
        SessionOptions opts = SessionOptions.builder()
                .profile(config("PROFILE", "profile", null))
                .deviceId(config("DEVICE_ID", "device_id", null))
                .recordVideo(bool("RECORD_VIDEO", "record_video", true))
                .captureLogcat(bool("CAPTURE_LOGCAT", "capture_logcat", true))
                .captureScreenshots(bool("CAPTURE_SCREENSHOTS", "capture_screenshots", true))
                .captureNetwork(bool("CAPTURE_NETWORK", "capture_network", false))
                .clientName(result.getName())
                .build();

        DrizzSession sess = farm.createSession(opts);
        String k = key(result);
        sessions.put(k, sess);
        farms.put(k, farm);
    }

    @Override
    public void afterInvocation(IInvokedMethod method, ITestResult result) {
        if (!method.isTestMethod()) return;
        String k = key(result);
        DrizzSession sess = sessions.remove(k);
        DrizzFarm farm = farms.remove(k);
        if (sess == null) return;
        // One extra screenshot if the test failed — before release, so
        // it actually captures the failure UI state.
        if (!result.isSuccess()) {
            try { sess.screenshot(); } catch (Exception ignored) {}
        }
        try { sess.release(); } catch (Exception ignored) {}
        try {
            sess.artifacts().forEach(a ->
                System.out.println("  drizz-farm " + a.type + ": " + farm.baseUrl() + a.url));
        } catch (Exception ignored) {}
    }

    // ---- Key helpers -------------------------------------------------

    private static String key(ITestResult r) {
        return r.getTestContext().getName() + "#" + r.getMethod().getQualifiedName() + "#" + r.getParameters().length;
    }
    private static String key(ITestContext ctx) {
        // Best-effort lookup by suite name — only one active session per
        // context is supported; parallel @Test methods inside the same
        // suite would need per-thread storage.
        return sessions.keySet().stream()
                .filter(k -> k.startsWith(ctx.getName() + "#"))
                .findFirst().orElse(ctx.getName());
    }

    // ---- Config helpers ---------------------------------------------

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
