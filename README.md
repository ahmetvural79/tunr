<div align="center">

<br/>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/logo-wordmark.svg" />
  <source media="(prefers-color-scheme: light)" srcset="assets/logo-wordmark.svg" />
  <img src="assets/logo-wordmark.svg" alt="tunr" width="340" />
</picture>

<br/><br/>

**Local → Public in < 3 seconds.**

[![CI](https://github.com/ahmetvural79/tunr/workflows/CI/badge.svg)](https://github.com/ahmetvural79/tunr/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/ahmetvural79/tunr)](https://goreportcard.com/report/github.com/ahmetvural79/tunr)
[![Release](https://img.shields.io/github/v/release/ahmetvural79/tunr?color=7c3aed)](https://github.com/ahmetvural79/tunr/releases)
[![License: PolyForm Shield](https://img.shields.io/badge/License-PolyForm%20Shield%201.0.0-7c3aed.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.22+-00add8)](go.mod)

[tunr.sh](https://tunr.sh) · [Docs](https://tunr.sh/docs) · [Dashboard](https://app.tunr.sh)

</div>

---

```bash
$ tunr share --port 3000

  🚀 Tunnel active:  https://abc1x2y3.tunr.sh

  Ctrl+C to stop...
```

## What is tunr?

**tunr** exposes your local development server to the internet in under 3 seconds — with automatic HTTPS and zero configuration. **Browser WebSockets** (e.g. Next.js / Vite HMR) are bridged over the same control channel as HTTP when you use the tunr relay + CLI; see [Troubleshooting](#troubleshooting) for Next.js `allowedDevOrigins` and edge cases.

It's a developer-first alternative to ngrok and Cloudflare Tunnel, built in Go as a single static binary that runs on macOS, Linux, and Windows (ARM64 included).

### What makes tunr different

| Feature | tunr | ngrok | Cloudflare Tunnel | Pinggy |
|---------|------|-------|-------------------|--------|
| Zero config | ✅ | ⚠️ | ⚠️ | ✅ |
| Automatic HTTPS | ✅ | ✅ | ✅ | ✅ |
| HTTPS / WebSocket + HMR tunnel | ✅ | ✅ | ✅ | ✅ |
| TCP tunnel | ✅ | ✅ | ✅ | ✅ |
| UDP / TLS tunnel | 🔜 | ❌ | ⚠️ | ✅ |
| Vibecoder Demo Mode | ✅ | ❌ | ❌ | ❌ |
| Freeze Mode | ✅ | ❌ | ❌ | ❌ |
| Feedback Widget Injection | ✅ | ❌ | ❌ | ❌ |
| Path Routing | ✅ | ❌ | ⚠️ | ❌ |
| Auto-Login Bypass | ✅ | ❌ | ❌ | ❌ |
| Auto-Expiring Tunnels (TTL) | ✅ | ❌ | ❌ | ❌ |
| MCP Integration | ✅ | ❌ | ❌ | ❌ |
| QR code tunnel sharing | ✅ | ❌ | ❌ | ✅ |
| Bearer Token / Key Auth | ✅ | ⚠️ | ❌ | ✅ |
| IP Whitelisting | ✅ | ❌ | ❌ | ✅ |
| Header Modification | ✅ | ❌ | ❌ | ✅ |
| Password / Basic Auth | ✅ | ✅ | ✅ (Zero Trust) | ✅ |
| Custom Subdomains | ✅ | ✅ | ❌ | ✅ |
| Custom / Wildcard Domains | ✅ | ⚠️ | ✅ | ✅ |
| Open Source CLI | ✅ | ❌ | ✅ | ❌ |
| Request Inspector / Replay | ✅ | ✅ | ❌ | ✅ |
| Multi-Region Relay | ✅ | ✅ | ✅ | ✅ |
| Python / Node.js SDKs | ✅ | ✅ | ❌ | ❌ |
| Single binary | ✅ | ✅ | ⚠️ | ✅ |

---

## Install

```bash
# macOS (Homebrew) — recommended
brew install ahmetvural79/tap/tunr

# Linux / macOS (one-liner)
curl -sSL https://tunr.sh/install | sh

# npm (Node.js projects)
npx tunr@latest share --port 3000

# Python SDK
pip install tunr

# Node.js SDK
npm install @tunr/cli

# Build from source
git clone https://github.com/ahmetvural79/tunr.git
cd tunr
go build -o tunr ./cmd/tunr
```

Requires **Go 1.22+** to build from source.

> **Free forever.** The CLI and all core features are open source. Cloud features (custom subdomains, team dashboards) require a [tunr.sh](https://tunr.sh) account.

---

## Quick Start

```bash
# 1. Start your dev server
npm run dev  # → http://localhost:3000

# 2. Share it
tunr share --port 3000

# That's it. You get:
#   🚀 https://abc1x2y3.tunr.sh
```

---

## Commands

```bash
# Share a local port (foreground)
tunr share --port 3000
tunr share --port 8080 --subdomain myapp  # custom subdomain (Pro)

# Route paths to different ports
tunr share --route /=3000 --route /api=8080

# Password protection & expiration
tunr share -p 8080 --password "secret" --ttl 30m

# Vibecoder demo superpowers
tunr share -p 3000 --demo --freeze --inject-widget
tunr share -p 3000 --auto-login "Cookie: session=demo"

# Secure & debug (Pinggy-powered)
tunr share -p 3000 --qr                     # QR code for mobile scanning
tunr share -p 3000 --auth-token "my-secret" # Bearer token access control
tunr share -p 3000 --allow-ip "1.2.3.0/24"  # IP whitelist (CIDR)
tunr share -p 3000 --header-add "X-Debug: 1"
tunr share -p 3000 --x-forwarded-for --original-url
tunr share -p 3000 --cors-origin "https://myapp.com"

# Custom domain
tunr share -p 3000 --domain demo.client.com

# Machine-readable output for CI/CD
tunr share -p 3000 --json

# Daemon mode (runs in background)
tunr start --port 3000
tunr stop
tunr status

# Inspect & debug
tunr open           # Open HTTP inspector dashboard
tunr logs           # Stream request logs
tunr logs --follow  # Real-time log stream
tunr replay <id>    # Re-send a captured request

# System
tunr doctor         # System health check
tunr version
tunr update         # Self-update to latest release
tunr uninstall      # Remove tunr from your system

# Auth
tunr login
tunr logout

# Config
tunr config show
tunr config init    # Creates .tunr.json in cwd

# AI / MCP
tunr mcp            # Start MCP server (Claude, Cursor, Windsurf)

# TCP tunnels
tunr tcp --port 5432
tunr tcp --port 22 --qr
tunr tcp --port 6379 --allow-ip 10.0.0.0/8 --region ams
```

### Full CLI Reference

| Command | Description |
|---------|-------------|
| `tunr share -p PORT` | Expose local port with HTTPS URL |
| `tunr share -p PORT -s NAME` | Custom subdomain (Pro) |
| `tunr share --route /PATH=PORT` | Map specific URL paths to local ports |
| `tunr share -p PORT --password "PASS"` | Enable Basic Authentication |
| `tunr share -p PORT --ttl 1h` | Auto-close tunnel after duration |
| `tunr share -p PORT --demo` | Read-only demo mode |
| `tunr share -p PORT --freeze` | Freeze mode (cache-on-crash) |
| `tunr share -p PORT --inject-widget` | Inject feedback widget into HTML |
| `tunr share -p PORT --auto-login "Cookie: s=demo"` | Auto-inject auth cookie |
| `tunr share -p PORT --domain HOST` | Use custom domain |
| `tunr share -p PORT --json` | JSON output (CI/CD, scripting) |
| `tunr share -p PORT --qr` | Display QR code for the tunnel URL |
| `tunr share -p PORT --auth-token TOKEN` | Bearer token / API key protection |
| `tunr share -p PORT --allow-ip CIDR` | IP whitelist (CIDR notation) |
| `tunr share -p PORT --header-add "H: V"` | Add headers to forwarded requests |
| `tunr share -p PORT --header-replace "H: V"` | Replace headers before forwarding |
| `tunr share -p PORT --header-remove H` | Remove headers before forwarding |
| `tunr share -p PORT --x-forwarded-for` | Inject X-Forwarded-For with client IP |
| `tunr share -p PORT --original-url` | Inject X-Original-URL with public URL |
| `tunr share -p PORT --cors-origin ORIGIN` | CORS preflight allowed origins |
| `tunr start -p PORT` | Background daemon mode |
| `tunr stop` | Stop daemon |
| `tunr status` | Show active tunnels |
| `tunr logs` | Stream HTTP request logs |
| `tunr open` | Open inspector dashboard |
| `tunr replay <id>` | Replay captured request |
| `tunr doctor` | Diagnose issues |
| `tunr login` | Authenticate (browser-based OAuth) |
| `tunr update` | Self-update CLI binary |
| `tunr uninstall` | Remove tunr from system |
| `tunr mcp` | Start MCP server |
| `tunr config init` | Create `.tunr.json` |
| `tunr tcp -p PORT` | Expose local port via TCP tunnel |
| `tunr tcp -p PORT --qr` | TCP tunnel with QR code |
| `tunr tcp -p PORT --region REGION` | TCP tunnel in specific region (ams, sea, sin) |
| `tunr share -p PORT --region REGION` | HTTP tunnel in specific region |

---

## Troubleshooting

### Next.js: blank page over `tunr share` (port 3000)

Next.js **dev** blocks cross-origin access to dev-only endpoints unless you allow your tunnel host.

1. Add **`allowedDevOrigins`** in `next.config.js` / `next.config.ts` (see [Next.js docs — allowedDevOrigins](https://nextjs.org/docs/app/api-reference/config/next-config-js/allowedDevOrigins)):

```js
/** @type {import('next').NextConfig} */
const nextConfig = {
  allowedDevOrigins: ['*.tunr.sh', 'tunr.sh'],
}
module.exports = nextConfig
```

Use your real tunnel domain pattern if you use a custom subdomain or self-hosted edge.

2. For a **stable** public demo without HMR, prefer a production build:

```bash
npm run build && npm run start
tunr share --port 3000
```

### “Chrome offline” / “This site can’t be reached” / dinosaur page when using `--inject-widget`

That page is the browser’s **network error** UI — the **main HTML document** never completed successfully (not the widget script failing in isolation).



### WebSocket / HMR over the public URL

The tunr **edge relay** upgrades the public `wss://` connection and streams frames to your **CLI**, which opens a local `ws://` connection to your dev server. That gives you end-to-end HMR-style WebSockets without a separate tunnel product.

**Still required for some frameworks:** Next.js dev server may block cross-origin requests until you add your tunnel host to `allowedDevOrigins` in `next.config` (see above). If HMR still fails, fall back to **`next build && next start`** or test HMR on **localhost**.

**Relay / edge:** WebSocket bridging is implemented on the tunr relay; self-hosted edges must run a relay build that includes this feature.

Optional: for relay **origin checks** on the browser WebSocket handshake, set `TUNR_WS_EXTRA_ALLOWED_ORIGIN_SUFFIXES` (comma-separated hostname suffixes).

---

## Vibecoder Demo Features

tunr ships with four proxy-level superpowers designed for freelancers and agencies demoing to clients:

### ❄️ Freeze Mode (`--freeze`)

If your local server crashes mid-demo, tunr serves the last successful response from memory. Your client never sees a broken page.

```bash
tunr share --port 3000 --freeze
```

### 🛡️ Read-Only Demo Mode (`--demo`)

Intercept destructive HTTP methods (`POST`, `PUT`, `DELETE`) at the proxy layer. The client can click "Place Order" — nothing actually writes to your database.

```bash
tunr share --port 3000 --demo
```

### 💬 Feedback Widget Injection (`--inject-widget`)

Injects a transparent overlay widget into every HTML page served through the tunnel. Clients can pin visual feedback and errors are forwarded to your terminal in real-time. Like Marker.io, but free and built-in.

```bash
tunr share --port 3000 --inject-widget
```

### 🔑 Auto-Login Bypass (`--auto-login`)

Inject an auth cookie so your client lands on the demo account automatically — no signup, no email verification.

```bash
tunr share --port 3000 --auto-login "Cookie: session=demo-token"
```

Combine them all for the ultimate demo setup:

```bash
tunr share --port 3000 --demo --freeze --inject-widget
```

---

## Advanced Tunnel Features

### 🔒 Password Protected Tunnels (`--password`)

Add Basic Authentication to your public URL instantly without writing any code. Keep your development environments secure from unauthorized access while sharing with clients or third parties.

```bash
tunr share -p 8080 --password "secret"
# Or provide a specific username
tunr share -p 8080 --password "client:secret"
```

### ⏳ Auto-Expiring Tunnels (`--ttl`)

Forget to stop a tunnel exposing your local machine? Use a Time-To-Live (TTL). Once the duration expires, the tunnel daemon safely terminates the connection and shuts down the proxy.

```bash
tunr share -p 3000 --ttl 1h30m
```

### 🔀 Path Routing (`--route`)

Map different incoming URL paths to different upstream ports on your machine. This is perfect for testing microservices or serving your frontend and API from a single public proxy domain.

```bash
# Anything to / goes to 3000, /api goes to 8080
tunr share --route /=3000 --route /api=8080
```

### 🌐 Multi-Region Routing (`--region`)

Select a preferred relay region for lower latency to specific geographic areas.

```bash
# European relay (Amsterdam)
tunr share --port 3000 --region ams

# US West relay (Seattle)
tunr share --port 3000 --region sea

# Asia relay (Singapore)
tunr share --port 3000 --region sin

# TCP tunnel with region selection
tunr tcp --port 5432 --region ams
```

Currently available regions:
- `ams` — Amsterdam, EU (Europe)
- `sea` — Seattle, US West (Americas)
- `sin` — Singapore (Asia-Pacific)

### 🔌 TCP Tunnels (`tunr tcp`)

Expose raw TCP services — databases, SSH, Redis, game servers — through secure tunnels without HTTP overhead.

```bash
# PostgreSQL
tunr tcp --port 5432

# SSH with QR code for mobile sharing
tunr tcp --port 22 --qr

# Redis with IP restriction
tunr tcp --port 6379 --allow-ip 10.0.0.0/8

# MySQL in specific region
tunr tcp --port 3306 --region ams
```

TCP tunnels forward raw bytes over the same WebSocket control channel — no HTTP parsing on the relay side. Perfect for any TCP-based service.

---

## Programming APIs

### Python SDK

```bash
pip install tunr
```

```python
from tunr import TunrClient, TunnelOptions

client = TunrClient()

# Simple tunnel
tunnel = client.share(port=3000)
print(tunnel.public_url)

# With options
opts = TunnelOptions(
    subdomain="myapp",
    password="demo123",
    allow_ips=["10.0.0.0/8"],
)
tunnel = client.share(port=8080, opts=opts)

# Inspect requests
requests = client.get_requests(tunnel.id)

# Replay a request
client.replay_request(requests[0].id)

# Clean up
tunnel.close()
```

### Node.js SDK

```bash
npm install @tunr/cli
```

```typescript
import { TunrClient } from '@tunr/cli'

const client = new TunrClient()

// Simple tunnel
const tunnel = await client.share(3000)
console.log(tunnel.publicUrl)

// With options
const tunnel = await client.share(8080, {
  subdomain: 'myapp',
  password: 'demo123',
  allowIps: ['10.0.0.0/8'],
})

// Event-based lifecycle
tunnel.on('ready', () => console.log('Tunnel live'))
tunnel.on('error', (err) => console.error(err))
tunnel.on('exit', () => console.log('Tunnel closed'))

// Inspect & replay
const requests = await client.getRequests(tunnel.id)
await client.replayRequest(requests[0].id)

// Clean up
await tunnel.close()
```

---

## Security & Debugging (Pinggy-Inspired)

tunr now includes all the enterprise-grade tunnel security and debugging features from Pinggy, built natively:

### 📱 QR Code Tunnel Sharing (`--qr`)

Instantly generate a scannable QR code for your tunnel URL. Perfect for mobile testing and sharing URLs with clients.

```bash
tunr share -p 3000 --qr
```

### 🔑 Bearer Token Access (`--auth-token`)

Protect your tunnel with a simple API key/token. Requests must include `Authorization: Bearer <token>` or pass `?token=<token>` in the query string.

```bash
tunr share -p 3000 --auth-token "my-super-secret-key"
```

### 🛡️ IP Whitelisting (`--allow-ip`)

Restrict tunnel access to specific IP ranges using CIDR notation. Only whitelisted IPs can reach your local server.

```bash
# Only allow your office network
tunr share -p 3000 --allow-ip "203.0.113.0/24"

# Multiple networks
tunr share -p 3000 --allow-ip "10.0.0.0/8,172.16.0.0/12"
```

### 🔧 Live Header Modification

Add, replace, or remove HTTP headers on the fly before they reach your local server.

```bash
# Inject a debug header
tunr share -p 3000 --header-add "X-Debug: true"

# Replace the Host header for internal routing
tunr share -p 3000 --header-replace "Host: internal.local"

# Remove fingerprinting headers
tunr share -p 3000 --header-remove "X-Powered-By"
```

### 🌐 Forwarded Headers (`--x-forwarded-for`, `--original-url`)

Inject standard proxy headers so your application knows the original client IP and URL.

```bash
tunr share -p 3000 --x-forwarded-for --original-url
```

- `X-Forwarded-For` — the real client IP address
- `X-Original-URL` — the full public tunnel URL that was requested

### 🔓 CORS Preflight (`--cors-origin`)

Allow browser CORS preflight requests from specific origins without server-side changes.

```bash
tunr share -p 3000 --cors-origin "https://myapp.com"
```


## HTTP Inspector

tunr ships with a built-in HTTP request inspector (like ngrok's web UI, but local).

```bash
tunr open  # opens http://localhost:19842
```

Features:
- Live request/response stream
- Headers, body, timing
- One-click replay
- Export as curl command

---

## MCP Integration (Claude, Cursor, Windsurf)

tunr implements the **Model Context Protocol** — AI agents can manage tunnels directly.

**Claude Desktop** (`~/.claude/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "tunr": {
      "command": "tunr",
      "args": ["mcp"]
    }
  }
}
```

**Cursor** (`.cursor/mcp.json`):
```json
{
  "mcpServers": {
    "tunr": { "command": "tunr", "args": ["mcp"] }
  }
}
```

---

## Configuration (`.tunr.json`)

Create a workspace config file:

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

tunr is a single Go binary that:

1. Starts a local HTTPS proxy with an embedded inspector
2. Opens a WebSocket connection to the **tunr relay** (edge server)
3. The relay issues a `*.tunr.sh` subdomain and forwards traffic
4. HTTPS terminates at the relay; CLI ↔ dev-server traffic runs over the same WebSocket stream

```
Browser → relay.tunr.sh → [WebSocket] → tunr binary → localhost:PORT
```

**Protocol support:** Currently tunr tunnels **HTTP/HTTPS + WebSocket** traffic. The relay architecture is designed to support TCP, UDP, and TLS tunnels in the future (`🔜` in the comparison table above).

**Multi-region:** The relay supports region selection via the `--region` flag. Currently available regions: `ams` (Amsterdam, EU), `sea` (Seattle, US West), `sin` (Singapore, Asia). The balancer infrastructure (`relay/internal/relay/balancer.go`) manages cross-region routing metadata.

**Wildcards:** The relay is configured with `*.tunr.sh` wildcard routing through Fly.io / Caddy; wildcard domain support for custom domains is on the roadmap.

The relay server is proprietary and not part of this repository. You can self-host the CLI against your own relay by overriding the relay URL.

---

## Security

tunr takes security seriously for an open-source CLI tool:

- Auth tokens stored in **OS keychain** (not plaintext files)
- All relay traffic over **TLS 1.3**
- No telemetry, no analytics, no phone-home by default
- Supply chain integrity via `go mod verify` and govulncheck in CI

Found a vulnerability? **Do not open a public issue.** See [SECURITY.md](SECURITY.md).

---

## How tunr Compares

### tunr vs ngrok

Both tools share localhost, but tunr focuses on developer experience and vibecoding workflows:

| | tunr | ngrok (Personal) |
|--|------|------------------|
| Monthly Price | 💸 Free / affordable | 💸 $10/month |
| Bandwidth | 📦 Unlimited | 📦 5 GB/month cap |
| Vibecoder Demo Features | ❄️🛡️💬✅ Exclusive | ❌ |
| IP Whitelisting | ✅ | ❌ (Enterprise only) |
| Bearer Token Auth | ✅ | ❌ |
| Header Modification | ✅ | ❌ |
| QR Code Tunnel Sharing | ✅ | ❌ |
| MCP / AI Integration | ✅ | ❌ |
| Open Source CLI | ✅ | ❌ |

[Compare Pinggy vs ngrok](https://pinggy.io/compare/pinggy-vs-ngrok/)

### tunr vs Cloudflare Tunnel

| | tunr | Cloudflare Tunnel |
|--|------|-------------------|
| Setup complexity | ⚡ 1 command (`tunr share -p 3000`) | ⚠️ Requires Cloudflare account + DNS config |
| Persistent subdomains | ✅ (tunr.sh managed) | ❌ Must own a domain first |
| Vibecoder Demo Features | ✅ Exclusive | ❌ |
| Request Inspection | ✅ Live inspector + replay | ❌ |
| Bandwidth limits | 📦 Unlimited | ⚠️ 100 MB max upload |
| IP Whitelisting | ✅ CLI-level (no dashboard) | ❌ |
| Local dashboard | ✅ Built-in | ❌ |

[Compare Pinggy vs Cloudflare Tunnel](https://pinggy.io/compare/pinggy-vs-cloudflare-tunnel/)

### tunr vs LocalXpose

| | tunr | LocalXpose (Pro) |
|--|------|------------------|
| Monthly Price | 💸 Free / affordable | 💸 $8/month |
| Bearer Token Auth | ✅ | ❌ |
| MCP Integration | ✅ | ❌ |
| Vibecoder Demo Features | ✅ Exclusive | ❌ |
| Header Modification | ✅ | ❌ |
| Open Source | ✅ | ❌ |

[Compare Pinggy vs LocalXpose](https://pinggy.io/compare/pinggy-vs-localxpose/)

### tunr vs LocalTunnel

LocalTunnel is free but minimal — tunr adds a full feature set on top of the same zero-cost model:

| | tunr | LocalTunnel |
|--|------|-------------|
| HTTPS tunnel | ✅ | ✅ |
| WebSocket / HMR | ✅ | ❌ |
| Custom domains | ✅ | ❌ |
| Persistent subdomains | ✅ | ❌ |
| IP Whitelisting | ✅ | ❌ |
| Bearer Token Auth | ✅ | ❌ |
| Request Inspector | ✅ | ❌ |
| Password Protection | ✅ | ❌ |
| Demo / Freeze / Widget | ✅ Exclusive | ❌ |

[Compare Pinggy vs LocalTunnel](https://pinggy.io/compare/pinggy-vs-localtunnel/)

| Feature | Status | Notes |
|---------|--------|-------|
| TCP tunnel support | ✅ Released | Database, SSH, game server tunnels |
| Python / Node.js SDKs | ✅ Released | Programmatic tunnel creation via `pip install tunr` / `npm i @tunr/cli` |
| Multi-region relay | ✅ Released | `--region` flag with `ams`, `sea`, `sin` regions |
| UDP tunnel support | 📋 Backlog | Real-time apps, DNS, game servers |
| TLS tunnel (E2E encryption) | 📋 Backlog | End-to-end TLS without relay decryption |
| Wildcard custom domains | 🔜 Planned | `*.yourdomain.com` routing |
| GUI desktop app | 📋 Backlog | Windows, macOS, Linux |
| Webhook verification | 📋 Backlog | Signature validation for incoming webhooks |
| Team collaboration | 📋 Backlog | Shared tunnels, member management |
| Remote device management | 📋 Backlog | Manage tunnels on IoT / remote machines |
| Persistent TCP/UDP ports | 📋 Backlog | Fixed-port tunnel endpoints |
| Automatic Let's Encrypt certs | 📋 Backlog | Per-tunnel TLS certificate provisioning |

---

Contributions are welcome! Please read [CONTRIBUTING.md](docs/CONTRIBUTING.md) first.

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Make your changes
4. Ensure CI passes (`go test ./...` + `golangci-lint run`)
5. Open a pull request

---

## License

PolyForm Shield 1.0.0 — see [LICENSE](LICENSE).

You are free to use, modify, and distribute this software. The only restriction is that you may not use it to build a competing product or service. See the license for full terms.

---

<div align="center">

**[tunr.sh](https://tunr.sh)** · [Docs](https://tunr.sh/docs) · [Discord](https://discord.gg/tunr) · [Twitter/X](https://x.com/vural_met)

Built with 💜 in Go

</div>
