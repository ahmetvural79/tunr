# tunr.sh — Production Launch Walkthrough

> From zero to production, step by step. Single server, single domain, full control.

This guide covers every step needed to **publish tunr as open source on GitHub**, **deploy the backend + frontend on a single Hetzner server**, **set up the freemium model**, and **take the tunr.sh domain live**.

---

## Architecture Overview

```
                        ┌─────────────────────────────────────────────┐
                        │           Hetzner CPX21 (tek sunucu)        │
                        │                                             │
  tunr.sh ──────────┐   │  ┌──────────┐   ┌──────────────────────┐   │
  www.tunr.sh ──────┤   │  │  Caddy   │──▶│ /var/www/tunr        │   │
                    ├──▶│  │  :443    │   │ (Landing — static)   │   │
  app.tunr.sh ──────┤   │  │  :80     │──▶│ Next.js Dashboard    │   │
                    │   │  │          │   │ (localhost:3000)      │   │
  *.tunr.sh ────────┤   │  │          │──▶│ Relay Server (Go)    │   │
  relay.tunr.sh ────┘   │  │          │   │ (localhost:8080)      │   │
                        │  └──────────┘   └──────────────────────┘   │
                        │                                             │
                        │  ┌──────────────────────┐                  │
                        │  │ PostgreSQL (Docker)   │                  │
                        │  │ localhost:5432        │                  │
                        │  └──────────────────────┘                  │
                        └─────────────────────────────────────────────┘
```

**Yes — a single Hetzner server hosts both the backend and the frontend.** CPX21 (3 vCPU, 4GB RAM, ~€12/mo) is more than sufficient for the early stage. Caddy routes everything as a reverse proxy:

| Subdomain | Target | Description |
|-----------|--------|-------------|
| `tunr.sh`, `www.tunr.sh` | `/var/www/tunr/` (static) | Landing page |
| `app.tunr.sh` | `localhost:3000` | Next.js dashboard |
| `relay.tunr.sh` | `localhost:8080` | Go relay server |
| `*.tunr.sh` | `localhost:8080` | User tunnel subdomains |

---

## Prerequisites

Before you begin, make sure you have the following:

- [ ] **GitHub account** — `tunr-dev` organization created
- [ ] **Hetzner Cloud account** — payment method added
- [ ] **Cloudflare account** — `tunr.sh` domain added to Cloudflare
- [ ] **tunr.sh domain** — registered (Namecheap, Cloudflare, etc.)
- [ ] **SSH key pair** — generated with `ssh-keygen -t ed25519`

---

## Phase 0: License and Repo Preparation

### 0.1 — PolyForm Shield License

Tunr is a freemium product. The code is open source, but competitors are prohibited from taking the code and building a competing service. This is exactly the protection provided by the **PolyForm Shield 1.0.0** license.

> **MIT vs PolyForm Shield:** MIT allows anyone to do whatever they want with the code — someone could fork tunr and launch a competing service as "bettertunr.io". PolyForm Shield prevents this but allows users to read, modify, and use the code in their own internal projects.

The `LICENSE` file in the repo is already set as PolyForm Shield.

