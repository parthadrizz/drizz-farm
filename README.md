# drizz-farm

**A self-hosted Android device lab that runs on your own Macs.** One binary, zero cloud dependencies, full ADB root access.

Turn a Mac Mini (or any Mac) into an emulator pool with a web dashboard. Add more Macs to scale horizontally. Tests run via Appium, Espresso, Maestro, or whatever framework you like — drizz-farm just exposes the devices.

```
┌─────────────┐        ┌──────────────┐        ┌──────────────┐
│  Dashboard  │◀──────▶│   Mac Mini   │        │   Mac Mini   │
│  (browser)  │        │   4 AVDs     │◀──────▶│   4 AVDs     │
└─────────────┘        │   drizz-farm │        │   drizz-farm │
                       └──────────────┘        └──────────────┘
```

## Why

Running mobile tests shouldn't cost $500/month per seat. drizz-farm replaces BrowserStack/LambdaTest for teams who already own Mac hardware:

- **Full access.** Real ADB, real root. Install any APK. Instrument anything.
- **Unlimited minutes.** No per-minute pricing, no parallel test caps.
- **Your data stays local.** Session artifacts live on your Macs, not somebody's cloud.
- **Horizontal scale.** Add Macs; each one contributes its emulators to the pool.
- **Single binary, zero infra.** Go daemon with an embedded React dashboard.

## Install

```bash
# macOS (Apple Silicon or Intel)
curl -fsSL https://get.drizz.ai | bash

# or Homebrew
brew install drizz-dev/tap/drizz-farm
```

Then:

```bash
drizz-farm setup       # one-time: detect Android SDK, install as launchd service
```

Setup auto-installs a launchd service so the daemon starts at login. Dashboard at `http://<hostname>.local:9401`.

### macOS firewall prompt (first run)

The first time drizz-farm or an emulator accepts an incoming connection, macOS shows an **"Application Firewall"** dialog asking whether to allow it. Clicking **Allow** once is enough — the prompt doesn't come back.

If you're rolling out to many Macs and want to skip the prompt, pre-authorize during provisioning (requires one `sudo`):

```bash
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --add /usr/local/bin/drizz-farm
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --unblockapp /usr/local/bin/drizz-farm
```

These commands are safe to re-run and no-op if the binary is already registered. Once we code-sign and notarize the release builds (planned for v0.9), the firewall + Gatekeeper prompts go away entirely.

## Quickstart

### 1. First Mac — run `setup`

`setup` detects the Android SDK, creates a config, and installs the daemon. If you don't have the Android SDK yet:

```bash
brew install --cask android-commandlinetools
# or install Android Studio from https://developer.android.com/studio
```

Then:

```bash
drizz-farm setup
```

### 2. Create an AVD

Open the dashboard (`http://<hostname>.local:9401`), click **+ Add**, pick a device and system image. Or from the CLI:

```bash
drizz-farm create --profile default --count 2
```

### 3. Boot an emulator

Click **Start** next to any AVD in the dashboard. It boots headless (software GPU so screen capture works), shows up as warm in ~30 seconds.

Or via API:

```bash
curl -X POST http://localhost:9401/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{"profile": "default"}'
```

The response gives you an ADB port you can connect to. Your test runner talks to it normally.

### 4. (optional) Add more Macs

On the first Mac, go to **Settings → Group → Create group**. Copy the generated key.

On the second Mac:

```bash
drizz-farm join http://<first-mac>.local:9401 <group-key>
```

Or use **Settings → Group → Join group** from the second Mac's dashboard.

Each Mac still runs its own emulators. The group is a shared directory — the dashboard on any node shows devices from all of them. Cross-node requests go from the browser directly to the target Mac (no backend proxying).

## What you get

| | |
|---|---|
| **Pool management** | Semaphore-limited concurrency, boot-on-demand, idle shutdown, health probes |
| **Session lifecycle** | Create, allocate, timeout, release, queue when exhausted |
| **Device control API** | GPS, battery, network, orientation, locale, dark mode, APK install, deeplinks, ADB exec |
| **Screen streaming** | WebSocket PNG + WebRTC H.264 |
| **Recording** | Video, screenshots, logcat, HAR |
| **Persistence** | SQLite for session history |
| **Webhooks** | Fire on session events |
| **Multi-node** | Group of Macs, shared dashboard view |
| **Auto-start** | macOS launchd integration |

