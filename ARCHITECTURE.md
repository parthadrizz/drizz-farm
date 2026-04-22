# drizz-farm Architecture

Self-hosted emulator pool that turns Mac hardware into a full device lab. Single Go binary with embedded React dashboard.

## Design Philosophy

**Each node is independent. A registry tells you where to find them. Browser talks to each node directly.**

That's it. No leader election. No peer-to-peer sync. No distributed consensus. No cross-node proxying on the backend.

### Why this model

A device lab is not a distributed database. It's a bunch of Mac Minis running emulators. The only thing you need to coordinate is: "which nodes exist and where are they?" That's a service registry, not a federation.

Every distributed-systems pattern you might reach for (leader election, quorum writes, version vectors) is solving a problem you don't have. If the registry goes away, every node still works at its own URL. If a node crashes, nothing else cares — it's just offline.

### What this gives us

- **No SPOF in practice** — any node serves the registry, any node serves its own devices
- **Trivially scales** — adding a node = adding a line to the registry
- **Works identically** across LAN / VPN / cloud hub deployments (see below)
- **Most distributed-systems bugs are impossible by construction** — no peer list to go stale, no leader to flap, no version to conflict

## Deployment Modes

Same binary, three ways to reach it.

### 1. LAN
```
Browser → mac-mini-1.local:9401   (Apple mDNS provides this free)
Browser → mac-mini-2.local:9401
```
Each node's config has an `external_url`. Defaults to `hostname.local:port`. Registry on any node lists everyone.

### 2. VPN (Tailscale / WireGuard)
```
Browser → mac-mini-1.ts.net:9401
Browser → mac-mini-2.ts.net:9401
```
Each node sets `external_url: <name>.ts.net`. Works from home, office, anywhere on the VPN. Zero code change — just config.

### 3. Cloud hub (SaaS future)
```
                                      ┌─── outbound WS ───┐
Browser → farm.drizz.ai/nodes/m1     ├── Hub server ────┼─→ mac-mini-1
          farm.drizz.ai/nodes/m2     └───────────────────┘── mac-mini-2
```
Each node maintains one **outbound** WebSocket to the hub. Hub proxies browser requests back through that tunnel. Solves NAT/firewall/auth/billing in one shot. This is the monetizable version (drizz.ai).

**The node code is the same in all three modes.** Only `external_url` and an optional hub-dial flag change.

## Components

```
drizz-farm/
  cmd/              CLI (setup, start, create, status, daemon)
  internal/
    android/        SDK detection, ADB, emulator launch, AVD management, USB scanning
    api/            HTTP handlers, embedded React dashboard, CORS
    appium/         Per-session Appium server lifecycle
    config/         YAML config parsing
    daemon/         macOS launchd integration (auto-start, auto-restart)
    device/         Device interface (emulator, USB, iOS)
    discovery/      mDNS announce + hostname registration (dns-sd)
    health/         Per-device health probes
    license/        Tier gating (free / pro / team)
    pool/           Device pool: semaphore, state machine, periodic ADB rescan
    registry/       Node registry — who's in the group, at what URL
    session/        Session broker: create, timeout, release
    store/          SQLite persistence (session history, metrics)
    webhook/        Outbound event notifications
  web/              React + Tailwind dashboard
  main.go           Entry point
```

## How nodes find each other

```yaml
# ~/.drizz-farm/nodes.yaml  (served by any node at GET /nodes)
group_name: my-lab
group_key: <shared secret, gates who can register>
nodes:
  - name: mac-mini-1
    url:  http://mac-mini-1.local:9401
  - name: mac-mini-2
    url:  http://mac-mini-2.local:9401
```

- Any node serves `GET /nodes` → returns this list
- Dashboard loaded from `mac-mini-1` fetches `/nodes`, then queries each node's `/pool`, `/avds` **directly from the browser** (CORS enabled)
- Adding a node: edit `nodes.yaml` on any one node, it pushes to the others
- Removing a node: opposite

No leader. No election. The registry is just a list.

## Device Lifecycle

```
OFFLINE → BOOTING → WARM → ALLOCATED → RESETTING → WARM
                      ↑                    │
                      └────────────────────┘
                               (or ERROR on health fail)
```

