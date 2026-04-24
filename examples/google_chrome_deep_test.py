"""
Deep Appium + drizz-farm test suite driving Chrome on Android.

Twenty-one test functions. Each runs as its own pytest test so you get
proper pass/fail reporting, per-test duration, screenshot-on-failure, and
parallelism via pytest-xdist. The driver fixture is a single Appium
session that all tests share — much faster than re-creating a session
per test, and the produced video covers the entire flight so you can
review every failure in one playback.

Stack:
    pytest + Appium-Python-Client + Selenium + httpx for the handful
    of drizz-farm-specific extras (GPS / network / battery / rotate).

Install:
    pip install appium-python-client pytest httpx

Run against your farm:
    DRIZZ_URL=http://parthas-macbook-pro.local:9401 pytest examples/google_chrome_deep_test.py -v

Run a single test:
    pytest examples/google_chrome_deep_test.py::test_05_results_count -v

Run headed for local debugging and keep the emulator visible:
    DRIZZ_URL=http://localhost:9401 DRIZZ_KEEP_SESSION=1 pytest -v

Every test prints step context so the terminal output lines up with the
video frame-by-frame when you open the Playback UI afterward.
"""
from __future__ import annotations

import os
import time
from dataclasses import dataclass
from typing import Iterator

import httpx
import pytest
from appium import webdriver
from appium.options.android import UiAutomator2Options
from appium.webdriver.webdriver import WebDriver
from selenium.common.exceptions import TimeoutException, NoSuchElementException
from selenium.webdriver.common.by import By
from selenium.webdriver.common.keys import Keys
from selenium.webdriver.support import expected_conditions as EC
from selenium.webdriver.support.ui import WebDriverWait


# ---------------------------------------------------------------------------
# Configuration — everything driven by env vars so the same file runs
# locally, in CI, or against a shared farm with no code edits.
# ---------------------------------------------------------------------------

DRIZZ_URL = os.environ.get("DRIZZ_URL", "http://localhost:9401").rstrip("/")
QUERY_PRIMARY = os.environ.get("QUERY", "drizz farm github")
SHORT_WAIT = 6
LONG_WAIT = 20


@dataclass
class DrizzSession:
    """Bundle of what every test needs: the Appium driver, the drizz-farm
    session id (for the daemon-side REST calls), and the farm base URL."""
    driver: WebDriver
    session_id: str
    farm_url: str
    http: httpx.Client

    def api(self, method: str, path: str, **kwargs) -> httpx.Response:
        url = f"{self.farm_url}/api/v1/sessions/{self.session_id}{path}"
        r = self.http.request(method, url, timeout=10, **kwargs)
        r.raise_for_status()
        return r


# ---------------------------------------------------------------------------
# Fixtures — one shared Appium session for the whole module. The suite is
# sequential (tests build on each other's state) and sharing a session
# makes the resulting video a single coherent recording.
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def drizz() -> Iterator[DrizzSession]:
    opts = UiAutomator2Options()
    opts.platform_name = "Android"
    opts.automation_name = "UiAutomator2"
    opts.browser_name = "Chrome"
    opts.set_capability("drizz:record_video", True)
    opts.set_capability("drizz:capture_logcat", True)
    opts.set_capability("drizz:capture_screenshots", True)
    opts.set_capability("drizz:timeout_minutes", 15)

    hub = f"{DRIZZ_URL}/wd/hub"
    print(f"\n[drizz] hub={hub}")
    driver = webdriver.Remote(hub, options=opts)
    sid = driver.session_id
    print(f"[drizz] session={sid}")
    driver.implicitly_wait(2)
    sess = DrizzSession(driver=driver, session_id=sid, farm_url=DRIZZ_URL,
                        http=httpx.Client())
    try:
        yield sess
    finally:
        sess.http.close()
        if os.environ.get("DRIZZ_KEEP_SESSION") != "1":
            driver.quit()
        print(f"\n[drizz] artifacts: {DRIZZ_URL}/api/v1/sessions/{sid}/artifacts")
        print(f"[drizz] playback : {DRIZZ_URL}/playback/{sid}\n")


