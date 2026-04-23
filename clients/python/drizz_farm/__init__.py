"""
drizz-farm — Python client + pytest plugin.

Minimal, synchronous client over the HTTP API. Two entry points:

    from drizz_farm import Farm
    farm = Farm("http://farm.local:9401")
    sess = farm.create_session(record_video=True)
    try:
        sess.shell("pm list packages -3")          # raw adb shell
        sess.install_apk(open("build/app.apk", "rb").read())
        sess.set_gps(37.7749, -122.4194)
        sess.upload_file(open("fixture.jpg", "rb").read(),
                         target="/sdcard/DCIM/Camera/fixture.jpg")
    finally:
        sess.release()                              # video.mp4 lands here
        for art in sess.artifacts():
            print(art["type"], art["url"])

For pytest users, the plugin auto-creates a session per test and
releases at teardown. Artifacts auto-attach to the test report.

    def test_login(drizz_session):
        drizz_session.set_gps(37.7749, -122.4194)
        drizz_session.install_apk(APK_BYTES)
        # ... run your appium/espresso/ui-automator flow

The `drizz_session` fixture is injected by the plugin; configure it
via pytest.ini or env:

    [pytest]
    drizz_url = http://farm.local:9401
    drizz_record_video = true
    drizz_capture_logcat = true
    drizz_profile = api34_play
"""

from .client import Farm, Session, DrizzError

__version__ = "0.1.0"
__all__ = ["Farm", "Session", "DrizzError", "__version__"]
