# tunr.sh — Production Setup Guide

> Single Hetzner server: Landing + Dashboard + Relay + PostgreSQL.

---

## Architecture

```
                    ┌─────────────────────────────────────────────┐
                    │           Hetzner CPX21 (single server)     │
                    │                                             │
tunr.sh ────────┐   │  ┌──────────┐   ┌──────────────────────┐   │
www.tunr.sh ────┤   │  │  Caddy   │──▶│ /var/www/tunr        │   │
                ├──▶│  │  :443    │   │ (Landing — static)   │   │
app.tunr.sh ────┤   │  │  :80     │──▶│ Next.js Dashboard    │   │
                │   │  │          │   │ (localhost:3000)      │   │
*.tunr.sh ──────┤   │  │          │──▶│ Relay Server (Go)    │   │
relay.tunr.sh ──┘   │  │          │   │ (localhost:8080)      │   │
                    │  └──────────┘   └──────────────────────┘   │
                    │                                             │
                    │  ┌──────────────────────┐                  │
                    │  │ PostgreSQL (Docker)   │                  │
                    │  │ localhost:5432        │                  │
                    │  └──────────────────────┘                  │
                    └─────────────────────────────────────────────┘
```

| Subdomain | Target | Description |
|-----------|--------|-------------|
| `tunr.sh`, `www.tunr.sh` | `/var/www/tunr/` (static) | Landing page |
| `app.tunr.sh` | `localhost:3000` | Next.js dashboard |
| `relay.tunr.sh` | `localhost:8080` | Go relay server |
| `*.tunr.sh` | `localhost:8080` | User tunnel subdomains |

---

## Prerequisites

- Hetzner Cloud account (CPX21 ~€12/mo)
- `tunr.sh` domain on Cloudflare (Free plan)
- SSH key pair
- Resend account (magic link emails)
- Paddle account (billing — optional for initial setup)

---

## Step 1: Server Setup

```bash
ssh root@<SERVER_IP>

# System update
apt update && apt upgrade -y

# Firewall
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw enable

# Security
apt install -y fail2ban unattended-upgrades
systemctl enable --now fail2ban
dpkg-reconfigure -plow unattended-upgrades

# Application user
useradd -m -s /bin/bash tunr
mkdir -p /home/tunr/.ssh
cp /root/.ssh/authorized_keys /home/tunr/.ssh/
chown -R tunr:tunr /home/tunr/.ssh
chmod 700 /home/tunr/.ssh && chmod 600 /home/tunr/.ssh/authorized_keys
usermod -aG sudo tunr

hostnamectl set-hostname tunr-prod
```

---

## Step 2: Install Software

```bash
# Docker
curl -fsSL https://get.docker.com | sh
systemctl enable --now docker
usermod -aG docker tunr

# Go 1.22+
wget https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin:/root/go/bin' >> /etc/profile.d/go.sh
source /etc/profile.d/go.sh

# Node.js 20 LTS
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt install -y nodejs

# Caddy with Cloudflare DNS plugin
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
xcaddy build --with github.com/caddy-dns/cloudflare --output /usr/local/bin/caddy
setcap cap_net_bind_service=+ep /usr/local/bin/caddy

useradd -r -s /usr/sbin/nologin caddy
mkdir -p /etc/caddy /var/log/caddy /var/lib/caddy
chown caddy:caddy /var/log/caddy /var/lib/caddy
```

---

## Step 3: PostgreSQL

```bash
mkdir -p /opt/tunr && cd /opt/tunr

cat > docker-compose.yml << 'COMPOSE'
version: '3.9'
services:
  postgres:
    image: postgres:16-alpine
    container_name: tunr-postgres
    restart: unless-stopped
    environment:
      POSTGRES_DB: tunr
      POSTGRES_USER: tunr
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - pgdata:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d:ro
    ports:
      - "127.0.0.1:5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U tunr"]
      interval: 10s
      timeout: 5s
      retries: 5
volumes:
  pgdata:
COMPOSE
```

Copy migrations:

```bash
mkdir -p /opt/tunr/migrations
# From dev machine:
scp /path/to/tunr/relay/migrations/001_init.sql root@<SERVER_IP>:/opt/tunr/migrations/
```

---

## Step 4: Environment File

```bash
DB_PASS=$(openssl rand -base64 32)
JWT_SEC=$(openssl rand -hex 32)

cat > /opt/tunr/.env << ENV
# Database
DB_PASSWORD=$DB_PASS
DATABASE_URL=postgres://tunr:$DB_PASS@localhost:5432/tunr?sslmode=disable

# Relay
TUNR_DOMAIN=tunr.sh
TUNR_JWT_SECRET=$JWT_SEC
PORT=8080

# Cloudflare (Caddy TLS)
CF_API_TOKEN=<your-cloudflare-api-token>

# Dashboard
JWT_SECRET=$JWT_SEC
NEXT_PUBLIC_APP_URL=https://app.tunr.sh

# Resend (magic link auth)
RESEND_API_KEY=<your-resend-api-key>
RESEND_FROM=tunr <noreply@tunr.sh>

# Paddle (billing — set after creating product in Paddle)
PADDLE_API_KEY=
PADDLE_WEBHOOK_SECRET=
NEXT_PUBLIC_PADDLE_CLIENT_TOKEN=
NEXT_PUBLIC_PADDLE_PRICE_ID=
NEXT_PUBLIC_PADDLE_ENV=sandbox
ENV

chmod 600 /opt/tunr/.env
```