- **Semaphore** (buffered channel, size = `max_concurrent`) prevents over-allocation
- **Periodic ADB rescan** adopts emulators started outside drizz-farm (e.g. Android Studio)
- Each node manages its own pool. No cross-node allocation.

## Session Flow

```
POST /sessions {profile: "default"}        (hits one specific node)
    ↓
broker.Create() on that node
    ↓
local pool has capacity?
    yes → allocate, boot if needed, start Appium
    no  → queue (bounded) or 503
    ↓
Session ACTIVE with connection info (host, adb_port, serial)
```

**Routing decision = browser's job.** Dashboard sees everyone's capacity via the registry, picks the right node, calls it directly. No backend federation.

## Hostname Resolution

| Setup | URL | Who provides it |
|---|---|---|
| Standalone node | `hostname.local:9401` | macOS mDNS (built-in) |
| Multiple nodes, LAN | `hostname.local:9401` each, registry aggregates | macOS mDNS |
| Multiple nodes, VPN | `<name>.ts.net:9401` each | VPN's DNS |
| Cloud hub | `farm.drizz.ai/nodes/<name>` | Hub routing |

No custom `.local` hostnames. No leader owning a shared hostname. Each node is reachable as itself.

## API Surface

| Endpoint | Purpose |
|---|---|
| `GET /nodes` | Full member list (name, url, capabilities) |
| `POST /nodes` | Add a node (signed with group key) |
| `DELETE /nodes/:name` | Remove a node |
| `GET /pool` | This node's devices |
| `POST /pool/boot` | Boot an AVD on this node |
| `POST /pool/shutdown` | Shutdown a device on this node |
| `POST /sessions` | Create session on this node |
| `GET /sessions/:id/screen` | WebSocket stream (PNG or WebRTC) |
| `/sessions/:id/...` | All device control (GPS, battery, ADB exec, etc.) |
| `GET /node/health` | This node's stats |
| `GET /config`, `PUT /config` | Node config |

**No `/federation/*`. No `/mesh/*`. No cross-node proxies.** A node only knows about itself.

## Config (`~/.drizz-farm/config.yaml`)

```yaml
node:
  name: mac-mini-1          # auto-detected hostname if empty
  external_url: ""          # what other browsers use to reach this node.
                            # empty = hostname.local:port (LAN default)
                            # set to mac-mini-1.ts.net for VPN
                            # set to https://... for tunnel

group:
  name: my-lab              # which group this node belongs to
  key: <secret>             # shared secret for registry auth
  registry_file: ~/.drizz-farm/nodes.yaml  # list of group members

pool:
  max_concurrent: 4         # semaphore size
  session_timeout_minutes: 60
  queue_max_size: 20
  profiles:
    android:
      default: {ram_mb: 2048, gpu: auto, snapshot: true}

api:
  host: 0.0.0.0
  port: 9401

sdk:                        # detected once by setup
  root: ~/Library/Android/sdk
  ...
```

## Build & Run

```bash
cd web && npm run build && cd ..
cp -r web/dist/* internal/api/dashboard/
go build -o bin/drizz-farm .

./bin/drizz-farm setup      # detect SDK, configure node
./bin/drizz-farm start      # or: daemon install (launchd, auto-start on boot)
# Dashboard at http://hostname.local:9401
```

## Tests

```bash
go test ./...               # all packages
```

No more tests for leader election, version merging, peer sync. The complexity they were testing no longer exists.

---

## What we learned (by building the wrong thing first)

v1 was a federated mesh with leader election, versioned sync, and cross-node session routing. Every bug we hit — leader flip-flops, stale peer IPs across networks, 5s HTTP timeouts on 60s operations, `dns-sd` race loops, duplicate peer entries by `host:port`, zombie sessions from crashed federation daemons, peer dedup by name — **only existed because nodes coordinated with each other**.

The actual requirement was: "one URL that works, add Macs easily, no master to maintain." A registry delivers all three without the distributed systems. Federation was solving problems we didn't have.

This rewrite deletes ~500 lines of distributed coordination code and every one of those bug classes.
