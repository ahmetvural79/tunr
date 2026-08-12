# Self-Hosting tunr

This guide explains how to run your own tunr relay server. This is useful for:
- Organizations that need full data sovereignty
- Air-gapped environments
- Custom domain routing without the tunr.sh cloud

## Prerequisites

- A server with a public IP (Hetzner, DigitalOcean, AWS, etc.)
- A domain with DNS access (e.g., `tunnel.yourcompany.com`)
- Docker and Docker Compose installed
- Ports 80 and 443 open

## Quick Start

### 1. Clone the repository
```bash
git clone https://github.com/ahmetvural79/tunr.git
cd tunr
```

### 2. Configure DNS

Add a wildcard A record pointing to your server:

```
*.tunnel.yourcompany.com  A  <your-server-ip>
tunnel.yourcompany.com    A  <your-server-ip>
```

### 3. Set environment variables

```bash
cp .env.example .env
# Edit .env with your values:
#   TUNR_DOMAIN=tunnel.yourcompany.com
#   TUNR_JWT_SECRET=<random-64-char-string>
```

### 4. Start the stack

```bash
docker compose up -d
```

This starts:
- **Caddy** — TLS termination with automatic Let's Encrypt certificates
- **Relay** — WebSocket relay server for tunnel traffic
- **PostgreSQL** — Database for user accounts, tunnel history, audit logs

### 5. Connect a client

```bash
tunr share --port 3000 --relay https://tunnel.yourcompany.com
```

Or set it permanently:
```bash
export TUNR_RELAY_URL=https://tunnel.yourcompany.com
tunr share --port 3000
```

## Architecture

```
Internet
  │
  ▼
┌──────────────────────┐
│  Caddy (TLS/HTTPS)   │ ← Wildcard cert: *.tunnel.yourcompany.com
│  :80, :443           │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│  tunr Relay Server   │ ← WebSocket hub, subdomain routing
│  :8080 (internal)    │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│  PostgreSQL          │ ← Users, tunnels, audit logs
│  :5432 (internal)    │
└──────────────────────┘
```

## Adding the cloud layer (`tunr deploy`)

Everything above self-hosts **tunnels**. To also run `tunr deploy` — building
and hosting apps on your own box — you need the runner sidecar, which drives
Docker directly.

> **Preview.** This path is newer than the tunnel stack and assumes a Linux
> host you control. It does not work on Docker Desktop for macOS: the density
> levers write to the host cgroup hierarchy.

### 1. Host prerequisites

```bash
sudo ./scripts/host-density.sh     # zram + cgroup v2 soft limits
docker network create --opt com.docker.network.bridge.enable_icc=false tunr-apps
```

`host-density.sh` is not optional. Without zram, `memory.reclaim` has nowhere
to evict to and `memory.high` containment fails outright — an app that should
have been throttled triggers a system-wide OOM instead. Install gVisor
(`runsc`) too, or set the runner to `runc` and accept weaker isolation.

### 2. Start the runner

```bash
docker compose -f docker-compose.runner.yml up -d
```

The runner needs `/sys/fs/cgroup` mounted **rw**, `/proc` with `pid: host`, and
the Docker socket. Without those every density lever silently no-ops — check
for `cgroup levers OFF` in its startup log.

### 3. Point the relay at it

```bash
# .env
RUNNER_URL=http://tunr-runner:9091
RUNNER_SECRET=<shared secret, also set on the runner>
```

Restart the relay, then:

```bash
tunr login --relay https://tunnel.yourcompany.com
tunr deploy --name my-app --relay https://tunnel.yourcompany.com
```

See [SCALING.md](SCALING.md) for capacity thresholds and when to split the
builder onto its own machine.

## Systemd (without Docker)

If you prefer running without containers:

```ini
# /etc/systemd/system/tunr-relay.service
[Unit]
Description=tunr Relay Server
After=network-online.target postgresql.service

[Service]
Type=simple
User=tunr
ExecStart=/usr/local/bin/tunr-relay \
    --domain tunnel.yourcompany.com \
    --port 8080
Restart=always
RestartSec=5
Environment=DATABASE_URL=postgres://tunr:tunr@localhost:5432/tunr
Environment=TUNR_JWT_SECRET=your-secret-here

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now tunr-relay
```

## Security Notes

- Always use HTTPS in production (Caddy handles this automatically)
- Change `TUNR_JWT_SECRET` to a strong random value
- Consider restricting access with IP whitelisting at the firewall level
- The relay server stores tunnel metadata and audit logs; ensure your PostgreSQL is secured
- Use the PolyForm Shield license terms — self-hosting for internal use is allowed; competing services are not

## Updating

```bash
cd tunr
git pull
docker compose build --no-cache relay
docker compose up -d
```
