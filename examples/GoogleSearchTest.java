/**
 * Google search test for Java / JUnit 5 — Appium drop-in against drizz-farm.
 *
 * Drive Chrome mobile via UiAutomator2, search, assert results.
 * drizz:* capabilities opt in to video / logcat capture on the daemon side.
 *
 * Maven dependencies (appium-java-client pulls in Selenium + HTTP client):
 *   <dependency>
 *     <groupId>io.appium</groupId>
 *     <artifactId>java-client</artifactId>
 *     <version>9.3.0</version>
 *   </dependency>
 *   <dependency>
 *     <groupId>org.junit.jupiter</groupId>
 *     <artifactId>junit-jupiter</artifactId>
 *     <version>5.11.0</version>
 *     <scope>test</scope>
 *   </dependency>
 *
 * Run:
 *   DRIZZ_URL=http://parthas-macbook-pro.local:9401 mvn test
 */
import io.appium.java_client.android.AndroidDriver;
import io.appium.java_client.android.options.UiAutomator2Options;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.openqa.selenium.By;
import org.openqa.selenium.WebElement;
import org.openqa.selenium.support.ui.ExpectedConditions;
import org.openqa.selenium.support.ui.WebDriverWait;

import java.net.URI;
import java.time.Duration;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertFalse;

public class GoogleSearchTest {

    private static final String DRIZZ_URL =
        System.getenv().getOrDefault("DRIZZ_URL", "http://localhost:9401");
    private static final String QUERY =
        System.getenv().getOrDefault("QUERY", "drizz farm open source device lab");

    private AndroidDriver driver;

    @BeforeEach
    void setUp() throws Exception {
        UiAutomator2Options opts = new UiAutomator2Options()
            .setPlatformName("Android")
            .setAutomationName("UiAutomator2")
            .setBrowserName("Chrome")
            // drizz-farm extras — /wd/hub strips these out and starts
            // captures before forwarding cleaned caps to Appium.
            .amend("drizz:record_video",       true)
            .amend("drizz:capture_logcat",     true)
            .amend("drizz:capture_screenshots", true)
            .amend("drizz:timeout_minutes",    10);

        driver = new AndroidDriver(URI.create(DRIZZ_URL + "/wd/hub").toURL(), opts);
        driver.manage().timeouts().implicitlyWait(Duration.ofSeconds(5));
        System.out.println("[drizz] session " + driver.getSessionId());
    }

    @Test
    void searchForDrizzFarm() {
        driver.get("https://www.google.com/ncr");
        WebDriverWait wait = new WebDriverWait(driver, Duration.ofSeconds(15));

        // Consent screen (EU / fresh emulators) — tap accept if visible.
        try {
            WebElement accept = wait.until(ExpectedConditions.elementToBeClickable(
                By.xpath("//button[.//span[contains(., 'Accept') or contains(., 'agree')]]")));
            accept.click();
            System.out.println("[drizz] consent accepted");
        } catch (Exception ignored) {
        }

        WebElement box = wait.until(ExpectedConditions.elementToBeClickable(By.name("q")));
        box.clear();
        box.sendKeys(QUERY);
        box.submit();

        wait.until(ExpectedConditions.presenceOfElementLocated(By.id("search")));

        List<WebElement> results = driver.findElements(By.cssSelector("#search a h3"));
        System.out.println("[drizz] got " + results.size() + " results");
        for (int i = 0; i < Math.min(5, results.size()); i++) {
            System.out.println("  " + (i + 1) + ". " + results.get(i).getText());
        }
        assertFalse(results.isEmpty(), "expected at least one search result");
    }

    @AfterEach
    void tearDown() {
        if (driver != null) {
            String sid = driver.getSessionId().toString();
            driver.quit();
            System.out.println("[drizz] artifacts: " + DRIZZ_URL + "/api/v1/sessions/" + sid + "/artifacts");
            System.out.println("[drizz] playback : " + DRIZZ_URL + "/playback/" + sid);
        }
    }
}