Start PostgreSQL:

```bash
cd /opt/tunr
docker compose up -d postgres

# Verify tables
docker compose exec postgres psql -U tunr -d tunr -c "\dt"
```

---

## Step 5: Source Code

```bash
# Option A: rsync from dev machine
rsync -avz --exclude='.git' --exclude='node_modules' --exclude='.next' \
  /path/to/tunr/ root@<SERVER_IP>:/opt/tunr/src/

# Option B: git clone
git clone https://github.com/ahmetvural79/tunr.git /opt/tunr/src
```

---

## Step 6: Landing Page

```bash
mkdir -p /var/www/tunr
cp -r /opt/tunr/src/landing/* /var/www/tunr/
cp /opt/tunr/src/install.sh /var/www/tunr/
cp /opt/tunr/src/assets/logo.svg /var/www/tunr/

chown -R caddy:caddy /var/www/tunr
chmod -R 755 /var/www/tunr
```

---

## Step 7: Dashboard (Next.js)

```bash
cd /opt/tunr/src/landing/app
npm ci --production
npm run build
```

Systemd service:

```bash
cat > /etc/systemd/system/tunr-dashboard.service << 'SVC'
[Unit]
Description=tunr Dashboard (Next.js)
After=network.target

[Service]
User=tunr
WorkingDirectory=/opt/tunr/src/landing/app
EnvironmentFile=/opt/tunr/.env
ExecStart=/usr/bin/npm start
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tunr-dashboard

[Install]
WantedBy=multi-user.target
SVC

systemctl daemon-reload
systemctl enable --now tunr-dashboard
```

---

## Step 8: Relay Server

**Relay server is included in the repo.** It lives as a separate Go module under `relay/`; it handles WebSocket transport, subdomain assignment, HTTP proxying, and health API. This is the backbone of the platform.

**Build:** Relay binary: `cd /opt/tunr/src/relay && CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /usr/local/bin/tunr-relay ./cmd/server`  
**Environment:** `TUNR_JWT_SECRET` (32+ chars), `TUNR_DOMAIN=tunr.sh`, `PORT=8080`; `DATABASE_URL` optional.  
**Note:** CLI connects to relay via WebSocket; `tunr share` returns real `*.tunr.sh` URLs.

Systemd service:

```bash
cat > /etc/systemd/system/tunr-relay.service << 'SVC'
[Unit]
Description=tunr Relay Server
After=network.target docker.service
Requires=docker.service

[Service]
User=tunr
WorkingDirectory=/opt/tunr
EnvironmentFile=/opt/tunr/.env
ExecStart=/usr/local/bin/tunr-relay
Restart=always
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/tunr
PrivateTmp=true
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tunr-relay

[Install]
WantedBy=multi-user.target
SVC

mkdir -p /var/log/tunr
chown tunr:tunr /var/log/tunr
systemctl daemon-reload
systemctl enable --now tunr-relay
```

---

## Step 9: Caddy

```bash
cat > /etc/caddy/Caddyfile << 'CADDY'
# Landing page
tunr.sh, www.tunr.sh {
    root * /var/www/tunr
    file_server

    handle /install.sh {
        root * /var/www/tunr
        file_server
    }

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
    }
}

# Dashboard (Next.js)
app.tunr.sh {
    reverse_proxy localhost:3000

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
    }
}

# Relay + tunnel subdomains
relay.tunr.sh, *.tunr.sh {
    tls {
        dns cloudflare {env.CF_API_TOKEN}
    }

    reverse_proxy localhost:8080
}
CADDY
```

Systemd service:

```bash
cat > /etc/systemd/system/caddy.service << 'SVC'
[Unit]
Description=Caddy Web Server
After=network.target

[Service]
User=caddy
Group=caddy
Environment=HOME=/var/lib/caddy
Environment=XDG_CONFIG_HOME=/var/lib/caddy
Environment=XDG_DATA_HOME=/var/lib/caddy
ExecStart=/usr/local/bin/caddy run --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile
TimeoutStopSec=5s
EnvironmentFile=/opt/tunr/.env
Restart=on-failure
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
SVC

systemctl daemon-reload
systemctl enable --now caddy
```

---

## Step 10: Cloudflare DNS

