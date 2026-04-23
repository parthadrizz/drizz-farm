"""
Pytest plugin — `pip install drizz-farm[pytest]` and get a
`drizz_session` fixture automatically.

Fixture creates a fresh session per test (by default) and releases it
at teardown. On failure, screenshots + video + logcat are attached to
the test report (visible in pytest-html / Allure) and the artifact
URLs are printed to the terminal.

Config options (pytest.ini or env):
    drizz_url              URL of the daemon (default: http://localhost:9401)
    drizz_profile          pool profile to match
    drizz_device_id        specific device (overrides profile)
    drizz_record_video     bool, default true for failing tests only
    drizz_capture_logcat   bool, default true
    drizz_capture_screenshots  bool, default true
    drizz_capture_network  bool, default false
    drizz_scope            "function" | "module" | "session" (default function)

Env variables take precedence over ini (e.g. DRIZZ_URL=http://farm:9401).
"""
from __future__ import annotations

import os
from typing import Any, Iterator

import pytest

from .client import Farm, Session


def pytest_addoption(parser: pytest.Parser) -> None:
    group = parser.getgroup("drizz-farm")
    group.addoption("--drizz-url", dest="drizz_url",
                    default=None, help="drizz-farm daemon URL")
    group.addoption("--drizz-profile", dest="drizz_profile",
                    default=None, help="pool profile to allocate from")
    group.addoption("--drizz-device-id", dest="drizz_device_id",
                    default=None, help="specific device instance to allocate")

    parser.addini("drizz_url", "drizz-farm daemon URL", default="http://localhost:9401")
    parser.addini("drizz_profile", "pool profile", default="")
    parser.addini("drizz_device_id", "specific device id", default="")
    parser.addini("drizz_record_video", "record video (bool)", default="true")
    parser.addini("drizz_capture_logcat", "capture logcat (bool)", default="true")
    parser.addini("drizz_capture_screenshots", "capture screenshots (bool)", default="true")
    parser.addini("drizz_capture_network", "capture network HAR (bool)", default="false")
    parser.addini("drizz_scope", "fixture scope: function|module|session", default="function")


def _ini_bool(config: pytest.Config, name: str) -> bool:
    return str(config.getini(name)).strip().lower() in ("1", "true", "yes", "on")


def _env_or_cfg(config: pytest.Config, env_key: str, opt: str, ini: str) -> str:
    if v := os.getenv(env_key):
        return v
    if v := config.getoption(opt, default=None):
        return v
    return str(config.getini(ini) or "")


@pytest.fixture(scope="session")
def drizz_farm(pytestconfig: pytest.Config) -> Iterator[Farm]:
    """The Farm connection — reused across all tests in the run."""
    url = _env_or_cfg(pytestconfig, "DRIZZ_URL", "drizz_url", "drizz_url") or "http://localhost:9401"
    with Farm(url) as f:
        # Fail fast with a helpful error when the daemon's unreachable.
        try:
            f.health()
        except Exception as e:
            pytest.skip(f"drizz-farm not reachable at {url}: {e}")
        yield f


@pytest.fixture
def drizz_session(
    drizz_farm: Farm,
    pytestconfig: pytest.Config,
    request: pytest.FixtureRequest,
) -> Iterator[Session]:
    """A fresh session per test. Auto-releases at teardown."""
    profile = _env_or_cfg(pytestconfig, "DRIZZ_PROFILE", "drizz_profile", "drizz_profile") or None
    device_id = _env_or_cfg(pytestconfig, "DRIZZ_DEVICE_ID", "drizz_device_id", "drizz_device_id") or None

    sess = drizz_farm.create_session(
        profile=profile,
        device_id=device_id,
        record_video=_ini_bool(pytestconfig, "drizz_record_video"),
        capture_logcat=_ini_bool(pytestconfig, "drizz_capture_logcat"),
        capture_screenshots=_ini_bool(pytestconfig, "drizz_capture_screenshots"),
        capture_network=_ini_bool(pytestconfig, "drizz_capture_network"),
        client_name=request.node.name,
    )
    # Stash on the node so the hook below can attach artifacts.
    request.node._drizz_session = sess
    try:
        yield sess
    finally:
        try:
            arts = sess.artifacts()
        except Exception:
            arts = []
        sess.release()
        # Print artifact URLs once — visible in -v output; pytest-html
        # picks them up too.
        if arts:
            base = drizz_farm.base_url
            print(f"\n  drizz-farm artifacts for {request.node.name}:")
            for a in arts:
                print(f"    {a.type:10s} {a.filename:30s} {base}{a.url}")


# ---- Hook: on test failure, nudge a screenshot in-flight so it lands
# in the artifacts even when record_video is off. Best-effort; wrapped
# in try/except so a bad daemon doesn't turn a real test failure into
# a fixture error. ---------------------------------------------------


@pytest.hookimpl(hookwrapper=True)
def pytest_runtest_makereport(item: pytest.Item, call: pytest.CallInfo) -> Any:
    outcome = yield
    report = outcome.get_result()
    if report.when != "call" or not report.failed:
        return
    sess = getattr(item, "_drizz_session", None)
    if sess is None:
        return
    try:
        # One last screenshot capturing the failure state.
        png = sess.screenshot()
        setattr(report, "drizz_failure_screenshot", png)
    except Exception:
        pass
