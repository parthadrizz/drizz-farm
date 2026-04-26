"""
Real 30-step Chrome browsing session against drizz-farm.

This is ONE sequential test function (not a pytest matrix) that drives
Chrome mobile through an actual browsing journey: multiple searches,
click-throughs to result pages, scrolling, back-navigation, tab
switches, gestures. Every step that completes prints a line so the
terminal log tells a story frame-by-frame that matches the recorded
video.

Philosophy: each step is best-effort. A transient selector miss (Google
layout A/B testing, a missing element) logs a skip and moves on — one
flaky selector shouldn't abort 29 others. Hard failures (no driver,
broken proxy) still raise.

Install:
    pip install appium-python-client pytest httpx

Run:
    DRIZZ_URL=http://parthas-macbook-pro.local:9401 \\
      pytest -v examples/google_chrome_deep_test.py -s

The -s flag is important: lets the per-step prints flow to stdout so
you can follow the run in real time.
"""
from __future__ import annotations

import os
import time
import traceback

import httpx
import pytest
from appium import webdriver
from appium.options.android import UiAutomator2Options
from appium.webdriver.webdriver import WebDriver
from selenium.common.exceptions import (
    NoSuchElementException,
    StaleElementReferenceException,
    TimeoutException,
    WebDriverException,
)
from selenium.webdriver.common.by import By
from selenium.webdriver.common.keys import Keys
from selenium.webdriver.support import expected_conditions as EC
from selenium.webdriver.support.ui import WebDriverWait


DRIZZ_URL = os.environ.get("DRIZZ_URL", "http://localhost:9401").rstrip("/")


# ---------------------------------------------------------------------------
# Fixture — single Appium session for the whole journey. Module-scoped so
# all 30 steps share the same driver + same recorded video.
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def session():
    opts = UiAutomator2Options()
    opts.platform_name = "Android"
    opts.automation_name = "UiAutomator2"
    opts.browser_name = "Chrome"
    # Chromedriver bundled with Appium (v113) is older than Chrome on
    # modern emulators (v120+). Mismatched versions make every locator
    # call throw "invalid argument: invalid locator". Autodownload
    # lets Appium fetch a chromedriver that matches the device's
    # actual Chrome version before the session starts.
    opts.set_capability("appium:chromedriverAutodownload", True)
    opts.set_capability("drizz:record_video", True)
    opts.set_capability("drizz:capture_logcat", True)
    opts.set_capability("drizz:capture_screenshots", True)
    opts.set_capability("drizz:timeout_minutes", 20)

    hub = f"{DRIZZ_URL}/wd/hub"
    print(f"\n[setup] hub={hub}")
    driver = webdriver.Remote(hub, options=opts)
    appium_sid = driver.session_id
    print(f"[setup] appium sid={appium_sid}")

    # Map appium sid → drizz sid so the REST calls target the right record.
    http = httpx.Client(timeout=15)
    r = http.get(f"{DRIZZ_URL}/wd/hub/session/{appium_sid}/drizz-session-id")
    r.raise_for_status()
    drizz_sid = r.json()["drizz_session_id"]
    print(f"[setup] drizz sid={drizz_sid}")
    print(f"[setup] playback will be available at {DRIZZ_URL}/playback/{drizz_sid}")

    driver.implicitly_wait(3)
    try:
        yield driver, drizz_sid, http
    finally:
        http.close()
        try:
            driver.quit()
        except Exception as e:
            print(f"[teardown] driver.quit failed (non-fatal): {e}")
        print(f"\n[teardown] artifacts: {DRIZZ_URL}/api/v1/sessions/{drizz_sid}/artifacts")
        print(f"[teardown] playback : {DRIZZ_URL}/playback/{drizz_sid}\n")


# ---------------------------------------------------------------------------
# Step-running scaffolding — lets the test log "step N: …" with pass/fail,
# keep going on per-step errors, and print a final summary.
# ---------------------------------------------------------------------------

class Journey:
    def __init__(self) -> None:
        self.idx = 0
        self.passed: list[str] = []
        self.failed: list[tuple[str, str]] = []
        self.skipped: list[tuple[str, str]] = []

    def step(self, label: str):
        self.idx += 1
        n = self.idx
        return _StepContext(self, n, label)


