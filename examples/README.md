# Examples

Ready-to-run Appium tests that exercise drizz-farm as a drop-in /wd/hub.

## 1. Smoke — `google_search_test.py` / `GoogleSearchTest.java`

Bare minimum: open Chrome, search Google, assert results. Good for a first "does everything work end-to-end" signal after a fresh install.

## 2. Deep — `google_chrome_deep_test.py` (pytest, 21 steps)

A full pytest-structured suite: 21 individual tests against a single shared Appium session, covering:

- Navigation, logo assertion, search box typing, submit
- Result counting + first-result check
- Scroll up / down
- Query refinement + tab switching (Images)
- Browser back, URL + title assertions
- drizz-farm API: **GPS injection**, **network profile throttle (3g → 4g)**, **orientation flip**, **low-battery injection**
- Appium `get_screenshot_as_png`

Each test is an independent pytest function, so:
- You get proper per-step pass/fail reporting
- Failure screenshots auto-save to `/tmp/<test_name>-failure.png`
- You can rerun just one step: `pytest ::test_15_inject_gps_san_francisco`

```bash
pip install appium-python-client pytest httpx
DRIZZ_URL=http://parthas-macbook-pro.local:9401 \
  pytest examples/google_chrome_deep_test.py -v
```

## Python

```bash
pip install appium-python-client
DRIZZ_URL=http://parthas-macbook-pro.local:9401 \
  python examples/google_search_test.py
```

## Java / JUnit 5

```xml
<!-- pom.xml deps shown at top of GoogleSearchTest.java -->
```

```bash
DRIZZ_URL=http://parthas-macbook-pro.local:9401 mvn test
```

## What you get

After the test finishes, the daemon has captured:

- `video.mp4` — full session recording (declared via `drizz:record_video`)
- `logcat.txt` — all logcat buffers
- `capture.log` — per-chunk capture lifecycle for debugging

All three appear at `DRIZZ_URL/api/v1/sessions/<session_id>/artifacts` and
in the Playback UI at `DRIZZ_URL/playback/<session_id>`.
