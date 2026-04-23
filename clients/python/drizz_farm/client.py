"""
HTTP client for a drizz-farm daemon.

Two types:
    Farm     — the daemon's URL + high-level session creation
    Session  — a single active session; methods for every device API

Design:
    - Synchronous (httpx) — matches pytest's execution model.
      If you need async, wrap in `asyncio.to_thread` or send PRs.
    - One Session == one HTTP POST /sessions; release() closes it.
    - Error paths raise DrizzError with status + server message
      so test failures have actionable diagnostics.
    - The client never assumes which capture is on; attributes mirror
      whatever the daemon returned at session-create time.
"""
from __future__ import annotations

import time
from dataclasses import dataclass
from typing import Any, Iterable, Optional

import httpx


class DrizzError(RuntimeError):
    """Raised for any non-2xx response from the daemon."""

    def __init__(self, status: int, message: str, body: Any = None) -> None:
        super().__init__(f"[{status}] {message}")
        self.status = status
        self.message = message
        self.body = body


@dataclass
class ArtifactFile:
    """One file produced during a session (video/logcat/screenshot/network)."""

    type: str
    filename: str
    size: int
    url: str  # relative to the farm base URL

    @classmethod
    def from_dict(cls, d: dict) -> "ArtifactFile":
        return cls(
            type=d.get("type", "other"),
            filename=d.get("filename", ""),
            size=d.get("size", 0),
            url=d.get("url", ""),
        )


