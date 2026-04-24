"""
Google search test against drizz-farm's Appium-compat /wd/hub endpoint.

Point DRIZZ_URL at your farm (default http://localhost:9401). This does
NOT install an app — it drives Chrome via Appium's UiAutomator2, opens
google.com, types a query, taps search, asserts results are visible.
Because it uses no installed APK, it works on any Play-image emulator.

drizz:* capabilities opt in to video + logcat + HAR capture. After the
test, fetch artifacts from /api/v1/sessions/<session_id>/artifacts.

Run:
    pip install appium-python-client
    DRIZZ_URL=http://parthas-macbook-pro.local:9401 python examples/google_search_test.py
"""
import os
import time

from appium import webdriver
from appium.options.android import UiAutomator2Options
from appium.webdriver.common.appiumby import AppiumBy
from selenium.webdriver.common.by import By
from selenium.webdriver.support import expected_conditions as EC
from selenium.webdriver.support.ui import WebDriverWait


DRIZZ_URL = os.environ.get("DRIZZ_URL", "http://localhost:9401")
QUERY = os.environ.get("QUERY", "drizz farm open source device lab")


def main() -> None:
    opts = UiAutomator2Options()
    opts.platform_name = "Android"
    opts.automation_name = "UiAutomator2"
    # Drive the built-in Chrome instead of a custom APK so the test
    # works on any Play-store emulator image.
    opts.browser_name = "Chrome"
    # drizz-farm extras — the Appium server ignores unknown caps; our
    # /wd/hub compat layer peels them off and starts the captures
    # before forwarding the cleaned caps to Appium.
    opts.set_capability("drizz:record_video", True)
    opts.set_capability("drizz:capture_logcat", True)
    opts.set_capability("drizz:capture_screenshots", True)
    opts.set_capability("drizz:timeout_minutes", 10)

    hub = f"{DRIZZ_URL}/wd/hub"
    print(f"[drizz] connecting to {hub}")
    driver = webdriver.Remote(hub, options=opts)
    session_id = driver.session_id
    print(f"[drizz] session {session_id}")

    try:
        driver.get("https://www.google.com/ncr")  # ncr = no-country-redirect, stable UI

        wait = WebDriverWait(driver, 15)
        # Handle the consent screen if it shows (EU / new install).
        try:
            accept = wait.until(EC.element_to_be_clickable(
                (By.XPATH, "//button[.//span[contains(., 'Accept') or contains(., 'agree')]]")
            ))
            accept.click()
            print("[drizz] consent accepted")
        except Exception:
            pass

        search_box = wait.until(EC.element_to_be_clickable(
            (By.NAME, "q")
        ))
        search_box.clear()
        search_box.send_keys(QUERY)
        search_box.submit()
        print(f"[drizz] searched: {QUERY}")

        # Wait for the search results container.
        wait.until(EC.presence_of_element_located((By.ID, "search")))
        time.sleep(2)  # let results settle for the video

        results = driver.find_elements(By.CSS_SELECTOR, "#search a h3")
        print(f"[drizz] got {len(results)} results")
        for i, r in enumerate(results[:5], 1):
            print(f"  {i}. {r.text}")
        assert len(results) > 0, "expected at least one search result"
        print("[drizz] PASS")
    finally:
        driver.quit()

    # Drizz-farm finalizes video.mp4 / logcat.txt on session release.
    print(f"[drizz] artifacts: {DRIZZ_URL}/api/v1/sessions/{session_id}/artifacts")
    print(f"[drizz] playback : {DRIZZ_URL}/playback/{session_id}")


if __name__ == "__main__":
    main()