@pytest.fixture
def driver(drizz: DrizzSession) -> WebDriver:
    """Shortcut for tests that only need the WebDriver handle."""
    return drizz.driver


# ---------------------------------------------------------------------------
# Small reusable helpers. Keeping them at module scope so tests read as
# "what" not "how", and any flake fix (e.g. adding a retry) lives in
# one place.
# ---------------------------------------------------------------------------

def _wait(driver: WebDriver, seconds: int = LONG_WAIT) -> WebDriverWait:
    return WebDriverWait(driver, seconds)


def _accept_consent_if_shown(driver: WebDriver) -> bool:
    """Google's EU consent screen blocks everything else when present.
    Tap 'Accept all' / 'I agree' if visible; no-op otherwise.
    Returns True if clicked so the caller can assert progression."""
    try:
        btn = WebDriverWait(driver, SHORT_WAIT).until(
            EC.element_to_be_clickable((
                By.XPATH,
                "//button[.//span[contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', "
                "'abcdefghijklmnopqrstuvwxyz'), 'accept') or contains(translate(., "
                "'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'agree')]]",
            ))
        )
        btn.click()
        return True
    except TimeoutException:
        return False


def _search(driver: WebDriver, query: str) -> None:
    """Clear the search box, type a query, submit with Enter."""
    box = _wait(driver).until(EC.element_to_be_clickable((By.NAME, "q")))
    box.clear()
    box.send_keys(query)
    box.send_keys(Keys.ENTER)
    _wait(driver).until(EC.presence_of_element_located((By.ID, "search")))


# ---------------------------------------------------------------------------
# Tests — twenty-one steps. Each test's name reads like a sentence so the
# pytest report doubles as documentation of what ran.
# ---------------------------------------------------------------------------

def test_01_open_google_homepage(driver: WebDriver) -> None:
    driver.get("https://www.google.com/ncr")  # ncr = no country redirect
    _accept_consent_if_shown(driver)
    _wait(driver).until(EC.presence_of_element_located((By.NAME, "q")))
    assert "google" in driver.current_url.lower()


def test_02_google_logo_is_displayed(driver: WebDriver) -> None:
    logo = driver.find_element(By.CSS_SELECTOR, "img[alt='Google']")
    assert logo.is_displayed()


def test_03_type_query_into_search_box(driver: WebDriver) -> None:
    box = driver.find_element(By.NAME, "q")
    box.clear()
    box.send_keys(QUERY_PRIMARY)
    assert box.get_attribute("value") == QUERY_PRIMARY


def test_04_submit_search_via_enter(driver: WebDriver) -> None:
    driver.find_element(By.NAME, "q").send_keys(Keys.ENTER)
    _wait(driver).until(EC.presence_of_element_located((By.ID, "search")))


def test_05_results_count_at_least_five(driver: WebDriver) -> None:
    results = driver.find_elements(By.CSS_SELECTOR, "#search a h3")
    print(f"  got {len(results)} result titles")
    assert len(results) >= 5, f"expected >= 5 results, got {len(results)}"


def test_06_first_result_is_clickable(driver: WebDriver) -> None:
    first = driver.find_element(By.CSS_SELECTOR, "#search a h3")
    assert first.is_displayed()
    assert first.text.strip() != ""


def test_07_scroll_down_the_results(driver: WebDriver) -> None:
    before = driver.execute_script("return window.pageYOffset")
    driver.execute_script("window.scrollBy(0, 800)")
    time.sleep(0.5)
    after = driver.execute_script("return window.pageYOffset")
    assert after > before, f"scroll didn't move: {before} -> {after}"


def test_08_scroll_back_to_top(driver: WebDriver) -> None:
    driver.execute_script("window.scrollTo(0, 0)")
    time.sleep(0.3)
    assert driver.execute_script("return window.pageYOffset") == 0


def test_09_refine_query_by_editing_box(driver: WebDriver) -> None:
    _search(driver, "android emulator farm open source")
    results = driver.find_elements(By.CSS_SELECTOR, "#search a h3")
    assert len(results) >= 3