class Farm:
    """Entry point — the daemon itself."""

    def __init__(
        self,
        base_url: str,
        *,
        timeout: float = 30.0,
        api_version: str = "v1",
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_root = f"{self.base_url}/api/{api_version}"
        # A single client with connection pooling — individual requests
        # pass their own timeout so long-running ops (install, boot)
        # don't clash with the default.
        self._http = httpx.Client(timeout=timeout)

    # ---- Connection / discovery --------------------------------------

    def health(self) -> dict:
        r = self._http.get(f"{self.api_root}/node/health")
        _raise_for_status(r)
        return r.json()

    def devices(self, **filters: Any) -> list[dict]:
        """List devices, optionally filtered. Pass free=True / profile=/
        kind=android_emulator / reserved=False."""
        params = {k: _stringify(v) for k, v in filters.items() if v is not None}
        r = self._http.get(f"{self.api_root}/devices", params=params)
        _raise_for_status(r)
        return r.json().get("devices", [])

    def pool(self) -> dict:
        r = self._http.get(f"{self.api_root}/pool")
        _raise_for_status(r)
        return r.json()

    # ---- Session lifecycle -------------------------------------------

    def create_session(
        self,
        *,
        profile: Optional[str] = None,
        device_id: Optional[str] = None,
        avd_name: Optional[str] = None,
        source: str = "python-client",
        timeout_minutes: Optional[int] = None,
        record_video: bool = False,
        capture_logcat: bool = False,
        capture_screenshots: bool = True,
        capture_network: bool = False,
        retention_hours: Optional[int] = None,
        client_name: Optional[str] = None,
        wait_until_ready: bool = True,
        wait_timeout_s: float = 90.0,
    ) -> "Session":
        """Create + return a Session.

        Pass device_id or avd_name for a specific instance; otherwise
        we match by profile (or the daemon picks the first available).
        Capture flags set which artifacts we save at release.

        If wait_until_ready is True (default) we poll the session until
        state == "active" — useful when the farm has to boot a cold
        emulator on demand.
        """
        body: dict[str, Any] = {"source": source}
        if profile:
            body["profile"] = profile
        if device_id:
            body["device_id"] = device_id
        if avd_name:
            body["avd_name"] = avd_name
        if timeout_minutes is not None:
            body["timeout_minutes"] = timeout_minutes
        if client_name:
            body["client_name"] = client_name
        caps: dict[str, Any] = {
            "record_video": record_video,
            "capture_logcat": capture_logcat,
            "capture_screenshots": capture_screenshots,
            "capture_network": capture_network,
        }
        if retention_hours is not None:
            caps["retention_hours"] = retention_hours
        body["capabilities"] = caps

        r = self._http.post(f"{self.api_root}/sessions", json=body)
        _raise_for_status(r)
        sess = Session(self, r.json())
        if wait_until_ready and sess.state != "active":
            sess.wait_until_active(timeout_s=wait_timeout_s)
        return sess

    # ---- Low-level -------------------------------------------------

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> "Farm":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()


class Session:
    """Handle to one active session on the farm."""

    def __init__(self, farm: Farm, data: dict) -> None:
        self._farm = farm
        self._http = farm._http
        self._data = data

    # ---- Introspection ---------------------------------------------

    @property
    def id(self) -> str:
        return self._data["id"]

    @property
    def state(self) -> str:
        return self._data.get("state", "unknown")

    @property
    def instance_id(self) -> str:
        return self._data.get("instance_id", "")

    @property
    def serial(self) -> str:
        return self._data.get("connection", {}).get("adb_serial", "")

    @property
    def appium_url(self) -> Optional[str]:
        return self._data.get("connection", {}).get("appium_url")

    def refresh(self) -> dict:
        r = self._http.get(f"{self._farm.api_root}/sessions/{self.id}")
        _raise_for_status(r)
        self._data = r.json()
        return self._data

    def wait_until_active(self, *, timeout_s: float = 90.0, poll_s: float = 1.0) -> None:
        deadline = time.monotonic() + timeout_s
        while time.monotonic() < deadline:
            self.refresh()
            if self.state == "active":
                return
            if self.state in ("released", "timed_out", "error"):
                raise DrizzError(0, f"session ended in state={self.state} before becoming active")
            time.sleep(poll_s)
        raise DrizzError(0, f"session did not reach active within {timeout_s}s (last state={self.state})")

    # ---- Device simulation -----------------------------------------

    def set_gps(self, latitude: float, longitude: float) -> dict:
        return self._post("gps", {"latitude": latitude, "longitude": longitude})

    def set_network(self, profile: str) -> dict:
        """Profiles: 2g|3g|4g|5g|wifi_slow|wifi_fast|offline|flaky."""
        return self._post("network", {"profile": profile})

    def set_battery(self, level: int, status: Optional[str] = None) -> dict:
        body: dict[str, Any] = {"level": level}
        if status:
            body["status"] = status
        return self._post("battery", body)

    def set_orientation(self, orientation: str) -> dict:
        """'portrait' or 'landscape'."""
        return self._post("orientation", {"orientation": orientation})

    def set_locale(self, locale: str) -> dict:
        """e.g. 'en_US', 'en_GB', 'ja_JP'."""
        return self._post("locale", {"locale": locale})

    def set_timezone(self, timezone: str) -> dict:
        """IANA tz string, e.g. 'America/New_York'."""
        return self._post("timezone", {"timezone": timezone})

    def set_dark_mode(self, dark: bool) -> dict:
        return self._post("appearance", {"dark": dark})

    def set_font_scale(self, scale: float) -> dict:
        return self._post("font-scale", {"scale": scale})

    def set_animations(self, enabled: bool) -> dict:
        return self._post("animations", {"enabled": enabled})

    def set_volume(self, level: int) -> dict:
        return self._post("volume", {"level": level})

    def set_clipboard(self, text: str) -> dict:
        return self._post("clipboard", {"text": text})

    def set_sensor(self, name: str, values: str) -> dict:
        """name = acceleration|gyroscope|proximity; values colon-separated."""
        return self._post("sensor", {"name": name, "values": values})

    def shake(self) -> dict:
        return self._post("shake", {})

    def lock(self) -> dict:
        return self._post("lock", {"action": "lock"})

    def unlock(self) -> dict:
        return self._post("lock", {"action": "unlock"})

    def press_key(self, keycode: str) -> dict:
        """keycode can be a name ('HOME', 'BACK') or an integer code."""
        return self._post("key", {"keycode": keycode})

    # ---- Apps ------------------------------------------------------

    def install_apk(self, apk_bytes: bytes) -> dict:
        """Upload the APK bytes over multipart and install it."""
        r = self._http.post(
            f"{self._farm.api_root}/sessions/{self.id}/install",
            files={"apk": ("app.apk", apk_bytes, "application/vnd.android.package-archive")},
            timeout=120.0,
        )
        _raise_for_status(r)
        return r.json()

    def install_apk_path(self, host_path: str) -> dict:
        """Install an APK that already lives on the daemon's host filesystem."""
        r = self._http.post(
            f"{self._farm.api_root}/sessions/{self.id}/install",
            json={"path": host_path},
            timeout=120.0,
        )
        _raise_for_status(r)
        return r.json()

    def uninstall(self, package: str) -> dict:
        return self._post("uninstall", {"package": package})

    def clear_data(self, package: str) -> dict:
        return self._post("clear-data", {"package": package})

    def grant_permission(self, package: str, permission: str) -> dict:
        return self._post(
            "permissions",
            {"package": package, "permission": permission, "grant": True},
        )

    def revoke_permission(self, package: str, permission: str) -> dict:
        return self._post(
            "permissions",
            {"package": package, "permission": permission, "grant": False},
        )

    def package_info(self, package: str) -> dict:
        r = self._http.get(
            f"{self._farm.api_root}/sessions/{self.id}/package-info",
            params={"package": package},
        )
        _raise_for_status(r)
        return r.json()

    def open_deeplink(self, url: str) -> dict:
        return self._post("deeplink", {"url": url})

    # ---- Files + media ---------------------------------------------

    def upload_file(self, data: bytes, *, target: Optional[str] = None, filename: str = "upload.bin") -> dict:
        """Push bytes to the device. target defaults to /sdcard/Download/<filename>."""
        files = {"file": (filename, data, "application/octet-stream")}
        form: dict[str, str] = {}
        if target:
            form["target"] = target
        r = self._http.post(
            f"{self._farm.api_root}/sessions/{self.id}/files/upload",
            files=files,
            data=form,
            timeout=60.0,
        )
        _raise_for_status(r)
        return r.json()

    def inject_camera_image(self, image_bytes: bytes, filename: str = "inject.jpg") -> dict:
        """Drop an image into /sdcard/DCIM/Camera and kick the media scanner
        so the gallery picker sees it — the standard mock-a-photo flow."""
        r = self._http.post(
            f"{self._farm.api_root}/sessions/{self.id}/camera",
            files={"image": (filename, image_bytes, "image/jpeg")},
            timeout=30.0,
        )
        _raise_for_status(r)
        return r.json()

    def screenshot(self) -> bytes:
        """Take a screenshot right now. Requires capture_screenshots=True on create."""
        r = self._http.post(f"{self._farm.api_root}/sessions/{self.id}/screenshot", timeout=10.0)
        _raise_for_status(r)
        return r.content

    # ---- Biometric + notifications ---------------------------------

    def enroll_fingerprint(self) -> dict:
        return self._post("biometric", {"action": "enroll"})

    def fingerprint_touch(self, *, fail: bool = False) -> dict:
        return self._post("biometric", {"action": "fail" if fail else "touch"})

    def push_notification(self, title: str, body: str, *, tag: Optional[str] = None) -> dict:
        payload: dict[str, str] = {"title": title, "body": body}
        if tag:
            payload["tag"] = tag
        return self._post("push-notification", payload)

    # ---- Raw ADB escape hatch --------------------------------------

    def shell(self, command: str) -> str:
        """Run a shell command on the device and return stdout. The server
        prepends `adb shell` internally — pass the raw remote command."""
        r = self._http.post(
            f"{self._farm.api_root}/sessions/{self.id}/adb",
            json={"command": command},
            timeout=30.0,
        )
        _raise_for_status(r)
        data = r.json()
        return data.get("output", "")

    # ---- Artifacts -------------------------------------------------

    def artifacts(self) -> list[ArtifactFile]:
        """List artifacts produced so far — video, logcat, screenshots, network HAR."""
        r = self._http.get(f"{self._farm.api_root}/sessions/{self.id}/artifacts")
        _raise_for_status(r)
        return [ArtifactFile.from_dict(a) for a in r.json().get("artifacts", [])]

    def download_artifact(self, filename: str) -> bytes:
        r = self._http.get(
            f"{self._farm.api_root}/sessions/{self.id}/artifacts/{filename}",
            timeout=60.0,
        )
        _raise_for_status(r)
        return r.content

    # ---- Release ---------------------------------------------------

    def release(self) -> None:
        """Stop captures, return the emulator to the pool, finalize
        the session. Idempotent — safe to call in a try/finally."""
        try:
            r = self._http.delete(f"{self._farm.api_root}/sessions/{self.id}")
            if r.status_code == 404:
                return  # already released
            _raise_for_status(r)
        except httpx.HTTPError:
            # Best-effort: we want tests to clean up, not propagate
            # release-time connection hiccups as test failures.
            pass

    def __enter__(self) -> "Session":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.release()

    # ---- Internal --------------------------------------------------

    def _post(self, path: str, body: dict) -> dict:
        r = self._http.post(
            f"{self._farm.api_root}/sessions/{self.id}/{path}",
            json=body,
            timeout=20.0,
        )
        _raise_for_status(r)
        try:
            return r.json()
        except ValueError:
            return {}


# ---- Utilities -----------------------------------------------------


def _raise_for_status(r: httpx.Response) -> None:
    if 200 <= r.status_code < 300:
        return
    body: Any = None
    msg = f"HTTP {r.status_code}"
    try:
        body = r.json()
        if isinstance(body, dict):
            msg = body.get("message") or body.get("error") or msg
    except ValueError:
        body = r.text
        msg = r.text[:200] if r.text else msg
    raise DrizzError(r.status_code, msg, body)


def _stringify(v: Any) -> str:
    if isinstance(v, bool):
        return "true" if v else "false"
    return str(v)