| Type | Name | Content | Proxy |
|------|------|---------|-------|
| A | `@` | `<SERVER_IP>` | Proxied |
| A | `www` | `<SERVER_IP>` | Proxied |
| A | `app` | `<SERVER_IP>` | Proxied |
| A | `relay` | `<SERVER_IP>` | DNS Only |
| A | `*` | `<SERVER_IP>` | DNS Only |

**SSL/TLS mode:** Full (Strict)

> `relay` and `*` must be DNS Only — WebSocket tunnels need direct connection, not Cloudflare proxy.

---

## Step 11: Resend Domain

1. [Resend Dashboard](https://resend.com/) → Domains → Add `tunr.sh`
2. Add the DNS records Resend provides to Cloudflare (SPF, DKIM, DMARC)
3. Wait for verification
4. Create API Key → copy to `/opt/tunr/.env` as `RESEND_API_KEY`

---

## Step 12: Paddle Setup

1. [Paddle Dashboard](https://vendors.paddle.com/) → Catalog → Products → Create
   - Name: **tunr Pro**
   - Price: **$5/month** (recurring)
2. Copy the **Price ID** → set as `NEXT_PUBLIC_PADDLE_PRICE_ID` in `.env`
3. Developer Tools → Authentication → Client-side token → copy to `NEXT_PUBLIC_PADDLE_CLIENT_TOKEN`
4. Developer Tools → Authentication → API key → copy to `PADDLE_API_KEY`
5. Developer Tools → Notifications → New destination:
   - URL: `https://app.tunr.sh/api/webhooks/paddle`
   - Events: `subscription.created`, `subscription.updated`, `subscription.canceled`, `subscription.past_due`
   - Copy webhook secret → `PADDLE_WEBHOOK_SECRET`
6. Set `NEXT_PUBLIC_PADDLE_ENV=sandbox` for testing, change to `production` when ready

After updating `.env`, restart the dashboard:

```bash
systemctl restart tunr-dashboard
```

---

## Step 13: Smoke Test

```bash
# Service status
systemctl status caddy tunr-relay tunr-dashboard docker

# Landing page
curl -sI https://tunr.sh | head -5

# Dashboard
curl -sI https://app.tunr.sh | head -5

# Install script
curl -s https://tunr.sh/install.sh | head -5

# Relay health
curl -s https://relay.tunr.sh/api/v1/health

# TLS check
curl -vI https://tunr.sh 2>&1 | grep "TLSv1"

# Security headers
curl -sI https://tunr.sh | grep -iE "strict|x-content|x-frame"

# Database
docker compose -f /opt/tunr/docker-compose.yml exec postgres psql -U tunr -d tunr -c "\dt"
```

---

## Step 14: Backup

```bash
cat > /opt/tunr/backup.sh << 'BACKUP'
#!/bin/bash
set -euo pipefail
DATE=$(date +%Y%m%d_%H%M%S)
DIR="/opt/tunr/backups/$DATE"
mkdir -p "$DIR"

docker compose -f /opt/tunr/docker-compose.yml exec -T postgres \
  pg_dump -U tunr tunr | gzip > "$DIR/tunr_db.sql.gz"

cp /opt/tunr/.env "$DIR/.env.bak"
find /opt/tunr/backups -mindepth 1 -maxdepth 1 -mtime +30 -exec rm -rf {} \;
echo "[$(date)] Backup OK: $DIR"
BACKUP

chmod +x /opt/tunr/backup.sh

# Cron: nightly at 03:00
(crontab -l 2>/dev/null; echo "0 3 * * * /opt/tunr/backup.sh >> /var/log/tunr/backup.log 2>&1") | crontab -
```

---

## Updating

```bash
# 1. Pull latest code (or from private repo)
cd /opt/tunr/src && git pull origin main

# 2. Relay update (build from relay/ module, not CLI)
cd /opt/tunr/src/relay
CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /usr/local/bin/tunr-relay ./cmd/server
systemctl restart tunr-relay

# 3. Status check
systemctl status tunr-relay
journalctl -u tunr-relay -f --no-pager -n 20
```

**Note:** If `relay/` is not in public repo, it should be present on server via a separate private repo or rsync. To update relay only, once relay code is updated, the `go build` + `systemctl restart` commands above are sufficient.

```bash
# Dashboard update (optional - skip if unchanged)
cd /opt/tunr/src/landing/app && npm ci --production && npm run build
systemctl restart tunr-dashboard

# Landing static update
cp -r /opt/tunr/src/landing/* /var/www/tunr/
```

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| Dashboard not loading | `journalctl -u tunr-dashboard -f` — check for build or env errors |
| Magic link not arriving | Check Resend dashboard for delivery status, verify DNS records |
| Paddle webhook failing | Check `PADDLE_WEBHOOK_SECRET` matches, test with sandbox |
| 522 error | Cloudflare SSL mode must be Full or Full (Strict) |
| Wildcard TLS failing | Ensure `CF_API_TOKEN` has Zone:DNS:Edit permission |
| PostgreSQL connection refused | `docker compose ps` — ensure postgres container is running |

---

*Last updated: March 2026*
