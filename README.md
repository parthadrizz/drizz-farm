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