## Architecture

drizz-farm is deliberately simple: each node is independent, a registry lists where they are, and the browser routes between them. No leader election, no mesh consensus, no backend-to-backend coordination.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for details.

## How it compares

| | drizz-farm | [appium-device-farm](https://github.com/AppiumTestDistribution/appium-device-farm) | [STF](https://github.com/openstf/stf) |
|---|---|---|---|
| **Status** | Actively developed | Active (~240 releases) | **Unmaintained** (last release 2020) |
| **Install** | One brew / curl install, single Go binary | Appium 2.0 + Node + Prisma + DB | Node 8 + RethinkDB + ZeroMQ + protobuf |
| **Android emulators** | ✓ first-class, boot-on-demand | ✓ | ✗ USB phones only |
| **iOS** | Stubbed (planned) | ✓ simulators + physical | ✗ |
| **Appium required** | ✗ optional — works with raw ADB | **✓ plugin for Appium** | ✗ (has its own control) |
| **Declarative session capabilities** | ✓ `record_video` / `capture_logcat` / `capture_network` / `capture_screenshots` at create time | imperative start/stop per artifact | basic screenshots |
| **Unified artifacts endpoint** | ✓ `GET /sessions/{id}/artifacts` lists + downloads all | separate endpoints per type | file download per screenshot |
| **Device reservations** | ✓ with label, persisted across restarts, source-aware allocator | session locking | device booking (legacy) |
| **Specific-device allocation** | ✓ `device_id` OR `avd_name` OR profile, fail-fast on busy | yes | limited |
| **Multipart file / APK upload** | ✓ HTTP multipart from any CI | partial | partial |
| **Camera injection → gallery** | ✓ multipart → `/sdcard/DCIM/Camera` + media scanner | not built in | not built in |
| **Device simulation APIs** | 25+ endpoints (GPS, battery, sensors, permissions, timezone, dark mode, push notifications, clipboard, biometric, …) | Appium-level subset | basic remote control |
| **Live streaming** | WebRTC H.264 (primary) + PNG WebSocket (fallback) | session recording replay | real-time screen share |
| **Multi-node topology** | Registry (each node independent, no master) | Hub/node | Central provider |
| **Tech** | Go daemon + embedded React + SQLite | TypeScript + React + Prisma | Node 8 + RethinkDB |
| **License** | Apache 2.0 | Apache 2.0 | Apache 2.0 |
| **Stars** | new | 588 | 13.9k (legacy) |

**The short version:**

- **vs STF** — STF is unmaintained. Don't compare; move on.
- **vs appium-device-farm** — they're great if you already use Appium. drizz-farm is for the "I don't want to set up Appium + Node + a DB just to run Android tests" crowd. Our install is one binary, our API is HTTP (any language), and our per-session capability model means you declare what you want captured at session create and we handle start/stop/cleanup — you don't orchestrate recording state from your test runner.

**Gaps we'll close:** iOS support, session playback UI with event overlay, JUnit/TestNG clients.

## Appium compatibility

Existing Appium suites can point at drizz-farm with a single URL change:

```python
from appium import webdriver

driver = webdriver.Remote(
    "http://farm.local:9401/wd/hub",   # ← drizz-farm, not an Appium hub
    desired_capabilities={
        "platformName": "Android",
        "appium:automationName": "UiAutomator2",
        "appium:app": "/path/to/app.apk",
        # Optional drizz-farm extensions
        "drizz:profile": "api34_play",
        "drizz:record_video": True,
        "drizz:capture_logcat": True,
    },
)
```

drizz-farm allocates the device, spawns Appium behind the scenes, and transparently proxies every W3C WebDriver command to it. On `driver.quit()`, captures stop, video lands in `/api/v1/sessions/{id}/artifacts`.

`drizz:*` capabilities map 1:1 to session options — `profile`, `device_id`, `avd_name`, `record_video`, `capture_logcat`, `capture_screenshots`, `capture_network`, `retention_hours`, `timeout_minutes`.

**Migrating an existing Appium suite?** Step-by-step guide for Python, Java, JS, Ruby, C# — all clients, all frameworks (including pytest, JUnit 5, TestNG) — in **[docs/APPIUM-MIGRATION.md](./docs/APPIUM-MIGRATION.md)**. In most cases it's a single-line URL change and your test code is untouched.

## Deployment modes

Same binary, three ways to deploy:

| Mode | URL | When to use |
|---|---|---|
| **LAN** | `hostname.local:9401` | Default. Office with a single subnet. |
| **VPN** | `<name>.ts.net:9401` | Team working from home on Tailscale. Set `node.external_url` in config. |
| **Cloud hub** | `farm.drizz.ai/nodes/<name>` | Coming soon. Paid SaaS — access from anywhere, SSO, audit, support. |

## Build from source

```bash
git clone https://github.com/parthadrizz/drizz-farm.git
cd drizz-farm
cd web && npm install && npm run build && cd ..
cp -r web/dist internal/api/dashboard
go build -o bin/drizz-farm .
./bin/drizz-farm setup
```

Requires Go 1.21+ and Node 18+.

### Release build (universal macOS binary)

```bash
make release-mac      # Apple Silicon + Intel in one binary
make release          # + tarballs + SHA256SUMS in ./dist/
```

### Running the API conformance suite

```bash
make test-capabilities
```

Boots the daemon, then walks every API added in v0.1.14 → v0.1.21 — sessions, device list + filters, reservations, declarative capture capabilities, unified artifacts endpoint, multipart file upload, camera injection, and every device-simulation endpoint — verifying each by reading the resulting state back via `adb shell`.

Requirements: an Android emulator must be reachable via ADB before you run the suite (boot one with `drizz-farm start` + `drizz-farm create`). Tests that depend on a warm device auto-skip if the pool is empty; everything else runs regardless.

The suite downloads Appium's [ApiDemos-debug.apk](https://github.com/appium/android-apidemos) once (~3 MB, cached at `/tmp/drizz-apidemos.apk`) to exercise install / permissions / clear-data / uninstall flows against a real, signed package.

Output looks like:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  drizz-farm API conformance suite
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ✓ Group: GET /group returns key for loopback
  ✓ Devices: GET /devices shape                  (1 device(s) listed)
  ✓ Reservation: reserve + label appears in list
  ✓ Session: create by device_id binds to that instance
  ✓ Capabilities: echo back on create
  ✓ Screenshot: gated when capability disabled
  ✓ Battery: set 42% → dumpsys shows 42          (level: 42)
  ✓ Locale: set en_GB → getprop persist.sys.locale (en-GB)
  ✓ Dark mode: enable → cmd uimode night         (Night mode: yes)
  ✓ GPS: set SF → dumpsys location shows 37.77
  ✓ Clipboard: set 'drizz-clip-probe' → cmd clipboard matches
  ✓ Push notification: posts + cmd notification list includes tag
  ✓ Install: multipart APK → pm list contains package (io.appium.android.apis)
  ✓ Permissions: grant READ_CONTACTS → granted=true
  ✓ Uninstall: pm list no longer contains package
  ...
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  RESULTS: 29 passed  0 failed  2 skipped  (118.4s)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

Skips usually mean "no warm emulator" (boot one) or "mitmproxy not installed" (`brew install mitmproxy`).

## CLI reference

```
drizz-farm setup                            # detect SDK, install launchd service
drizz-farm start [--visible]                # run daemon (foreground)
drizz-farm stop                             # stop running daemon
drizz-farm status                           # node + pool status
drizz-farm create --profile X --count N     # create N AVDs
drizz-farm discover                         # list installed AVDs + SDK images
drizz-farm session ...                      # CLI session management
drizz-farm join <url> <group-key>           # join an existing group
drizz-farm daemon install|uninstall|status  # manage launchd service
```

## Requirements

- macOS 12+ (Apple Silicon or Intel)
- Android SDK (Android Studio or command-line tools)
- ~3 GB RAM per running emulator (tune via `pool.max_concurrent` in config)
- Port 9401 available (configurable)

Linux support is experimental — the binary builds, but emulator launch, launchd integration, and mDNS need testing.

## License

Apache 2.0. See [LICENSE](./LICENSE).

---

Built by [Drizz Labs](https://drizz.ai). Questions? Open an issue.