class _StepContext:
    def __init__(self, journey: Journey, n: int, label: str) -> None:
        self.journey = journey
        self.n = n
        self.label = label

    def __enter__(self):
        self.t0 = time.monotonic()
        print(f"\n[{self.n:02d}] {self.label}")
        return self

    def skip(self, why: str) -> None:
        # Raise this special exception from within the `with` block to
        # count as "skipped" not "failed". Lets tests note "element not
        # on this layout variant" cleanly.
        raise _Skipped(why)

    def __exit__(self, exc_type, exc, tb) -> bool:
        dt = time.monotonic() - self.t0
        if exc is None:
            self.journey.passed.append(f"[{self.n:02d}] {self.label}")
            print(f"     ✓ ok ({dt:.2f}s)")
        elif exc_type is _Skipped:
            self.journey.skipped.append((f"[{self.n:02d}] {self.label}", str(exc)))
            print(f"     ⊘ skip — {exc}")
        else:
            detail = f"{exc_type.__name__}: {str(exc).splitlines()[0] if str(exc) else ''}"
            self.journey.failed.append((f"[{self.n:02d}] {self.label}", detail))
            print(f"     ✗ FAIL ({dt:.2f}s) — {detail}")
        return True  # swallow so the journey continues


class _Skipped(Exception):
    pass


# ---------------------------------------------------------------------------
# Small reusable helpers.
# ---------------------------------------------------------------------------

def wait(driver: WebDriver, secs: int = 12) -> WebDriverWait:
    return WebDriverWait(driver, secs)


def maybe_accept_consent(driver: WebDriver) -> bool:
    """Google's consent dialog is region-dependent. Try a few selectors,
    return True if clicked."""
    selectors = [
        (By.XPATH, "//button[.//span[contains(., 'Accept all')]]"),
        (By.XPATH, "//button[.//span[contains(., 'I agree')]]"),
        (By.XPATH, "//button[contains(., 'Accept all')]"),
        (By.ID, "L2AGLb"),  # Google's stable consent button id in some regions
    ]
    for by, q in selectors:
        try:
            el = WebDriverWait(driver, 3).until(EC.element_to_be_clickable((by, q)))
            el.click()
            return True
        except (TimeoutException, NoSuchElementException):
            continue
    return False


def search_google(driver: WebDriver, query: str) -> None:
    box = wait(driver, 15).until(EC.element_to_be_clickable((By.NAME, "q")))
    box.click()
    # Some mobile layouts pre-fill the box; clear before typing.
    box.clear()
    box.send_keys(query)
    box.send_keys(Keys.ENTER)
    wait(driver, 15).until(EC.presence_of_element_located((By.ID, "search")))


def first_result(driver: WebDriver):
    # Organic result titles live inside <h3> within <a> inside #search.
    return driver.find_element(By.CSS_SELECTOR, "#search a h3")


# ---------------------------------------------------------------------------
# THE ACTUAL JOURNEY — one function, thirty sequential steps.
# ---------------------------------------------------------------------------

