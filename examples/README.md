# Examples

Ready-to-run Appium tests that exercise drizz-farm as a drop-in /wd/hub.

Both do the same thing: open Chrome on Android, search Google, assert results are visible. Useful as a first "does everything work end-to-end" smoke.

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
