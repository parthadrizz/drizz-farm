# drizz-farm (Python client)

Minimal HTTP client + **pytest plugin** for [drizz-farm](https://github.com/parthadrizz/drizz-farm) — the self-hosted Android device lab.

## Install

```bash
pip install drizz-farm          # client only
pip install drizz-farm[pytest]  # + pytest fixture
```

## Direct client

```python
from drizz_farm import Farm

with Farm("http://farm.local:9401") as farm:
    with farm.create_session(record_video=True, capture_logcat=True) as sess:
        # Install your APK from bytes (works from any CI, no shared FS needed)
        sess.install_apk(open("build/app.apk", "rb").read())

        # Drop a test image into the gallery so the app's picker sees it
        sess.inject_camera_image(open("fixtures/selfie.jpg", "rb").read())

        # Simulate real-world conditions
        sess.set_gps(37.7749, -122.4194)
        sess.set_network("3g")
        sess.set_battery(level=15)
        sess.set_locale("ja_JP")
        sess.set_timezone("Asia/Tokyo")

        # Run your test flow here (appium, espresso, uiautomator — any client)
        sess.shell("am start -n io.appium.android.apis/.ApiDemos")
        png = sess.screenshot()
        open("after.png", "wb").write(png)

    # At this point video.mp4, logcat.txt, screenshots all land in
    # the session's artifact dir. List + download:
    for a in sess.artifacts():
        print(a.type, a.filename, a.size, a.url)
```

## Pytest plugin

Add a section to `pytest.ini`:

```ini
[pytest]
drizz_url = http://farm.local:9401
drizz_profile = api34_play
drizz_record_video = true
drizz_capture_logcat = true
```

Then use the `drizz_session` fixture in any test:

```python
def test_login_flow(drizz_session):
    drizz_session.install_apk(open("build/app.apk", "rb").read())
    drizz_session.set_gps(37.7749, -122.4194)

    # Drive the app via Appium using drizz_session.appium_url,
    # or raw ADB with drizz_session.shell(...), or an Espresso runner
    # connected via drizz_session.serial — your choice.
    assert "MainActivity" in drizz_session.shell("dumpsys activity top")
```

Every test gets a fresh, isolated session. Artifacts (video, logcat, screenshots) auto-land and their URLs are printed at teardown. On test failure, an extra screenshot is captured in-flight.

## Configuration

All options are settable via `pytest.ini`, CLI (`--drizz-url=…`), or environment variable (`DRIZZ_URL=…`). Env wins, then CLI, then ini.

| Key | Default | Notes |
|---|---|---|
| `drizz_url` | `http://localhost:9401` | Farm daemon URL |
| `drizz_profile` | — | Pool profile to match |
| `drizz_device_id` | — | Pin to a specific device instance |
| `drizz_record_video` | `true` | Session video via screenrecord (chunked + stitched) |
| `drizz_capture_logcat` | `true` | Full device log → `logcat.txt` |
| `drizz_capture_screenshots` | `true` | Enables on-demand `screenshot()` API |
| `drizz_capture_network` | `false` | HAR via mitmproxy — requires `mitmdump` on farm host |

## License

Apache 2.0 — same as drizz-farm itself.