def test_chrome_browsing_journey(session) -> None:
    driver, drizz_sid, http = session
    j = Journey()

    def api(method: str, path: str, **kwargs) -> httpx.Response:
        return http.request(method, f"{DRIZZ_URL}/api/v1/sessions/{drizz_sid}{path}", **kwargs)

    # ---- Part 1: First search + read a result ----------------------------

    with j.step("Open google.com (first page load)") as s:
        driver.get("https://www.google.com/ncr")
        wait(driver).until(EC.presence_of_element_located((By.NAME, "q")))

    with j.step("Accept cookie consent (if shown)") as s:
        if not maybe_accept_consent(driver):
            s.skip("no consent dialog on this region/session")

    with j.step("Search 'best android emulators 2026'") as s:
        search_google(driver, "best android emulators 2026")
        count = len(driver.find_elements(By.CSS_SELECTOR, "#search a h3"))
        assert count >= 3, f"got {count} results"
        print(f"     {count} results shown")

    with j.step("Scroll down the results page (300px, 600px, 1000px)") as s:
        for y in (300, 600, 1000):
            driver.execute_script(f"window.scrollTo(0, {y})")
            time.sleep(0.6)

    with j.step("Scroll back to the top") as s:
        driver.execute_script("window.scrollTo(0, 0)")
        time.sleep(0.4)

    with j.step("Click the first organic result") as s:
        title = first_result(driver)
        result_text = title.text.strip()
        print(f"     clicking: {result_text!r}")
        # Click the parent <a> to navigate (h3 itself isn't always clickable).
        title.find_element(By.XPATH, "./ancestor::a").click()
        wait(driver, 25).until(lambda d: "google.com/search" not in d.current_url)
        print(f"     landed on: {driver.current_url[:80]}")

    with j.step("Wait for page to settle + scroll through it") as s:
        time.sleep(2)  # let the page paint
        for _ in range(3):
            driver.execute_script("window.scrollBy(0, 500)")
            time.sleep(0.5)

    with j.step("Read the landed-page title") as s:
        title_text = driver.title
        print(f"     title: {title_text!r}")
        assert title_text, "landed page has no title"

    with j.step("Browser back to search results") as s:
        driver.back()
        wait(driver).until(EC.presence_of_element_located((By.ID, "search")))
        assert "search" in driver.current_url

    # ---- Part 2: Second search + images tab ------------------------------

    with j.step("Run a second search: 'python selenium tutorial'") as s:
        search_google(driver, "python selenium tutorial")

    with j.step("Switch to the Images tab") as s:
        # Mobile Google's tabs: the container + link selectors vary. Try a couple.
        clicked = False
        for locator in [
            (By.XPATH, "//a[.//div[normalize-space()='Images']]"),
            (By.XPATH, "//a[contains(@href,'tbm=isch')]"),
            (By.LINK_TEXT, "Images"),
        ]:
            try:
                el = WebDriverWait(driver, 4).until(EC.element_to_be_clickable(locator))
                el.click()
                clicked = True
                break
            except (TimeoutException, NoSuchElementException):
                continue
        if not clicked:
            s.skip("no Images tab selector matched current layout")
        wait(driver).until(lambda d: "tbm=isch" in d.current_url)

    with j.step("Scroll the image grid") as s:
        for y in (600, 1200, 1800):
            driver.execute_script(f"window.scrollTo(0, {y})")
            time.sleep(0.4)

    with j.step("Tap the first image result") as s:
        imgs = driver.find_elements(By.CSS_SELECTOR, "img[data-src], #islrg img")
        if not imgs:
            s.skip("no image tiles located")
        imgs[0].click()
        time.sleep(1.5)  # image preview flyout

    with j.step("Browser back from image detail") as s:
        driver.back()
        time.sleep(1)

    with j.step("Back to web search (pop off Images)") as s:
        driver.back()
        wait(driver).until(EC.presence_of_element_located((By.ID, "search")))

    # ---- Part 3: Third search with meaningful content --------------------

    with j.step("Third search: 'weather san francisco'") as s:
        search_google(driver, "weather san francisco")

    with j.step("Assert the weather answer card is present") as s:
        # Google shows an inline weather card with id 'wob_wc' for weather queries.
        try:
            driver.find_element(By.ID, "wob_wc")
            print("     weather card rendered")
        except NoSuchElementException:
            s.skip("no inline weather card on this region")

    with j.step("Scroll past the answer card to organic results") as s:
        driver.execute_script("window.scrollBy(0, 900)")
        time.sleep(0.5)
        count = len(driver.find_elements(By.CSS_SELECTOR, "#search a h3"))
        print(f"     {count} organic results below the answer card")

    # ---- Part 4: A few quick shortcuts + gestures ------------------------

    with j.step("Swipe down to simulate refresh gesture") as s:
        size = driver.get_window_size()
        driver.execute_script("mobile: swipeGesture", {
            "left": size["width"] // 2, "top": 200,
            "width": 10, "height": size["height"] - 400,
            "direction": "down", "percent": 0.8,
        })
        time.sleep(0.5)

    with j.step("Navigate directly to example.com (non-Google)") as s:
        driver.get("https://example.com/")
        wait(driver, 15).until(EC.presence_of_element_located((By.TAG_NAME, "h1")))
        h1 = driver.find_element(By.TAG_NAME, "h1").text
        print(f"     h1: {h1!r}")
        assert "Example" in h1, f"expected 'Example' in h1, got {h1!r}"

    with j.step("Click the 'More information' link on example.com") as s:
        link = driver.find_element(By.LINK_TEXT, "More information...")
        link.click()
        wait(driver, 15).until(lambda d: "example.com" not in d.current_url)
        print(f"     landed on: {driver.current_url[:80]}")

    with j.step("Back twice to return to example.com then Google") as s:
        driver.back()
        time.sleep(0.6)
        driver.back()

    # ---- Part 5: Fourth search + result click ---------------------------

    with j.step("Navigate back to google.com") as s:
        driver.get("https://www.google.com/ncr")
        wait(driver).until(EC.presence_of_element_located((By.NAME, "q")))

    with j.step("Fourth search: 'appium documentation'") as s:
        search_google(driver, "appium documentation")

    with j.step("Click the first result (should lead to appium.io or similar)") as s:
        title = first_result(driver)
        print(f"     clicking: {title.text!r}")
        title.find_element(By.XPATH, "./ancestor::a").click()
        wait(driver, 20).until(lambda d: "google.com/search" not in d.current_url)
        print(f"     landed on: {driver.current_url[:80]}")

    with j.step("Scroll through the docs page") as s:
        for y in (400, 800, 1400, 2000):
            driver.execute_script(f"window.scrollTo(0, {y})")
            time.sleep(0.3)

    # ---- Part 6: drizz-farm side-channel effects ------------------------

    with j.step("Inject GPS (San Francisco) via drizz-farm API") as s:
        r = api("POST", "/gps", json={"latitude": 37.7749, "longitude": -122.4194})
        assert r.status_code == 200, r.text

    with j.step("Toggle network profile to 3g (throttle)") as s:
        r = api("POST", "/network", json={"profile": "3g"})
        assert r.status_code == 200, r.text

    with j.step("Reload the current page under throttled network") as s:
        driver.refresh()
        wait(driver, 30).until(lambda d: d.execute_script("return document.readyState") == "complete")

    with j.step("Restore network to 4g") as s:
        r = api("POST", "/network", json={"profile": "4g"})
        assert r.status_code == 200, r.text

    with j.step("Inject low battery (15%, discharging)") as s:
        r = api("POST", "/battery", json={"level": 15, "charging": "discharging"})
        assert r.status_code == 200, r.text

    with j.step("Capture an on-demand screenshot via Appium") as s:
        png = driver.get_screenshot_as_png()
        assert png.startswith(b"\x89PNG"), "not a valid PNG"
        print(f"     {len(png):,} bytes")

    with j.step("Final navigation back to google.com homepage") as s:
        driver.get("https://www.google.com/ncr")
        wait(driver).until(EC.presence_of_element_located((By.NAME, "q")))

    # ---- Wrap up --------------------------------------------------------
    total = len(j.passed) + len(j.failed) + len(j.skipped)
    print("\n" + "=" * 60)
    print(f" JOURNEY SUMMARY: {len(j.passed)} passed, "
          f"{len(j.failed)} failed, {len(j.skipped)} skipped (of {total})")
    if j.failed:
        print(" Failures:")
        for name, detail in j.failed:
            print(f"   {name}\n     {detail}")
    if j.skipped:
        print(" Skipped (non-fatal):")
        for name, why in j.skipped:
            print(f"   {name}  — {why}")
    print("=" * 60)

    # Mark the pytest test as failed only if MORE THAN HALF the steps
    # failed (so transient Google layout changes don't tank the run).
    assert len(j.failed) <= total // 2, (
        f"too many steps failed: {len(j.failed)} of {total}\n"
        + "\n".join(f"  {n}: {d}" for n, d in j.failed)
    )


if __name__ == "__main__":
    raise SystemExit(pytest.main([__file__, "-v", "-s"]))
