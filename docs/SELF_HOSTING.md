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
