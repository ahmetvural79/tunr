# Tunr v0.4.0 — Launch, Deploy & Install Walkthrough

Complete guide for releasing, deploying, and installing tunr v0.4.0 with all new features.

---

## Table of Contents

1. [What's New in v0.4.0](#whats-new)
2. [GitHub Release & CI](#github-release)
3. [Install via CLI (curl)](#install-cli)
4. [Install via Homebrew](#install-homebrew)
5. [Install via Docker](#install-docker)
6. [Install via pip (Python SDK)](#install-pip)
7. [Install via npm (Node.js SDK)](#install-npm)
8. [Self-Hosting the Relay](#self-hosting)
9. [System Service Installation](#service-install)
10. [Quick Start Examples](#quick-start)
11. [Verify Installation](#verify)

---

## 1. What's New in v0.4.0 <a name="whats-new"></a>

| Feature | Command | Description |
|---------|---------|-------------|
| **UDP Tunnels** | `tunr udp --port 53` | DNS, game servers, real-time apps |
| **TLS E2E Encryption** | `tunr tls --port 8443` | Zero-trust, relay can't read traffic |
| **Multi-Tunnel Config** | `tunr up` | Start all tunnels from `.tunr.json` |
| **Docker Image** | `docker run tunr` | ~15MB Alpine container |
| **Self-Hosting** | `docker compose up -d` | Full relay + caddy + postgres stack |
| **Service Install** | `tunr service install` | Auto-start on boot (systemd/launchd) |
| **Prometheus Metrics** | `curl :19842/metrics` | Grafana-ready observability |
| **Health Probes** | `/healthz`, `/readyz` | Kubernetes integration |
| **Corporate Proxy** | `--proxy http://proxy:8080` | Firewall traversal |
| **Expanded SDKs** | `client.tcp()`, `.udp()`, `.tls()` | Python & Node.js |

---

## 2. GitHub Release & CI <a name="github-release"></a>

### Create and push the release tag

```bash
# Make sure all changes are committed
cd tunr
git add -A
git commit -m "feat: v0.4.0 — UDP/TLS tunnels, Docker, multi-tunnel, metrics"

# Tag the release
git tag -a v0.4.0 -m "v0.4.0 — UDP/TLS tunnels, Docker, self-hosting, multi-tunnel config, Prometheus metrics, service install, proxy support"

# Push to GitHub
git push origin main
git push origin v0.4.0
```

### What happens automatically

1. **CI Pipeline** (`ci.yml`) runs: lint → test (Linux/macOS/Windows) → security audit → cross-platform build
2. **Release Pipeline** (`release.yml`) triggers on `v*` tags:
   - GoReleaser builds CLI + Relay for all platforms
   - Creates GitHub Release with checksums + signatures (cosign)
   - Updates Homebrew tap
   - Generates build attestation

### Manual release notes template

```markdown
## tunr v0.4.0

### 🎮 UDP Tunnels
Expose DNS servers, game servers, and real-time services.
`tunr udp --port 53 --region ams`

### 🔐 TLS End-to-End Encryption (Zero-Trust)
Relay cannot read your traffic. SNI-based routing.
`tunr tls --port 8443`

### 🚀 Multi-Tunnel Config
Define all tunnels in `.tunr.json`, start with one command.
`tunr up`

### 🐳 Docker & Self-Hosting
Run from Docker or self-host the full stack.
`docker compose up -d`

### 📊 Prometheus Metrics & Health Probes
K8s-ready `/metrics`, `/healthz`, `/readyz` endpoints.

### ⚡ Service Install
Auto-start on boot with systemd/launchd.
`tunr service install --port 3000`

### 🌐 Corporate Proxy Support
`tunr share -p 3000 --proxy http://proxy:8080`

### SDK Updates
Python & Node.js SDKs now support `tcp()`, `udp()`, `tls()` methods.
```

---

## 3. Install via CLI (curl) <a name="install-cli"></a>

The fastest way to install on macOS, Linux, or Windows (WSL):

```bash
curl -sL https://tunr.sh/install.sh | sh
```

### Manual install (specific version)

```bash
# macOS (Apple Silicon)
curl -L https://github.com/ahmetvural79/tunr/releases/download/v0.4.0/tunr_darwin_arm64.tar.gz | tar xz
sudo mv tunr /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/ahmetvural79/tunr/releases/download/v0.4.0/tunr_darwin_amd64.tar.gz | tar xz
sudo mv tunr /usr/local/bin/

# Linux (amd64)
curl -L https://github.com/ahmetvural79/tunr/releases/download/v0.4.0/tunr_linux_amd64.tar.gz | tar xz
sudo mv tunr /usr/local/bin/

# Linux (ARM64 / Raspberry Pi)
curl -L https://github.com/ahmetvural79/tunr/releases/download/v0.4.0/tunr_linux_arm64.tar.gz | tar xz
sudo mv tunr /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/ahmetvural79/tunr/releases/download/v0.4.0/tunr_windows_amd64.zip" -OutFile tunr.zip
Expand-Archive tunr.zip -DestinationPath .
Move-Item tunr.exe C:\Windows\System32\
```

### Verify

```bash
tunr --version
# tunr version 0.4.0
```

---

## 4. Install via Homebrew <a name="install-homebrew"></a>

```bash
# Install
brew install ahmetvural79/tap/tunr

# Or update existing
brew upgrade tunr

# Verify
tunr --version
```

---

## 5. Install via Docker <a name="install-docker"></a>

```bash
# Pull the image
docker pull ghcr.io/ahmetvural79/tunr:v0.4.0

# Or build locally
cd tunr
docker build -t tunr .

# Run a tunnel
docker run --rm -it tunr share --port 3000

# With network access to host services
docker run --rm -it --network host tunr share --port 3000
```

### Docker Compose (for projects)

```yaml
# Add to your project's docker-compose.yml
services:
  tunnel:
    image: ghcr.io/ahmetvural79/tunr:v0.4.0
    network_mode: host
    command: share --port 3000 --subdomain myapp
    restart: unless-stopped
```

---

## 6. Install via pip (Python SDK) <a name="install-pip"></a>

### Install

```bash
pip install tunr
# or
pip install tunr==0.4.0
```

### Prerequisites

- Python 3.10+
- `tunr` CLI must be installed (the SDK calls it as a subprocess)

### Quick start

```python
from tunr import TunrClient, TunnelOptions

client = TunrClient()

# HTTP tunnel
tunnel = client.share(port=3000)
print(f"Public URL: {tunnel.public_url}")

# TCP tunnel (database)
db_tunnel = client.tcp(port=5432)

# UDP tunnel (DNS)
dns_tunnel = client.udp(port=53)

# TLS tunnel (end-to-end encrypted)
tls_tunnel = client.tls(port=8443)

# With options
opts = TunnelOptions(
    subdomain="myapp",
    password="demo123",
    freeze=True,
    inject_widget=True,
    region="ams",
    proxy="http://proxy:8080",
    ttl="2h",
)
tunnel = client.share(port=8080, opts=opts)

# Inspect requests
requests = client.get_requests("myapp")
for req in requests:
    print(f"{req['method']} {req['path']} → {req['status_code']}")

# Observability
metrics = client.get_metrics()      # Prometheus text format
health = client.health_check()      # {"status": "ok", ...}

# Cleanup
tunnel.close()
client.close()
```

### Publish new version to PyPI

```bash
cd sdk/python
# Update version in __init__.py
python -m build
python -m twine upload dist/*
```

---

## 7. Install via npm (Node.js SDK) <a name="install-npm"></a>

### Install

```bash
npm install @tunr/cli
# or
yarn add @tunr/cli
```

### Prerequisites

- Node.js 18+
- `tunr` CLI must be installed

### Quick start

```typescript
import { TunrClient } from '@tunr/cli'

const client = new TunrClient()

// HTTP tunnel
const tunnel = await client.share(3000)
console.log(`Public URL: ${tunnel.publicUrl}`)

// TCP tunnel (database)
const dbTunnel = await client.tcp(5432)

// UDP tunnel (DNS)
const dnsTunnel = await client.udp(53)

// TLS tunnel (end-to-end encrypted)
const tlsTunnel = await client.tls(8443)

// With options
const appTunnel = await client.share(8080, {
  subdomain: 'myapp',
  password: 'demo123',
  freeze: true,
  injectWidget: true,
  region: 'ams',
  proxy: 'http://proxy:8080',
  ttl: '2h',
})

// Event-based lifecycle
tunnel.on('close', () => console.log('Tunnel closed'))

// Inspect requests
const requests = await client.getRequests('myapp')
for (const req of requests) {
  console.log(`${req.method} ${req.path} → ${req.status_code}`)
}

// Observability
const metrics = await client.getMetrics()    // Prometheus text
const health = await client.healthCheck()    // {status: "ok"}

// Cleanup
await tunnel.close()
```

### Publish new version to npm

```bash
cd sdk/node
# Update version in package.json
npm run build
npm publish --access public
```

---

## 8. Self-Hosting the Relay <a name="self-hosting"></a>

Run your own tunr relay on any VPS (Hetzner, DigitalOcean, AWS):

### Prerequisites

- A server with a public IP
- A domain with DNS access
- Docker + Docker Compose installed
- Ports 80 & 443 open

### Setup

```bash
# 1. Clone
git clone https://github.com/ahmetvural79/tunr.git
cd tunr

# 2. Configure DNS (wildcard A record)
# *.tunnel.yourcompany.com → your-server-ip
# tunnel.yourcompany.com   → your-server-ip

# 3. Set environment
export TUNR_DOMAIN=tunnel.yourcompany.com
export TUNR_JWT_SECRET=$(openssl rand -hex 32)

# 4. Start the stack
docker compose up -d

# 5. Connect from your machine
tunr share --port 3000 --relay https://tunnel.yourcompany.com
```

### Persistent config

```bash
export TUNR_RELAY_URL=https://tunnel.yourcompany.com
tunr share --port 3000
```

See [docs/SELF_HOSTING.md](./SELF_HOSTING.md) for the complete guide.

---

## 9. System Service Installation <a name="service-install"></a>

Auto-start tunr on boot:

### Install

```bash
# Linux (systemd)
sudo tunr service install --port 3000 --subdomain myapp

# macOS (launchd)
tunr service install --port 3000 --subdomain myapp
```

### Manage

```bash
# Check status
tunr service status

# View logs (Linux)
sudo journalctl -u tunr -f

# View logs (macOS)
tail -f ~/Library/Logs/tunr.log

# Uninstall
tunr service uninstall   # Linux: sudo
```

---

## 10. Quick Start Examples <a name="quick-start"></a>

### Basic HTTP tunnel

```bash
tunr share --port 3000
```

### Client demo (read-only + crash protection + feedback widget)

```bash
tunr share -p 3000 --demo --freeze --inject-widget
```

### TCP tunnel (PostgreSQL)

```bash
tunr tcp --port 5432 --region ams --qr
```

### UDP tunnel (DNS server)

```bash
tunr udp --port 53 --region ams
```

### TLS tunnel (end-to-end encrypted)

```bash
tunr tls --port 8443
```

### Multi-tunnel from config

Create `.tunr.json`:

```json
{
  "tunnels": {
    "frontend": { "port": 3000, "subdomain": "app" },
    "api":      { "port": 8080, "subdomain": "api", "password": "secret" },
    "db":       { "port": 5432, "protocol": "tcp" }
  }
}
```

```bash
tunr up
```

### Behind corporate proxy

```bash
tunr share -p 3000 --proxy http://corporate-proxy:8080
# or set env vars
export HTTPS_PROXY=http://corporate-proxy:8080
tunr share -p 3000
```

### Prometheus monitoring

```bash
# Start a tunnel
tunr share -p 3000

# In another terminal
curl http://localhost:19842/metrics    # Prometheus format
curl http://localhost:19842/healthz    # Health check
curl http://localhost:19842/readyz     # Readiness check
```

---

## 11. Verify Installation <a name="verify"></a>

```bash
# Version check
tunr --version

# Connectivity check
tunr doctor

# Quick share test (start a local server first)
python3 -m http.server 8080 &
tunr share --port 8080 --ttl 1m

# Check all new commands
tunr udp --help
tunr tls --help
tunr up --help
tunr service --help
```

### Expected output

```
tunr version 0.4.0

Available Commands:
  share       Expose a local port with a public HTTPS URL
  tcp         Expose a local TCP port over the internet
  udp         Expose a local UDP port over the internet
  tls         Expose a local TLS port with end-to-end encryption
  up          Start all tunnels defined in .tunr.json
  down        Stop all running daemon tunnels
  service     Manage tunr as a system service
  start       Start tunnel as a background daemon
  stop        Stop daemon tunnel
  status      Show active tunnels
  logs        Stream live request logs
  doctor      Diagnose setup or proxy issues
  login       Authenticate with tunr.sh
  logout      Remove stored credentials
  open        Open the HTTP inspector dashboard
  replay      Replay a captured HTTP request
  mcp         Start AI Model Context Protocol server
  config      Manage tunr configuration
  update      Self-update to latest version
  uninstall   Remove tunr from your system
```

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `command not found: tunr` | Re-run `curl -sL https://tunr.sh/install.sh \| sh` or check `$PATH` |
| `tunnel URL not found within 10s` | Check internet, try `tunr doctor`, or use `--proxy` |
| `port requires root privileges` | Use port ≥ 1024 |
| `Pro subscription required` | Free plan: random subdomains only. Upgrade at https://app.tunr.sh |
| `pip install tunr` fails | Ensure Python 3.10+ and `pip install --upgrade pip` |
| Docker `network unreachable` | Use `--network host` flag |
| Service won't start | Linux: check `sudo journalctl -u tunr`, macOS: check `~/Library/Logs/tunr.log` |
