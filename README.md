<div align="center">

<br/>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/logo-wordmark-dark.png" />
  <source media="(prefers-color-scheme: light)" srcset="assets/logo-wordmark-light.png" />
  <img src="assets/logo-wordmark-light.png" alt="tunr" width="200" />
</picture>

<br/><br/>

**Ship the apps your agent builds.**
One command — or one MCP call — and the thing Claude Code just wrote stops living on `localhost`.

[![Release](https://img.shields.io/github/v/release/ahmetvural79/tunr?color=7c3aed)](https://github.com/ahmetvural79/tunr/releases)
[![CLI: Apache 2.0](https://img.shields.io/badge/CLI-Apache--2.0-7c3aed.svg)](LICENSE)
[![Relay: PolyForm Shield](https://img.shields.io/badge/relay-PolyForm%20Shield-6b7280.svg)](relay/LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.22+-00add8)](go.mod)

[tunr.sh](https://tunr.sh) · [Docs](https://tunr.sh/docs) · [Dashboard](https://app.tunr.sh)

</div>

---

```bash
$ tunr deploy --name sprint
  ▲ Packing … 41 files, 210 KB
  ▲ building (nixpacks auto-detect)
  🚀 Live: https://sprint.tunr.sh
     (sleeps when idle, wakes on request)
```

Or don't type anything — tell your agent:

> *"deploy this to tunr"*

…and it calls `tunr_deploy` over MCP and hands you back the URL.

---

## Why

Coding agents produce a lot of small software: the internal dashboard, the
one-off scraper, the tool that does exactly one annoying thing for your team.
Almost all of it dies on `localhost`, because the gap between "it works on my
machine" and "my colleague can open it" is still an afternoon of Dockerfiles,
DNS and IAM.

tunr closes that gap to one command. The app builds with Nixpacks (no
Dockerfile), gets an HTTPS URL, sleeps when nobody's using it and wakes on the
next request — so hosting a dozen barely-used internal tools costs about what
hosting one does.

The 3-second tunnel tunr started as is still here, still free. It's now the
on-ramp rather than the product.

---

## Install

```bash
# macOS / Linux — Homebrew
brew install ahmetvural79/tap/tunr

# macOS / Linux — install script
curl -fsSL https://raw.githubusercontent.com/ahmetvural79/tunr/main/install.sh | sh

# Go
go install github.com/ahmetvural79/tunr/cmd/tunr@latest

# From source
git clone https://github.com/ahmetvural79/tunr.git && cd tunr && make build
```

A single static binary. macOS, Linux and Windows, amd64 and arm64, no runtime
dependencies. Verify it with `tunr doctor`.

**SDKs** — for driving tunnels from code rather than the shell:

```bash
pip install tunr          # Python
npm install @tunr/cli     # Node.js
```

---

## Cloud — deploy & host

```bash
tunr login                        # magic-link, token goes to your OS keychain
tunr deploy --name my-app         # build + run; prints the live URL
tunr apps                         # list your apps
tunr apps logs my-app --follow    # stream build + runtime logs
tunr apps delete my-app           # remove it, free the subdomain
```

| | |
|---|---|
| **Build** | Nixpacks auto-detect — Node, Python, Go, Ruby, Rust, PHP, Java. A `Dockerfile` is used if present. |
| **Isolation** | Every app runs in its own gVisor sandbox with CPU, memory and disk quotas. |
| **Scale to zero** | Idle → paused with its memory reclaimed (~20 MB resident), then stopped. Wake p50 is ~150 ms. |
| **Secrets** | `.env` files are never uploaded. Pass them with `--env KEY=VALUE`. |
| **Stays up** | Close your laptop. The app keeps serving. |

```bash
tunr deploy --name sprint --port 3000 --env DATABASE_URL=postgres://…
```

> **Status: preview.** Deploy works end to end and is what the MCP tools drive.
> Role-based sharing, per-app SQLite and `tunr rollback` are **not built yet** —
> see [Roadmap](#roadmap) for what's real and what isn't.

---

## Agent-native (MCP)

The CLI and the MCP server are two faces of the same control plane. Anything
you can do in a terminal, your agent can do in a tool call.

**Claude Code:**

```bash
claude mcp add tunr -- tunr mcp
```

**Claude Desktop** (`~/.claude/claude_desktop_config.json`) **/ Cursor** (`.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "tunr": { "command": "tunr", "args": ["mcp"] }
  }
}
```

### Tools

| Tool | What it does |
|---|---|
| `tunr_deploy` | Build & host a directory on the tunr cloud, return the live URL |
| `tunr_list_apps` | List deployed apps with URL and status |
| `tunr_app_logs` | Read an app's build + runtime logs (for debugging a bad deploy) |
| `tunr_delete_app` | Delete an app and free its subdomain |
| `tunr_share` | Open a temporary public tunnel to a running local port |
| `tunr_status` | List active tunnels |
| `tunr_inspect` | List HTTP requests captured by a tunnel |
| `tunr_replay` | Replay a captured request against your local server |
| `tunr_stop` | Close a tunnel |

`tunr_deploy` and `tunr_share` are deliberately distinct, and the tool
descriptions say so: *deploy* means "host this, it should outlive my laptop",
*share* means "expose what's running on :3000 right now". Cloud tools need
`tunr login` first.

---

## Tunnels

Still free, still unlimited, still 3 seconds.

```bash
tunr share --port 3000
#  🚀 https://abc1x2y3.tunr.sh
```

HTTP/HTTPS with WebSocket (HMR works), plus raw **TCP**, **UDP** and
end-to-end-encrypted **TLS** tunnels — all multiplexed over one connection.
Regions: `ams` (Amsterdam), `sea` (Seattle), `sin` (Singapore).

<details>
<summary><b>Demo features</b> — freeze, read-only, feedback widget, auto-login</summary>

<br/>

Built for showing work-in-progress to someone who is not a developer.

**❄️ Freeze Mode (`--freeze`)** — if your local server crashes mid-demo, tunr
serves the last successful response from memory. The other person never sees a
broken page.

```bash
tunr share --port 3000 --freeze
```

**🛡️ Read-Only Demo Mode (`--demo`)** — intercepts `POST`, `PUT` and `DELETE`
at the proxy layer. They can click "Place Order"; nothing writes to your
database.

```bash
tunr share --port 3000 --demo
```

**💬 Feedback Widget (`--inject-widget`)** — injects an overlay into every HTML
page served through the tunnel. Viewers pin visual feedback; it arrives in your
terminal in real time.

```bash
tunr share --port 3000 --inject-widget
```

**🔑 Auto-Login Bypass (`--auto-login`)** — inject an auth cookie so the visitor
lands on a demo account. No signup, no email verification.

```bash
tunr share --port 3000 --auto-login "Cookie: session=demo-token"
```

All at once:

```bash
tunr share --port 3000 --demo --freeze --inject-widget
```

</details>

<details>
<summary><b>Access control</b> — password, bearer token, IP whitelist, TTL, QR</summary>

<br/>

```bash
# Basic auth (user optional)
tunr share -p 8080 --password "secret"
tunr share -p 8080 --password "client:secret"

# Bearer token — Authorization: Bearer <token> or ?token=<token>
tunr share -p 3000 --auth-token "my-super-secret-key"

# IP whitelist (CIDR)
tunr share -p 3000 --allow-ip "203.0.113.0/24"
tunr share -p 3000 --allow-ip "10.0.0.0/8,172.16.0.0/12"

# Auto-expire — the tunnel closes itself
tunr share -p 3000 --ttl 1h30m

# QR code, for opening it on a phone
tunr share -p 3000 --qr
```

</details>

<details>
<summary><b>Routing & headers</b> — path routing, regions, header rewriting, CORS, proxies</summary>

<br/>

```bash
# Path routing — one public URL, several local ports
tunr share --route /=3000 --route /api=8080

# Region selection
tunr share --port 3000 --region ams     # Amsterdam (EU)
tunr share --port 3000 --region sea     # Seattle (US West)
tunr share --port 3000 --region sin     # Singapore (APAC)

# Header rewriting
tunr share -p 3000 --header-add "X-Debug: true"
tunr share -p 3000 --header-replace "Host: internal.local"
tunr share -p 3000 --header-remove "X-Powered-By"

# Forwarded headers — X-Forwarded-For (real client IP), X-Original-URL
tunr share -p 3000 --x-forwarded-for --original-url

# CORS preflight without touching your server
tunr share -p 3000 --cors-origin "https://myapp.com"

# Behind a corporate proxy
tunr share -p 3000 --proxy http://proxy:8080

# Custom domain (Pro)
tunr share -p 3000 --domain demo.client.com
```

</details>

<details>
<summary><b>TCP / UDP / TLS</b> — databases, SSH, game servers, zero-knowledge passthrough</summary>

<br/>

```bash
# TCP — raw bytes, no HTTP parsing on the relay
tunr tcp --port 5432                          # PostgreSQL
tunr tcp --port 22 --qr                       # SSH, QR for mobile
tunr tcp --port 6379 --allow-ip 10.0.0.0/8    # Redis, restricted
tunr tcp --port 3306 --region ams             # MySQL, EU relay

# UDP — DNS, game servers, anything datagram
tunr udp --port 53
tunr udp --port 27015 --region ams

# TLS — end-to-end encrypted, SNI passthrough. The relay cannot read it.
tunr tls --port 8443
```

</details>

<details>
<summary><b>Daemon, service & multi-tunnel</b></summary>

<br/>

```bash
# Background daemon
tunr start --port 3000
tunr status
tunr stop

# Several tunnels from .tunr.json
tunr up
tunr down

# Install as a system service (systemd / launchd) — starts on boot
tunr service install --port 3000
tunr service status
tunr service uninstall
```

</details>

<details>
<summary><b>Inspect & replay</b> — the local HTTP inspector</summary>

<br/>

```bash
tunr open           # dashboard at http://localhost:19842
tunr logs --follow  # stream requests in the terminal
tunr replay <id>    # re-send a captured request to your local server
```

Live request/response stream, headers, body, timing, one-click replay, export
as a `curl` command. Everything stays on your machine.

</details>

<details>
<summary><b>Full CLI reference</b></summary>

<br/>

| Command | Description |
|---------|-------------|
| **Cloud** | |
| `tunr deploy [dir]` | Build & host a project; `--name`, `--port`, `--env KEY=VAL` |
| `tunr apps` | List your cloud apps |
| `tunr apps logs <name>` | Stream logs; `--follow`, `--tail N` |
| `tunr apps delete <name>` | Delete an app |
| **Tunnels** | |
| `tunr share -p PORT` | Expose a local port over HTTPS |
| `tunr share -p PORT -s NAME` | Custom subdomain (Pro) |
| `tunr share --route /PATH=PORT` | Map URL paths to local ports |
| `tunr share -p PORT --password PASS` | Basic authentication |
| `tunr share -p PORT --ttl 1h` | Auto-close after a duration |
| `tunr share -p PORT --demo` | Read-only demo mode |
| `tunr share -p PORT --freeze` | Serve last-good response on crash |
| `tunr share -p PORT --inject-widget` | Inject the feedback widget |
| `tunr share -p PORT --auto-login "Cookie: s=demo"` | Auto-inject an auth cookie |
| `tunr share -p PORT --domain HOST` | Custom domain |
| `tunr share -p PORT --qr` | Print a QR code for the URL |
| `tunr share -p PORT --auth-token TOKEN` | Bearer token protection |
| `tunr share -p PORT --allow-ip CIDR` | IP whitelist |
| `tunr share -p PORT --header-add/-replace/-remove` | Rewrite headers |
| `tunr share -p PORT --x-forwarded-for --original-url` | Proxy headers |
| `tunr share -p PORT --cors-origin ORIGIN` | CORS preflight |
| `tunr share -p PORT --proxy URL` | HTTP/SOCKS5 proxy |
| `tunr share -p PORT --region ams\|sea\|sin` | Pick a relay region |
| `tunr share -p PORT --json` | JSON output for CI |
| `tunr tcp -p PORT` / `tunr udp -p PORT` / `tunr tls -p PORT` | TCP / UDP / TLS tunnels |
| `tunr up` / `tunr down` | Start/stop everything in `.tunr.json` |
| `tunr start` / `tunr stop` / `tunr status` | Daemon mode |
| `tunr service install\|status\|uninstall` | System service |
| **Everything else** | |
| `tunr login` / `tunr logout` | Authentication |
| `tunr open` / `tunr logs` / `tunr replay <id>` | Inspector |
| `tunr mcp` | Start the MCP server (stdio) |
| `tunr config init` / `tunr config show` | `.tunr.json` |
| `tunr doctor` | Diagnose your setup |
| `tunr update` / `tunr uninstall` / `tunr version` | Maintenance |

Global flags: `--relay URL` (point at your own relay), `--verbose`.

</details>

---

## SDKs

<details>
<summary><b>Python</b> — <code>pip install tunr</code></summary>

<br/>

```python
from tunr import TunrClient, TunnelOptions

client = TunrClient()

tunnel = client.share(port=3000)
print(tunnel.public_url)

db_tunnel  = client.tcp(port=5432)
dns_tunnel = client.udp(port=53)
tls_tunnel = client.tls(port=8443)

opts = TunnelOptions(
    subdomain="myapp",
    password="demo123",
    allow_ips=["10.0.0.0/8"],
    freeze=True,
    inject_widget=True,
    proxy="http://proxy:8080",
    ttl="2h",
)
tunnel = client.share(port=8080, opts=opts)

requests = client.get_requests(tunnel.subdomain)
client.replay_request(tunnel.subdomain, requests[0]["id"], port=3000)

metrics = client.get_metrics()   # Prometheus text
health  = client.health_check()  # {"status": "ok"}

tunnel.close()
```

</details>

<details>
<summary><b>Node.js</b> — <code>npm install @tunr/cli</code></summary>

<br/>

```typescript
import { TunrClient } from '@tunr/cli'

const client = new TunrClient()

const tunnel = await client.share(3000)
console.log(tunnel.publicUrl)

const dbTunnel  = await client.tcp(5432)
const dnsTunnel = await client.udp(53)
const tlsTunnel = await client.tls(8443)

const appTunnel = await client.share(8080, {
  subdomain: 'myapp',
  password: 'demo123',
  allowIps: ['10.0.0.0/8'],
  freeze: true,
  injectWidget: true,
  proxy: 'http://proxy:8080',
  ttl: '2h',
})

tunnel.on('ready', () => console.log('Tunnel live'))
tunnel.on('error', (err) => console.error(err))
tunnel.on('exit',  () => console.log('Tunnel closed'))

const requests = await client.getRequests('myapp')
await client.replayRequest('myapp', requests[0].id, 3000)

await tunnel.close()
```

</details>

---

## Configuration (`.tunr.json`)

```bash
tunr config init
```

```json
{
  "$schema": "https://tunr.sh/schema/.tunr.schema.json",
  "port": 3000,
  "inspectorEnabled": true,
  "dashboardPort": 19842,
  "mcp": { "enabled": true }
}
```

---

## Architecture

```
Browser ──▶ relay.tunr.sh ──┬── [WebSocket] ──▶ tunr CLI ──▶ localhost:PORT   (tunnel)
                            │
                            └── control plane ──▶ tunr-runner ──▶ gVisor sandbox   (cloud app)
                                     │
                                Postgres + route cache
```

**Tunnels.** The CLI starts a local proxy with an embedded inspector and opens
one WebSocket to the relay. The relay issues a `*.tunr.sh` subdomain and
terminates HTTPS. HTTP, WebSocket, TCP, UDP and TLS all multiplex over that
single connection — a typed message discriminator routes them.

**Cloud.** `tunr deploy` uploads a tarball to the control plane, which hands it
to `tunr-runner`. The runner builds with Nixpacks and runs the result in a
gVisor sandbox with cgroup quotas. The relay routes the subdomain straight at
the container, waking it if it's asleep.

**Scale to zero.** Apps move `HOT → WARM → STOPPED` on an idle timer. WARM is a
cgroup memory reclaim followed by a pause, which cuts real memory cost by ~55%
while keeping wake latency around 150 ms. Health-check probes are answered at
the edge so a monitored app can still fall asleep.

**Observability.** Prometheus metrics at `/metrics`, K8s probes at `/healthz`
and `/readyz` on the inspector port.

**Self-hosting.** `docker-compose.yml` runs the tunnel stack (relay + Caddy +
Postgres); `docker-compose.runner.yml` adds the cloud runner. Point the CLI at
it with `--relay https://tunnel.yourcompany.com` or `TUNR_RELAY_URL`. See
[docs/SELF_HOSTING.md](docs/SELF_HOSTING.md), and [docs/SCALING.md](docs/SCALING.md)
for capacity planning.

---

## Security

- Auth tokens live in the **OS keychain**, never in a dotfile
- All relay traffic over **TLS 1.3**; `tunr tls` is end-to-end, the relay can't read it
- Cloud apps run under **gVisor**, not bare containers
- `.env` files are excluded from deploy uploads by default
- No telemetry, no analytics, no phone-home
- Supply chain: `go mod verify` + `govulncheck` in CI, cosign-signed checksums

Found a vulnerability? **Don't open a public issue** — see [SECURITY.md](SECURITY.md).

---

## Roadmap

Honest status. "Preview" means it works but the edges are sharp; "planned"
means there is no code yet.

| | Status |
|---|---|
| HTTP/WS, TCP, UDP, TLS tunnels | ✅ Stable |
| Multi-region relay (`ams`/`sea`/`sin`) | ✅ Stable |
| Demo features (freeze, demo, widget, auto-login) | ✅ Stable |
| Access control (password, token, IP, TTL) | ✅ Stable |
| Inspector + replay, Prometheus, service install | ✅ Stable |
| Python / Node SDKs | ✅ Stable |
| Self-hosted tunnel relay | ✅ Stable |
| `tunr deploy` + `tunr apps` + logs | 🚧 Preview |
| MCP cloud tools (`tunr_deploy`, `tunr_app_logs`, …) | 🚧 Preview |
| Scale-to-zero (sleep/wake) | 🚧 Preview |
| Self-hosted cloud runner | 🚧 Preview |
| **Role-based sharing** (viewer/commenter/editor, `--org acme.com`) | 📋 Planned |
| Per-app SQLite + snapshots | 📋 Planned |
| `tunr rollback` (code *and* data) | 📋 Planned |
| Persistent TCP/UDP ports | 📋 Backlog |
| GUI desktop app | 📋 Backlog |

Comparing tunr's tunnel against ngrok, Cloudflare Tunnel, LocalXpose and
localtunnel: [docs/compare-tunnels.md](docs/compare-tunnels.md).

---

## Contributing

Contributions are welcome — read [CONTRIBUTING.md](docs/CONTRIBUTING.md) first.

```bash
make check      # vet + lint + test + govulncheck
make pre-push   # the above, plus a build
```

The relay is a separate Go module and isn't covered by the Makefile:

```bash
cd relay && go build ./cmd/server && go test ./...
```

---

## License

This repository is **dual-licensed by directory**:

| Path | Licence | |
|---|---|---|
| `cmd/`, `internal/`, `sdk/` — the CLI and SDKs | [Apache-2.0](LICENSE) | OSI-approved open source. Use it, fork it, ship it commercially. |
| `relay/` — relay, control plane, runner | [PolyForm Shield 1.0.0](relay/LICENSE) | Source-available, **not** open source. Read it, modify it, run your own instance — but don't use it to compete with tunr. |

Running the whole stack yourself, for yourself or inside your company, is
allowed under both. See [NOTICE](NOTICE) for the exact boundary.

---

<div align="center">

**[tunr.sh](https://tunr.sh)** · [Docs](https://tunr.sh/docs) · [Discord](https://discord.gg/tunr) · [Twitter/X](https://x.com/vural_met)

Built with 💜 in Go

</div>