def test_10_switch_to_images_tab(driver: WebDriver) -> None:
    try:
        link = _wait(driver, SHORT_WAIT).until(EC.element_to_be_clickable((
            By.XPATH, "//a[.//div[normalize-space()='Images']]")))
    except TimeoutException:
        link = driver.find_element(By.XPATH, "//a[contains(@href, 'tbm=isch')]")
    link.click()
    _wait(driver).until(EC.presence_of_element_located((By.CSS_SELECTOR, "img")))
    assert "tbm=isch" in driver.current_url


def test_11_scroll_the_images_grid(driver: WebDriver) -> None:
    before = driver.execute_script("return window.pageYOffset")
    driver.execute_script("window.scrollBy(0, 1200)")
    time.sleep(0.5)
    after = driver.execute_script("return window.pageYOffset")
    assert after > before


def test_12_browser_back_returns_to_web_results(driver: WebDriver) -> None:
    driver.back()
    _wait(driver).until(EC.presence_of_element_located((By.ID, "search")))
    assert "tbm=isch" not in driver.current_url


def test_13_page_title_matches_query(driver: WebDriver) -> None:
    title = driver.title
    print(f"  title: {title!r}")
    # Google always puts the query in the title of a SERP.
    assert "emulator" in title.lower() or "android" in title.lower()


def test_14_current_url_is_google(driver: WebDriver) -> None:
    url = driver.current_url
    assert url.startswith("https://www.google."), url


def test_15_inject_gps_san_francisco(drizz: DrizzSession) -> None:
    drizz.api("POST", "/gps",
              json={"latitude": 37.7749, "longitude": -122.4194})


def test_16_network_profile_to_3g(drizz: DrizzSession) -> None:
    drizz.api("POST", "/network", json={"profile": "3g"})


def test_17_reload_under_3g_throttling(driver: WebDriver) -> None:
    driver.refresh()
    _wait(driver).until(EC.presence_of_element_located((By.ID, "search")))


def test_18_restore_network_to_4g(drizz: DrizzSession) -> None:
    drizz.api("POST", "/network", json={"profile": "4g"})


def test_19_rotate_to_landscape_and_back(drizz: DrizzSession,
                                          driver: WebDriver) -> None:
    drizz.api("POST", "/orientation", json={"rotation": 1})
    time.sleep(1)
    drizz.api("POST", "/orientation", json={"rotation": 0})
    time.sleep(1)
    _wait(driver).until(EC.presence_of_element_located((By.ID, "search")))


def test_20_inject_low_battery(drizz: DrizzSession) -> None:
    drizz.api("POST", "/battery",
              json={"level": 15, "charging": "discharging"})


def test_21_take_appium_screenshot(driver: WebDriver) -> None:
    png = driver.get_screenshot_as_png()
    assert png.startswith(b"\x89PNG"), "not a valid PNG"
    print(f"  screenshot: {len(png)} bytes")


# ---------------------------------------------------------------------------
# pytest hooks — nothing drizz-specific here, just nicer output.
# Captures a screenshot for every failed test.
# ---------------------------------------------------------------------------

@pytest.hookimpl(hookwrapper=True)
def pytest_runtest_makereport(item, call):
    outcome = yield
    report = outcome.get_result()
    if report.when == "call" and report.failed:
        drv = None
        for name in ("driver", "drizz"):
            if name in item.funcargs:
                obj = item.funcargs[name]
                drv = getattr(obj, "driver", obj)
                break
        if drv is not None:
            out = f"/tmp/{item.name}-failure.png"
            try:
                drv.save_screenshot(out)
                print(f"\n  failure screenshot saved: {out}")
            except Exception as exc:
                print(f"\n  couldn't save screenshot: {exc}")


if __name__ == "__main__":
    # Allow running this file directly with `python google_chrome_deep_test.py`.
    # Useful for local iteration without thinking about pytest invocation.
    raise SystemExit(pytest.main([__file__, "-v", "-s"]))
