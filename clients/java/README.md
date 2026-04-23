# drizz-farm — Java client + JUnit 5 + TestNG

Thin Java library mirroring the [Python client](../python/) surface, with first-class integrations for JUnit 5 and TestNG.

## Install

### Maven
```xml
<dependency>
    <groupId>ai.drizz</groupId>
    <artifactId>drizz-farm-client</artifactId>
    <version>0.1.0</version>
</dependency>
```

### Gradle
```groovy
testImplementation "ai.drizz:drizz-farm-client:0.1.0"
```

Java 11+. One runtime dependency: Jackson. JUnit 5 and TestNG are marked `provided` — bring your own.

## Direct client

```java
import ai.drizz.farm.DrizzFarm;
import ai.drizz.farm.DrizzSession;
import ai.drizz.farm.SessionOptions;

try (DrizzFarm farm = new DrizzFarm("http://farm.local:9401")) {
    DrizzSession sess = farm.createSession(
        SessionOptions.builder()
            .recordVideo(true)
            .captureLogcat(true)
            .build());
    try {
        // Install your APK from bytes — works from any CI
        sess.installApk(Files.readAllBytes(Paths.get("build/app.apk")));

        // Drop a test image into the gallery
        sess.injectCameraImage(Files.readAllBytes(Paths.get("fixtures/selfie.jpg")), "selfie.jpg");

        // Simulate conditions
        sess.setGps(37.7749, -122.4194);
        sess.setNetwork("3g");
        sess.setBattery(15);
        sess.setLocale("ja_JP");

        // Drive the app — via Appium using sess.appiumUrl(),
        // raw ADB with sess.shell(...), or anything that can target
        // sess.serial() (an ADB serial).
        byte[] png = sess.screenshot();
        Files.write(Paths.get("after.png"), png);
    } finally {
        sess.release();  // video.mp4 / logcat.txt / network.har auto-finalize
    }

    sess.artifacts().forEach(a ->
        System.out.println(a.type + " " + a.url + " (" + a.size + " bytes)"));
}
```

## JUnit 5

```java
import ai.drizz.farm.DrizzSession;
import ai.drizz.farm.junit5.DrizzFarmExtension;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;

@ExtendWith(DrizzFarmExtension.class)
class LoginTest {

    @Test
    void testLogin(DrizzSession sess) throws Exception {
        sess.setGps(37.7749, -122.4194);
        sess.installApk(Files.readAllBytes(Paths.get("build/app.apk")));
        // ... drive Appium via sess.appiumUrl() or Espresso/UiAutomator directly ...
        assertTrue(sess.shell("dumpsys activity top").contains("MainActivity"));
    }
}
```

On test failure, the extension captures a final screenshot. Artifact URLs print to stdout at teardown so CI logs have clickable links to the video/logcat/network HAR.

## TestNG

```java
import ai.drizz.farm.testng.DrizzFarmListener;
import ai.drizz.farm.DrizzSession;
import org.testng.annotations.Listeners;
import org.testng.annotations.Test;
import org.testng.ITestContext;

@Listeners(DrizzFarmListener.class)
public class LoginTest {

    @Test
    public void testLogin(ITestContext ctx) throws Exception {
        DrizzSession sess = DrizzFarmListener.session(ctx);
        sess.setGps(37.7749, -122.4194);
        sess.installApk(Files.readAllBytes(Paths.get("build/app.apk")));
        // ... run Appium / Espresso / UiAutomator2 ...
    }
}
```

## Configuration

All knobs settable via env var or JVM system property. Env wins.

| Key | System prop | Env var | Default |
|---|---|---|---|
| URL | `drizz.url` | `DRIZZ_URL` | `http://localhost:9401` |
| Profile | `drizz.profile` | `DRIZZ_PROFILE` | — |
| Device ID | `drizz.device_id` | `DRIZZ_DEVICE_ID` | — |
| Record video | `drizz.record_video` | `DRIZZ_RECORD_VIDEO` | `true` |
| Capture logcat | `drizz.capture_logcat` | `DRIZZ_CAPTURE_LOGCAT` | `true` |
| Capture screenshots | `drizz.capture_screenshots` | `DRIZZ_CAPTURE_SCREENSHOTS` | `true` |
| Capture network | `drizz.capture_network` | `DRIZZ_CAPTURE_NETWORK` | `false` |

Example:
```bash
DRIZZ_URL=http://farm.local:9401 \
  DRIZZ_RECORD_VIDEO=true \
  DRIZZ_CAPTURE_NETWORK=true \
  mvn test
```

## License

Apache 2.0 — same as drizz-farm itself.