Reference projects:
- [slim](https://github.com/kamranahmedse/slim) — PolyForm Shield 1.0.0
- HashiCorp Terraform — BSL (similar concept)

### 0.2 — Freemium Plan Structure

| Feature | Free | Pro ($8/mo) |
|---------|------|-------------|
| Tunnel count | 1 concurrent | 10 concurrent |
| Subdomain | Random | Custom (`myapp.tunr.sh`) |
| Connection duration | 2 hours max | Unlimited |
| Bandwidth | 1 GB/day | 50 GB/day |
| Password protection | ✅ | ✅ |
| TTL / auto-expire | ✅ | ✅ |
| Path routing | ✅ | ✅ |
| Demo mode | ✅ | ✅ |
| Freeze mode | ❌ | ✅ |
| Widget injection | ❌ | ✅ |
| Auto-login | ❌ | ✅ |
| HTTP Inspector | Last 50 requests | Last 5000 requests |
| MCP Integration | ✅ | ✅ |
| Custom domain | ❌ | ✅ |
| Priority support | ❌ | ✅ |

### 0.3 — Create the GitHub Repository

```bash
# Create the tunr-dev organization on GitHub
# Then create the tunr-dev/tunr repo (public)

cd /path/to/tunr

# Set the remote
git remote set-url origin git@github.com:tunr-dev/tunr.git
# or if adding for the first time:
git remote add origin git@github.com:tunr-dev/tunr.git

# Initial push
git add -A
git commit -m "feat: initial public release"
git push -u origin main
```

### 0.4 — Homebrew Tap Repository

GoReleaser will automatically update the Homebrew formula. This requires a separate repo:

```bash
# Create on GitHub: tunr-dev/homebrew-tap (public)
# Put an empty README.md inside
```

### 0.5 — Set Up GitHub Secrets

In the `tunr-dev/tunr` repo settings → Settings → Secrets and variables → Actions:

| Secret | Value | Description |
|--------|-------|-------------|
| `GITHUB_TOKEN` | (automatic) | Provided by GitHub |
| `HOMEBREW_TAP_TOKEN` | GitHub PAT | Token with write access to the `tunr-dev/homebrew-tap` repo |

---

## Phase 1: Domain and DNS (Cloudflare)

### 1.1 — Add Domain to Cloudflare

1. [Cloudflare Dashboard](https://dash.cloudflare.com/) → Add a Site → `tunr.sh`
2. Plan: **Free**
3. Point the nameservers to Cloudflare from your domain registrar (Namecheap, etc.)

### 1.2 — Create Cloudflare API Token

Caddy needs Cloudflare API access to obtain a wildcard TLS certificate.

1. Cloudflare Dashboard → Profile → API Tokens → Create Token
2. Template: "Edit zone DNS"
3. Permissions:
   - `Zone : DNS : Edit`
   - `Zone : Zone : Read`
4. Zone resources: `tunr.sh`
5. Save the token — you'll use it on Hetzner

### 1.3 — DNS Records

After obtaining the server IP (in Phase 2), you'll add these records:

| Type | Name | Content | Proxy | Description |
|------|------|---------|-------|-------------|
| A | `@` | `<SERVER_IP>` | ☁️ Proxied | Landing page |
| A | `www` | `<SERVER_IP>` | ☁️ Proxied | Landing page redirect |
| A | `app` | `<SERVER_IP>` | ☁️ Proxied | Dashboard |
| A | `relay` | `<SERVER_IP>` | 🔘 DNS Only | Relay (WebSocket — must not go through proxy) |
| A | `*` | `<SERVER_IP>` | 🔘 DNS Only | Tunnel subdomains (WebSocket) |

> **Why are `relay` and `*` DNS Only?** WebSocket connections on the Cloudflare Free plan are subject to a 100-second timeout. Tunnels use long-lived WebSocket connections, so we bypass the Cloudflare proxy and use Caddy's TLS directly.

### 1.4 — Cloudflare SSL Setting

SSL/TLS → Overview → **Full (Strict)**

This ensures real TLS verification between Cloudflare and the server.

---

## Phase 2: Hetzner Server Setup

### 2.1 — Create the Server

[Hetzner Cloud Console](https://console.hetzner.cloud/) → New Server

| Setting | Value |
|---------|-------|
| Location | Falkenstein (fsn1) or Nuremberg (nbg1) |
| Image | Ubuntu 24.04 |
| Type | **CPX21** (3 vCPU, 4GB RAM, 80GB SSD) |
| Networking | IPv4 + IPv6 |
| SSH Keys | Add your public key |
| Name | `tunr-prod` |

**Cost:** ~€12.49/mo (billed hourly, stop anytime)

### 2.2 — Initial Server Security

```bash
ssh root@<SERVER_IP>

# Update the system
apt update && apt upgrade -y

# Firewall
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw enable

# Brute force protection
apt install -y fail2ban
systemctl enable --now fail2ban

# Automatic security patches
apt install -y unattended-upgrades
dpkg-reconfigure -plow unattended-upgrades

# Create application user
useradd -m -s /bin/bash tunr
mkdir -p /home/tunr/.ssh
cp /root/.ssh/authorized_keys /home/tunr/.ssh/
chown -R tunr:tunr /home/tunr/.ssh
chmod 700 /home/tunr/.ssh
chmod 600 /home/tunr/.ssh/authorized_keys

# Grant sudo to the tunr user
usermod -aG sudo tunr

# Restrict root SSH
sed -i 's/#PermitRootLogin yes/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
systemctl restart sshd
```

### 2.3 — Install Required Software

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
go version

# Node.js 20 LTS (for the Dashboard)
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt install -y nodejs
node -v && npm -v

# xcaddy (to build Caddy with the Cloudflare DNS plugin)
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# Build Caddy with the Cloudflare DNS plugin
xcaddy build --with github.com/caddy-dns/cloudflare --output /usr/local/bin/caddy
setcap cap_net_bind_service=+ep /usr/local/bin/caddy

# Caddy user
useradd -r -s /usr/sbin/nologin caddy
mkdir -p /etc/caddy /var/log/caddy
chown caddy:caddy /var/log/caddy
```

### 2.4 — Add DNS Records Now

Now that you have the server IP, add the DNS records from Phase 1.3 to Cloudflare.

Wait for DNS propagation:
```bash
dig tunr.sh +short
dig app.tunr.sh +short
dig relay.tunr.sh +short
```

---

## Phase 3: PostgreSQL (Docker)

### 3.1 — Working Directory and Docker Compose

```bash
mkdir -p /opt/tunr
cd /opt/tunr

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

### 3.2 — Environment File

```bash
cat > /opt/tunr/.env << ENV
# --- Database ---
DB_PASSWORD=$(openssl rand -base64 32)
DATABASE_URL=postgres://tunr:$(grep DB_PASSWORD /opt/tunr/.env 2>/dev/null | cut -d= -f2 || echo 'PLACEHOLDER')@localhost:5432/tunr?sslmode=disable

# --- Relay ---
TUNR_DOMAIN=tunr.sh
TUNR_JWT_SECRET=$(openssl rand -hex 32)
PORT=8080

# --- Cloudflare (Caddy TLS) ---
CF_API_TOKEN=<your-cloudflare-api-token-here>

# --- Dashboard ---
NEXT_PUBLIC_SUPABASE_URL=
NEXT_PUBLIC_SUPABASE_ANON_KEY=
ENV

chmod 600 /opt/tunr/.env

# Update DATABASE_URL with the correct password
source /opt/tunr/.env
sed -i "s|PLACEHOLDER|$DB_PASSWORD|" /opt/tunr/.env
```

### 3.3 — Migration SQL

```bash
mkdir -p /opt/tunr/migrations

cat > /opt/tunr/migrations/001_init.sql << 'SQL'
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    plan TEXT NOT NULL DEFAULT 'free',
    tunnel_limit INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tunnels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    subdomain TEXT UNIQUE NOT NULL,
    local_port INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    public_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_log (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    detail JSONB,
    ip TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tunnels_user ON tunnels(user_id);
CREATE INDEX idx_tunnels_subdomain ON tunnels(subdomain);
CREATE INDEX idx_sessions_token ON sessions(token_hash);
CREATE INDEX idx_audit_user ON audit_log(user_id);
SQL
```

### 3.4 — Start PostgreSQL

```bash
cd /opt/tunr
docker compose up -d postgres

# Verify tables were created
docker compose exec postgres psql -U tunr -d tunr -c "\dt"
```

---

## Phase 4: Source Code and Binaries

### 4.1 — Transfer Code to the Server

```bash
# From your development machine:
rsync -avz --exclude='.git' --exclude='node_modules' --exclude='vendor' \
  /path/to/tunr/ root@<SERVER_IP>:/opt/tunr/src/

# Alternative (after the repo is public):
# git clone https://github.com/tunr-dev/tunr.git /opt/tunr/src
```

### 4.2 — Build the Relay Server Binary

```bash
cd /opt/tunr/src/relay

CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags="-w -s -X main.Version=0.1.0" \
  -o /usr/local/bin/tunr-relay \
  ./cmd/server

chmod 755 /usr/local/bin/tunr-relay
```

### 4.3 — Build CLI Binaries (for Download)

```bash
cd /opt/tunr/src
mkdir -p /var/www/tunr/downloads

# Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath -ldflags="-w -s -X main.Version=0.1.0" \
  -o /var/www/tunr/downloads/tunr-linux-amd64 ./cmd/tunr

# Linux arm64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -trimpath -ldflags="-w -s -X main.Version=0.1.0" \
  -o /var/www/tunr/downloads/tunr-linux-arm64 ./cmd/tunr

# macOS Intel
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build \
  -trimpath -ldflags="-w -s -X main.Version=0.1.0" \
  -o /var/www/tunr/downloads/tunr-darwin-amd64 ./cmd/tunr

# macOS Apple Silicon
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
  -trimpath -ldflags="-w -s -X main.Version=0.1.0" \
  -o /var/www/tunr/downloads/tunr-darwin-arm64 ./cmd/tunr

# Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -trimpath -ldflags="-w -s -X main.Version=0.1.0" \
  -o /var/www/tunr/downloads/tunr-windows-amd64.exe ./cmd/tunr
```

### 4.4 — Install Script

```bash
cat > /var/www/tunr/install << 'INSTALLER'
#!/bin/sh
set -e

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

URL="https://tunr.sh/downloads/tunr-${OS}-${ARCH}"
DEST="/usr/local/bin/tunr"

echo "Downloading tunr for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o /tmp/tunr
chmod +x /tmp/tunr
sudo mv /tmp/tunr "$DEST"

echo ""
echo "  tunr installed to $DEST"
echo ""
echo "  Get started:"
echo "    tunr share --port 3000"
echo ""
INSTALLER
```

---

## Phase 5: Landing Page Deploy

### 5.1 — Copy Static Files

```bash
mkdir -p /var/www/tunr
cp -r /opt/tunr/src/landing/* /var/www/tunr/

chown -R caddy:caddy /var/www/tunr
chmod -R 755 /var/www/tunr
```

---

## Phase 6: Dashboard Deploy (Next.js)

### 6.1 — Install Dashboard Dependencies

```bash
cd /opt/tunr/src/landing/app

# Install dependencies
npm ci --production

# Production build
npm run build
```

### 6.2 — Dashboard Systemd Service

```bash
cat > /etc/systemd/system/tunr-dashboard.service << 'SERVICE'
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
SERVICE

systemctl daemon-reload
systemctl enable --now tunr-dashboard
systemctl status tunr-dashboard
```

---

## Phase 7: Relay Server Deploy

### 7.1 — Relay Systemd Service

```bash
cat > /etc/systemd/system/tunr-relay.service << 'SERVICE'
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
SERVICE

mkdir -p /var/log/tunr
chown tunr:tunr /var/log/tunr

systemctl daemon-reload
systemctl enable --now tunr-relay
systemctl status tunr-relay
```

---

## Phase 8: Caddy (TLS + Reverse Proxy)

Caddy manages all traffic: landing page, dashboard, relay, and tunnel subdomains.

### 8.1 — Caddyfile

```bash
cat > /etc/caddy/Caddyfile << 'CADDY'
# ============================================
# tunr.sh — Single Server Caddy Configuration
# ============================================

# Landing page
tunr.sh, www.tunr.sh {
    root * /var/www/tunr
    file_server

    handle /install {
        root * /var/www/tunr
        rewrite * /install
        file_server
    }

    handle /downloads/* {
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

# Relay server + Wildcard tunnel subdomains
relay.tunr.sh, *.tunr.sh {
    tls {
        dns cloudflare {env.CF_API_TOKEN}
    }

    reverse_proxy localhost:8080
}
CADDY
```

### 8.2 — Caddy Systemd Service

```bash
cat > /etc/systemd/system/caddy.service << 'SERVICE'
[Unit]
Description=Caddy Web Server
After=network.target

[Service]
User=caddy
Group=caddy
ExecStart=/usr/local/bin/caddy run --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile
TimeoutStopSec=5s
EnvironmentFile=/opt/tunr/.env
Restart=on-failure

AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable --now caddy
systemctl status caddy
```

### 8.3 — TLS Certificate Verification

```bash
# Wait a few minutes (for certificate issuance)
# Then test:

curl -vI https://tunr.sh 2>&1 | grep "SSL\|subject\|expire"
curl -vI https://app.tunr.sh 2>&1 | grep "SSL"
curl -vI https://relay.tunr.sh 2>&1 | grep "SSL"
```

---

## Phase 9: GitHub Releases & GoReleaser

### 9.1 — Release Workflow

The `.goreleaser.yaml` and `.github/workflows/release.yml` files in the repo are already prepared. The release process:

```
Developer            GitHub Actions          GitHub Releases
    │                      │                        │
    │  git tag v0.1.0      │                        │
    │  git push --tags     │                        │
    │─────────────────────▶│                        │
    │                      │  go test ./...         │
    │                      │  govulncheck ./...     │
    │                      │  goreleaser release     │
    │                      │──────────────────────▶ │
    │                      │                        │ Draft Release created
    │                      │                        │ 7 binaries + checksums
    │                      │                        │ Homebrew tap updated
    │                      │                        │
    │  Manual "Publish"    │                        │
    │──────────────────────────────────────────────▶│
    │                      │                        │ Release is LIVE!
```

### 9.2 — First Release

```bash
# On your development machine:
cd /path/to/tunr

# Final checks
go test ./...
go vet ./...

# Create and push the tag
git tag -a v0.1.0 -m "tunr v0.1.0 — first public release"
git push origin v0.1.0
```

GitHub Actions will automatically:
1. Run all tests and security scans
2. Build 7 binaries (linux/amd64, linux/arm64, linux/386, darwin/amd64, darwin/arm64, windows/amd64)
3. Generate SHA256 checksums
4. Create a draft release
5. Update the Homebrew formula

### 9.3 — Publish the Release

1. GitHub → tunr-dev/tunr → Releases
2. Review the draft release
3. Edit the release notes (if necessary)
4. Click **"Publish release"**

### 9.4 — How Will Users Install?

After the release is published, users can install with the following:

```bash
# Homebrew (macOS)
brew install tunr-dev/tap/tunr

# Curl (Linux/macOS)
curl -sSL https://tunr.sh/install | sh

# Direct download from GitHub Releases
# https://github.com/tunr-dev/tunr/releases/latest

# Build from source
git clone https://github.com/tunr-dev/tunr.git && cd tunr
go build -o tunr ./cmd/tunr
```

---

## Phase 10: Verification and Smoke Test

### 10.1 — Service Status

```bash
# Verify all services are running
systemctl status tunr-relay caddy tunr-dashboard docker

# PostgreSQL
docker compose -f /opt/tunr/docker-compose.yml ps
```

### 10.2 — Endpoint Tests

```bash
# Landing page
curl -sI https://tunr.sh | head -5

# Dashboard
curl -sI https://app.tunr.sh | head -5

# Relay health
curl -s https://relay.tunr.sh/api/v1/health

# Install script
curl -s https://tunr.sh/install | head -5

# Binary download
curl -sI https://tunr.sh/downloads/tunr-linux-amd64 | head -5
```

### 10.3 — End-to-End Tunnel Test

```bash
# From your own computer:

# 1. Install tunr
curl -sSL https://tunr.sh/install | sh

# 2. Start a test HTTP server
python3 -m http.server 9999 &

# 3. Open a tunnel
tunr share --port 9999

# 4. Open the provided URL in a browser
# You'll see a URL like https://abc123.tunr.sh
```

### 10.4 — Security Check

```bash
# TLS version
curl -vI https://tunr.sh 2>&1 | grep "TLSv1.3"

# Security headers
curl -sI https://tunr.sh | grep -iE "strict|x-content|x-frame"

# Firewall status
ufw status verbose

# Open ports
ss -tlnp
```

---

## Phase 11: Monitoring and Maintenance

### 11.1 — Log Monitoring

```bash
# Relay logs
journalctl -u tunr-relay -f

# Dashboard logs
journalctl -u tunr-dashboard -f

# Caddy logs
journalctl -u caddy -f

# PostgreSQL logs
docker compose -f /opt/tunr/docker-compose.yml logs -f postgres
```

### 11.2 — Automated Backup

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

# Cron: every night at 03:00
(crontab -l 2>/dev/null; echo "0 3 * * * /opt/tunr/backup.sh >> /var/log/tunr/backup.log 2>&1") | crontab -
```

### 11.3 — Update Procedure

```bash
# 1. Pull the latest code
cd /opt/tunr/src
git pull origin main

# 2. Update the relay binary
cd relay
CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" \
  -o /usr/local/bin/tunr-relay-new ./cmd/server

# 3. Zero-downtime swap
mv /usr/local/bin/tunr-relay-new /usr/local/bin/tunr-relay
systemctl restart tunr-relay

# 4. Update the dashboard
cd /opt/tunr/src/landing/app
npm ci --production && npm run build
systemctl restart tunr-dashboard

# 5. Update the landing page
cp -r /opt/tunr/src/landing/* /var/www/tunr/
```

---

## Launch Checklist

Final checklist — check off each item:

### Infrastructure
- [ ] Hetzner server is up and secured
- [ ] PostgreSQL is running and migrations are applied
- [ ] Caddy is running, all domains accessible via TLS
- [ ] Relay server is running
- [ ] Dashboard is running

### Domain & DNS
- [ ] `tunr.sh` resolves to the landing page
- [ ] `app.tunr.sh` resolves to the dashboard
- [ ] `relay.tunr.sh` resolves to the relay
- [ ] `*.tunr.sh` wildcard resolves to tunnels
- [ ] HTTPS certificates are valid

### GitHub
- [ ] `tunr-dev/tunr` repo is public
- [ ] `tunr-dev/homebrew-tap` repo created
- [ ] GitHub Actions CI workflow is running
- [ ] Release workflow tested
- [ ] `GITHUB_TOKEN` and `HOMEBREW_TAP_TOKEN` secrets added
- [ ] First release (`v0.1.0`) published

### Content
- [ ] README.md is up to date and accurate
- [ ] LICENSE is PolyForm Shield 1.0.0
- [ ] SECURITY.md exists
- [ ] CONTRIBUTING.md exists
- [ ] Landing page is live
- [ ] Install script is working

### Security
- [ ] `govulncheck` is clean
- [ ] Firewall is active (only 22, 80, 443)
- [ ] fail2ban is active
- [ ] `.env` file has 600 permissions
- [ ] PostgreSQL is accessible only from localhost
- [ ] Security headers are active

### Test
- [ ] `tunr share --port 3000` works
- [ ] HTTP traffic passes through the tunnel
- [ ] WebSocket connection is stable
- [ ] Inspector dashboard opens
- [ ] `tunr doctor` returns clean output

---

## Frequently Asked Questions

### Is a single server sufficient?

**Yes, more than sufficient for the start.** CPX21 (3 vCPU, 4GB RAM):
- Supports ~500 concurrent tunnels
- Go relay server uses very low resources (~50MB RAM)
- Next.js dashboard ~200MB RAM
- PostgreSQL ~100MB RAM
- Caddy ~20MB RAM
- **Total: ~400MB** — 10% of 4GB

Scale-up plan as you grow:
1. **First step:** CPX21 → CPX31 (4 vCPU, 8GB) — €16/mo
2. **Second step:** Move the dashboard to a separate server or Vercel
3. **Third step:** Distribute the relay across multiple regions (Fly.io / multi-region)

### What if free users exhaust the server?

Plan limits are enforced at the relay server:
- Free: 1 concurrent tunnel, 2 hours max, 1 GB/day
- Rate limiting: 10 tunnel requests per minute per IP
- Abuse detection: Abnormal traffic patterns are automatically blocked

### Can I accept community contributions with PolyForm Shield?

**Yes.** PolyForm Shield does not prevent contributions. Contributors can use, modify, and distribute the code — the only restriction is building a competing service. You should update CONTRIBUTING.md to reference PolyForm Shield.

---

*This document was prepared for the tunr v0.1.0 launch process. Last updated: March 2026*
