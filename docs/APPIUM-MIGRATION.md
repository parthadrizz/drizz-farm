# Migrating from Appium to drizz-farm

You have an existing Appium-based Android test suite. You want to start running it against drizz-farm — get device allocation, session video, logcat capture, reservations, etc. — without rewriting anything.

**In most cases this is a one-line change: swap the Appium server URL for drizz-farm's `/wd/hub` endpoint.** drizz-farm implements the W3C WebDriver protocol and proxies every command to the Appium server it spawns behind the scenes. Your test code, assertions, Page Objects — all unchanged.

This doc covers every scenario we've seen and how to handle it. If yours isn't here, open an issue on [parthadrizz/drizz-farm](https://github.com/parthadrizz/drizz-farm/issues) and we'll fold it in.

---

## Before you start

You need:
- A running drizz-farm daemon (`brew install drizz-ai/tap/drizz-farm` OR `curl -fsSL https://get.drizz.ai | bash`). Hostname below is `farm.local`; adjust to wherever yours runs.
- At least one warm Android emulator (`drizz-farm create && drizz-farm start`).
- Your existing Appium suite.

No drizz-farm library install required — we're exposing the same protocol your Appium client already speaks.

---

## The simplest case — URL swap only

Works for about 90% of suites. If your test app is already installed on the emulator, hosted at an HTTPS URL, or your tests drive mobile web (Chrome), nothing else changes.

### Python (`appium-python-client`)

```python
from appium import webdriver

# before
driver = webdriver.Remote("http://appium-hub:4723/wd/hub", desired_capabilities={...})

# after
driver = webdriver.Remote("http://farm.local:9401/wd/hub", desired_capabilities={...})
```

### Java (`appium-java-client`)

```java
// before
AppiumDriver driver = new AndroidDriver(
    new URL("http://appium-hub:4723/wd/hub"),
    caps);

// after
AppiumDriver driver = new AndroidDriver(
    new URL("http://farm.local:9401/wd/hub"),
    caps);
```

TestNG / JUnit setup — exactly the same, the URL is the only difference.

### JavaScript / Node (`webdriverio`)

```js
// before
const driver = await remote({
  hostname: "appium-hub",
  port: 4723,
  path: "/wd/hub",
  capabilities: {...},
});

// after
const driver = await remote({
  hostname: "farm.local",
  port: 9401,
  path: "/wd/hub",
  capabilities: {...},
});
```

### Ruby (`appium_lib`)

```ruby
# before
caps = { caps: {...}, appium_lib: { server_url: 'http://appium-hub:4723/wd/hub' } }
# after
caps = { caps: {...}, appium_lib: { server_url: 'http://farm.local:9401/wd/hub' } }
```

### C# (`Appium.WebDriver`)

```csharp
// before
var driver = new AndroidDriver(new Uri("http://appium-hub:4723/wd/hub"), caps);
// after
var driver = new AndroidDriver(new Uri("http://farm.local:9401/wd/hub"), caps);
```

That's the entire change. `driver.FindElement`, `driver.Click`, `driver.GetScreenshot`, `driver.Quit` — every call works.

---

## Enabling drizz-farm extras (optional)

You don't have to touch capabilities at all, but if you want session video, logcat capture, or to pin a specific emulator, add a `drizz:*` cap alongside your existing `appium:*` ones:

```python
driver = webdriver.Remote("http://farm.local:9401/wd/hub", desired_capabilities={
    "platformName": "Android",
    "appium:automationName": "UiAutomator2",
    "appium:appPackage": "com.example.app",
    "appium:appActivity": ".MainActivity",

    # drizz-farm extras — all optional
    "drizz:profile": "api34_play",           # match a pool profile
    "drizz:device_id": "abc123",             # OR pin a specific emulator
    "drizz:record_video": True,              # → video.mp4 saved on release
    "drizz:capture_logcat": True,            # → logcat.txt saved on release
    "drizz:capture_screenshots": True,       # → /screenshot endpoint enabled
    "drizz:capture_network": True,           # → network.har via mitmproxy
    "drizz:retention_hours": 48,             # → auto-delete artifacts after 48h
    "drizz:timeout_minutes": 30,             # → session hard-timeout
})

# Your test runs unchanged.
driver.find_element(AppiumBy.ID, "login_button").click()
# ...
driver.quit()
```

After `driver.quit()`, fetch the artifacts:

```bash
curl http://farm.local:9401/api/v1/sessions/<session_id>/artifacts
```

---

## The one real gotcha: local `appium:app` paths

If your test looks like this:

```python
"appium:app": "/Users/me/project/build/app.apk"   # ← path on YOUR laptop
```

…**that breaks**, because Appium is running on the drizz-farm host, not on your laptop. Appium can't see your filesystem.

This is exactly the same problem you'd have pointing at BrowserStack, LambdaTest, or any remote Appium hub. Three ways to fix:

### Option A — Install before session (recommended for CI)

Install the APK via drizz-farm's multipart upload endpoint, then use `appPackage` + `appActivity` in caps instead:

```python
import httpx

# 1. Create a placeholder session to get a device allocated
driver = webdriver.Remote("http://farm.local:9401/wd/hub", desired_capabilities={
    "platformName": "Android",
    "appium:automationName": "UiAutomator2",
    "drizz:record_video": True,
})

# 2. Install the APK directly via drizz-farm's install endpoint
with open("build/app.apk", "rb") as f:
    httpx.post(
        f"http://farm.local:9401/api/v1/sessions/{driver.session_id}/install",
        files={"apk": f},
    )

# 3. Launch the activity via Appium
driver.start_activity("com.example.app", ".MainActivity")

# ... test runs ...
driver.quit()
```

### Option B — Host the APK at an HTTPS URL

If your CI publishes artifacts (GitHub Actions, Jenkins, S3, GCS) to a reachable URL, pass that URL as `appium:app` and Appium itself will download it:

```python
"appium:app": "https://ci.example.com/builds/1234/app.apk"
```

No code change vs current — just update the cap value.

### Option C — Use the drizz-farm Python client

If you're happy adopting a thin wrapper, our [Python client](../clients/python/) handles this automatically:

```python
from drizz_farm import Farm

with Farm("http://farm.local:9401") as farm:
    with farm.create_session(record_video=True) as sess:
        sess.install_apk(open("build/app.apk", "rb").read())
        # Drive via sess.appium_url if you still want Appium,
        # OR use our direct API — either works.
```

---

## Running both Appium and drizz-farm side by side

You don't have to migrate all at once. Keep your existing Appium hub and add drizz-farm for new runs:

```python
import os
HUB = os.getenv("APPIUM_HUB", "http://appium-hub:4723/wd/hub")
driver = webdriver.Remote(HUB, caps)
```

Then:
```bash
# old flow
pytest

# drizz-farm flow
APPIUM_HUB=http://farm.local:9401/wd/hub pytest
```

Prove it works, flip the default when you're happy.

---

## What drizz-farm gives you that plain Appium doesn't

The reason to migrate is not a different test protocol — it's everything the daemon wraps around each session:

| | Plain Appium | **drizz-farm** |
|---|---|---|
| Device allocation | You bring your own device | Pool manager with reservations |
| Queue when pool exhausted | Test fails | Request queues for up to 5 min |
| Session video recording | Manual start/stop, manage files | Declare `drizz:record_video` once, we save + serve the MP4 |
| Logcat capture | You run `adb logcat` yourself | `drizz:capture_logcat` → `logcat.txt` |
| Network HAR | External mitmproxy setup | `drizz:capture_network` → `network.har` |
| Pinning a specific emulator | No native way | `drizz:device_id` |
| Dashboard + playback UI | None | `http://farm.local:9401/playback/<sid>` |
| Multi-machine farm | Manual Appium grid | Registry + group key |
| Cleanup of old artifacts | Your problem | Daemon retention sweeper |

The test code doesn't change. The operational wrapper around it does.

---

## Troubleshooting

**`HTTP 500 no device matches`** — pool is empty or every device is busy. Boot one: `curl -X POST http://farm.local:9401/api/v1/pool/boot -H 'Content-Type: application/json' -d '{"avd_name":"<name>"}'`.

**`HTTP 403 capability_disabled` on screenshot** — you passed `drizz:capture_screenshots: false`. Set it to `true` or omit (defaults to true).

**`HTTP 502 appium upstream`** — the per-session Appium server failed to start. Usually means the emulator isn't healthy. Check `GET /api/v1/pool` for the instance state; a `state: error` means reset + retry.

**Java driver prints `WARN: Unsupported feature...` at startup** — comes from some probes hitting our `/wd/hub/status`. It's informational; your tests still run. We return a minimal WD-shaped status response; if a specific capability probe is missing, file an issue.

**Test hangs at `driver.quit()`** — most common cause is the device getting stuck during screenrecord stop. Our `drizz-farm stop` will reap orphans; for in-process, catch the timeout and move on.

---

## Questions

- GitHub: [parthadrizz/drizz-farm/issues](https://github.com/parthadrizz/drizz-farm/issues)
- Docs: [drizz.ai](https://drizz.ai)
- Reply to the welcome email.
