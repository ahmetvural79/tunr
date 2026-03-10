# tunr — Deployment & Testing Guide

> Production setup from scratch on Hetzner Cloud and an end-to-end testing walkthrough.

---

## Table of Contents

1. [Requirements](#1-requirements)
2. [Hetzner Server Setup](#2-hetzner-server-setup)
3. [Docker & Dependencies](#3-docker--dependencies)
4. [PostgreSQL Setup](#4-postgresql-setup)
5. [Relay Server Deploy](#5-relay-server-deploy)
6. [Caddy (TLS + Reverse Proxy)](#6-caddy-tls--reverse-proxy)
7. [DNS Configuration](#7-dns-configuration)
8. [Landing Page Deploy](#8-landing-page-deploy)
9. [Environment Variables](#9-environment-variables)
10. [Service Management (systemd)](#10-service-management-systemd)
11. [End-to-End Testing Guide](#11-end-to-end-testing-guide)
12. [Monitoring and Logs](#12-monitoring-and-logs)
13. [Backups](#13-backups)
14. [Troubleshooting](#14-troubleshooting)

---

## 1. Requirements

| Component | Version | Purpose |
|-----------|---------|---------|
| Hetzner Cloud | CPX21 (3 vCPU, 4GB RAM) | Relay server |
| Ubuntu | 24.04 LTS | Operating system |
| Go | 1.22+ | Compile relay binary |
| Docker / Docker Compose | 25+ | PostgreSQL container |
| Caddy | 2.7+ | TLS + reverse proxy |
| Cloudflare | Free plan | DNS + wildcard TLS |

**Estimated cost:** ~€0.037/hour → ~€12/month (CPX21)

---

## 2. Hetzner Server Setup

### 2.1 Create a Server

[Hetzner Cloud Console](https://console.hetzner.cloud/) → New Server

| Setting | Value |
|---------|-------|
| Location | Nuremberg (nbg1) or Helsinki (hel1) |
| Image | Ubuntu 24.04 |
| Type | CPX21 (3 vCPU, 4GB) |
| Networking | Enable IPv4 + IPv6 |
| SSH Key | Add your public key |

### 2.2 Initial Connection and Security

```bash
# Connect
ssh root@<SERVER_IP>

# Update
apt update && apt upgrade -y

# Firewall — only open required ports
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp   # SSH
ufw allow 80/tcp   # HTTP (Let's Encrypt redirect)
ufw allow 443/tcp  # HTTPS
ufw enable

# fail2ban (brute force protection)
apt install -y fail2ban
systemctl enable --now fail2ban

# Unattended upgrades (automatic security patches)
apt install -y unattended-upgrades
dpkg-reconfigure -plow unattended-upgrades

# Disable root SSH login (SSH key login is already secure)
sed -i 's/#PermitRootLogin yes/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
systemctl restart sshd

# Create tunr user
useradd -m -s /bin/bash tunr
usermod -aG docker tunr
```

### 2.3 Set Hostname

```bash
hostnamectl set-hostname tunr-relay
echo "127.0.0.1 tunr-relay" >> /etc/hosts
```

---

## 3. Docker & Dependencies

```bash
# Install Docker
curl -fsSL https://get.docker.com | sh
systemctl enable --now docker

# Install Go (for compiling the relay binary)
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh
source /etc/profile.d/go.sh
go version

# Install Caddy
apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | tee /etc/apt/sources.list.d/caddy-stable.list
apt update && apt install -y caddy
```

---

## 4. PostgreSQL Setup

### 4.1 Start with Docker Compose

```bash
mkdir -p /opt/tunr && cd /opt/tunr

cat > docker-compose.yml << 'EOF'
version: '3.9'

services:
  postgres:
    image: postgres:16-alpine
    container_name: tunr-postgres
    restart: unless-stopped
    environment:
      POSTGRES_DB: tunr
      POSTGRES_USER: tunr
      POSTGRES_PASSWORD: ${DB_PASSWORD}  # Loaded from .env
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./relay/migrations:/docker-entrypoint-initdb.d:ro
    ports:
      - "127.0.0.1:5432:5432"   # Localhost only — not exposed externally
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U tunr"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
EOF
```

### 4.2 Create .env File

```bash
# SECURITY: .env is never committed to git!
cat > /opt/tunr/.env << EOF
DB_PASSWORD=$(openssl rand -base64 32)
TUNR_JWT_SECRET=$(openssl rand -hex 32)
TUNR_DOMAIN=tunr.sh
PORT=8080
CLOUDFLARE_API_TOKEN=your_cf_token_here
EOF

chmod 600 /opt/tunr/.env
```

> [!CAUTION]
> Never commit the `.env` file to git and never share it with anyone.

### 4.3 Start PostgreSQL

```bash
cd /opt/tunr
docker compose up -d postgres

# Verify that migrations have run
docker compose exec postgres psql -U tunr -d tunr -c "\dt"
# Should show users, tunnels, sessions, magic_tokens, audit_log, plan_limits tables
```

---

## 5. Relay Server Deploy

### 5.1 Copy Source Code

```bash
# From your development machine (in the project directory):
rsync -avz --exclude='.git' --exclude='vendor' \
  tunr/ root@<SERVER_IP>:/opt/tunr/src/

# Or via git:
# ssh root@<SERVER_IP>
# git clone https://github.com/yourusername/tunr /opt/tunr/src
```

### 5.2 Compile the Relay Binary

```bash
cd /opt/tunr/src/relay

# Production binary — no debug info, small size
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags="-w -s -X main.Version=$(git describe --tags --always)" \
  -o /usr/local/bin/tunr-relay \
  ./cmd/server

# Set permissions
chmod 755 /usr/local/bin/tunr-relay

# Test it
TUNR_JWT_SECRET="test-secret-32-chars-minimum-len" \
  tunr-relay --help 2>&1 || echo "Binary is working"
```

### 5.3 Compile and Distribute CLI Binary

```bash
# tunr CLI binary (distributed to users)
cd /opt/tunr/src
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath \
  -ldflags="-w -s" \
  -o /var/www/tunr/downloads/tunr-linux-amd64 \
  ./cmd/tunr

CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
  -trimpath \
  -ldflags="-w -s" \
  -o /var/www/tunr/downloads/tunr-darwin-arm64 \
  ./cmd/tunr

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -trimpath \
  -ldflags="-w -s" \
  -o /var/www/tunr/downloads/tunr-windows-amd64.exe \
  ./cmd/tunr
```

---

## 6. Caddy (TLS + Reverse Proxy)

### 6.1 Create a Cloudflare API Token

[Cloudflare Dashboard](https://dash.cloudflare.com/) → Profile → API Tokens → Create Token

Required permissions:
- `Zone:DNS:Edit` — for the tunr.sh zone
- `Zone:Zone:Read`

### 6.2 Build Caddy with Cloudflare DNS Plugin via xcaddy

```bash
# Install xcaddy
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# Build Caddy with Cloudflare DNS plugin
xcaddy build \
  --with github.com/caddy-dns/cloudflare \
  --output /usr/local/bin/caddy

# Grant capability to Caddy (no root required for ports 80/443)
setcap cap_net_bind_service=+ep /usr/local/bin/caddy
```

### 6.3 Configure Caddyfile

```bash
mkdir -p /etc/caddy
cp /opt/tunr/src/relay/caddy/Caddyfile /etc/caddy/Caddyfile

# Update Cloudflare token
sed -i "s/CLOUDFLARE_API_TOKEN/$CLOUDFLARE_API_TOKEN/" /etc/caddy/Caddyfile
```

### 6.4 Caddy Service

```bash
# /etc/systemd/system/caddy.service
cat > /etc/systemd/system/caddy.service << 'EOF'
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

[Install]
WantedBy=multi-user.target
EOF

useradd -r -s /usr/sbin/nologin caddy
systemctl daemon-reload
systemctl enable --now caddy
```

---

## 7. DNS Configuration

[Cloudflare Dashboard](https://dash.cloudflare.com/) → tunr.sh → DNS

| Type | Name | Content | Proxy |
|------|------|---------|-------|
| A | `@` | `<SERVER_IP>` | ☁️ Proxied |
| A | `www` | `<SERVER_IP>` | ☁️ Proxied |
| A | `*` | `<SERVER_IP>` | ☁️ Proxied |
| A | `relay` | `<SERVER_IP>` | 🟠 DNS only |

> [!IMPORTANT]
> The `*` wildcard record routes each tunnel's subdomain (abc123.tunr.sh) to the correct IP address.

**Cloudflare SSL/TLS Mode:** Full (Strict) — real TLS between Caddy and Cloudflare.

---

## 8. Landing Page Deploy

```bash
mkdir -p /var/www/tunr
cp -r /opt/tunr/src/landing/* /var/www/tunr/

# Set permissions
chown -R caddy:caddy /var/www/tunr
chmod -R 755 /var/www/tunr
```

---

## 9. Environment Variables

```bash
# /opt/tunr/.env — full list

# ─── Required ─────────────────────────────
TUNR_DOMAIN=tunr.sh
TUNR_JWT_SECRET=<at-least-32-character-random-string>
DATABASE_URL=postgres://tunr:<DB_PASSWORD>@localhost:5432/tunr?sslmode=disable

# ─── Optional ─────────────────────────────
PORT=8080
TUNR_LOG_LEVEL=info        # debug | info | warn

# ─── Paddle (billing) ────────────────────
PADDLE_API_KEY=<paddle-api-key>
PADDLE_WEBHOOK_SECRET=<paddle-webhook-secret>
PADDLE_SANDBOX=false

# ─── Cloudflare (TLS) ────────────────────
CLOUDFLARE_API_TOKEN=<cf-api-token>

# ─── Email (magic link) ──────────────────
SMTP_HOST=smtp.resend.com
SMTP_PORT=587
SMTP_USER=resend
SMTP_PASS=<resend-api-key>
SMTP_FROM=noreply@tunr.sh
```

---

## 10. Service Management (systemd)

```bash
# /etc/systemd/system/tunr-relay.service
cat > /etc/systemd/system/tunr-relay.service << 'EOF'
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

# SECURITY: Process isolation
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/tunr
PrivateTmp=true

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tunr-relay

[Install]
WantedBy=multi-user.target
EOF

mkdir -p /var/log/tunr
chown tunr:tunr /var/log/tunr

systemctl daemon-reload
systemctl enable --now tunr-relay

# Verify
systemctl status tunr-relay
journalctl -u tunr-relay -f
```

---

## 11. End-to-End Testing Guide

### 11.1 Unit Tests (Local)

```bash
# All tests (with race detector)
cd tunr
go test -race -timeout 60s ./internal/... ./cmd/...

# Relay tests
cd relay
go test -race -timeout 30s ./internal/...

# Specific package
go test -v -run TestJWTAlgNone ./relay/internal/auth/...

# Benchmark
go test -bench=. -benchmem ./internal/inspector/...
```

**Expected output:**
```
ok  github.com/tunr-dev/tunr/internal/billing   ✓
ok  github.com/tunr-dev/tunr/internal/inspector  ✓
ok  github.com/tunr-dev/tunr/internal/mcp        ✓
ok  github.com/tunr-dev/tunr/internal/config     ✓
ok  github.com/tunr-dev/tunr/relay/internal/auth ✓
ok  github.com/tunr-dev/tunr/relay/internal/relay ✓
```

### 11.2 CLI Smoke Test

```bash
# Build the binary
go build -o tunr ./cmd/tunr

# Version check
./tunr version

# System diagnostics
./tunr doctor

# Initialize config
./tunr config init
cat .tunr.json

# Help
./tunr --help
./tunr share --help
./tunr mcp --help
```

### 11.3 Relay Server End-to-End Test

```bash
# Terminal 1 — Start a test HTTP server
python3 -m http.server 9999

# Terminal 2 — Start the relay server (local)
cd relay
TUNR_JWT_SECRET="test-secret-minimum-32-chars!!" \
  DATABASE_URL="" \
  TUNR_DOMAIN="localhost:8080" \
  PORT=8080 \
  go run ./cmd/server &
RELAY_PID=$!

# Terminal 3 — Open a tunr CLI tunnel
# (point to relay at localhost:8080)
TUNR_RELAY_URL="ws://localhost:8080/tunnel/connect" \
  ./tunr share --port 9999

# Another terminal — Test the tunnel
curl http://localhost:19842/api/v1/health    # Inspector API
curl http://localhost:19842/api/v1/requests  # Captured requests

kill $RELAY_PID
```

### 11.4 WebSocket Tunnel Test

```bash
# Verify WebSocket connection with wscat
npm install -g wscat

# Connect to the relay (test)
wscat -c "ws://localhost:8080/tunnel/connect" \
  --header "Authorization: Bearer test"

# Send a hello message
{"type":"hello","data":{"token":"","local_port":3000,"version":"test"}}

# Expected response:
# {"type":"welcome","data":{"tunnel_id":"abc12345","subdomain":"xyzabc12","public_url":"http://xyzabc12.localhost:8080"}}
```

### 11.5 Security Tests

```bash
# 1. JWT alg:none attack test
go test -v -run TestJWTAlgNone ./relay/internal/auth/...

# 2. Webhook replay attack test
go test -v -run TestWebhookReplayAttack ./internal/billing/...

# 3. Header sanitization test
go test -v -run TestSensitiveHeaderRedaction ./internal/inspector/...

# 4. Race condition test
go test -race -v -run TestRegistryConcurrency ./relay/internal/relay/...

# 5. SSRF protection (are requests to private IPs blocked?)
# Verify the relay does not forward to private IPs
curl -H "Host: 127.0.0.1.tunr.sh" http://localhost:8080/
# Expected: 404 "tunnel not found" (because it is a private IP)

# 6. govulncheck (known CVEs)
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...

# 7. golangci-lint
golangci-lint run ./...
```

### 11.6 Production Health Checks

```bash
# Relay health check
curl https://tunr.sh/api/v1/health
# {"status":"ok","timestamp":1709855000}

# Active tunnel count
curl https://tunr.sh/api/v1/status
# {"active_tunnels":42}

# TLS certificate check
curl -vI https://tunr.sh 2>&1 | grep -E "SSL|subject|expire"

# Wildcard TLS
curl -vI https://test.tunr.sh 2>&1 | grep -E "SSL|CN="

# PostgreSQL connection
docker compose exec postgres psql -U tunr -d tunr \
  -c "SELECT COUNT(*) FROM users;"
```

### 11.7 Load Testing

```bash
# Simple load test with hey
go install github.com/rakyll/hey@latest

# 100 concurrent connections, 1000 requests
hey -n 1000 -c 100 https://tunr.sh/api/v1/health

# Expected results:
# Requests/sec: >500
# P99 latency: <100ms
# Error rate: 0%
```

### 11.8 CLI Integration Test (full flow)

```bash
#!/bin/bash
# test_e2e.sh — end-to-end integration test

set -euo pipefail

PORT=18888
echo "🧪 Starting tunr E2E test..."

# Test HTTP server
python3 -c "
import http.server, threading
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'tunr test OK')
    def log_message(self, *a): pass
s = http.server.HTTPServer(('', $PORT), H)
threading.Timer(30, s.shutdown).start()
s.serve_forever()
" &
HTTP_PID=$!

# Open tunr tunnel
TUNNEL_OUT=$(./tunr share --port $PORT --json 2>&1 | head -20)
TUNNEL_URL=$(echo "$TUNNEL_OUT" | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        d = json.loads(line)
        if 'url' in d: print(d['url']); break
    except: pass
")

if [ -z "$TUNNEL_URL" ]; then
  echo "❌ Failed to obtain tunnel URL"
  kill $HTTP_PID
  exit 1
fi

echo "✅ Tunnel is open: $TUNNEL_URL"

# HTTP test through the tunnel
RESP=$(curl -sf "$TUNNEL_URL" 2>&1 || echo "FAIL")
if [[ "$RESP" == *"tunr test OK"* ]]; then
  echo "✅ HTTP tunnel is working"
else
  echo "❌ HTTP tunnel failed: $RESP"
  kill $HTTP_PID
  exit 1
fi

# Inspector API
REQS=$(curl -sf http://localhost:19842/api/v1/requests 2>/dev/null)
echo "✅ Inspector: $REQS"

kill $HTTP_PID
echo "🎉 All tests PASSED!"
```

---

## 12. Monitoring and Logs

### View Logs

```bash
# Relay logs
journalctl -u tunr-relay -f

# Caddy logs
journalctl -u caddy -f

# PostgreSQL logs
docker compose logs -f postgres

# All service statuses
systemctl status tunr-relay caddy docker
```

### Prometheus Metrics (Phase 7+)

```bash
# Relay metrics (planned endpoint)
curl http://localhost:9090/metrics
```

---

## 13. Backups

```bash
# /opt/tunr/backup.sh
#!/bin/bash
set -euo pipefail
DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="/opt/tunr/backups/$DATE"
mkdir -p "$BACKUP_DIR"

# PostgreSQL dump
docker compose exec -T postgres \
  pg_dump -U tunr tunr | gzip > "$BACKUP_DIR/tunr_db.sql.gz"

# Config backup
cp /opt/tunr/.env "$BACKUP_DIR/.env.bak"

# Delete backups older than 30 days
find /opt/tunr/backups -mindepth 1 -maxdepth 1 -mtime +30 -exec rm -rf {} \;

echo "✅ Backup completed: $BACKUP_DIR"
```

```bash
# Cron: every night at 02:00
crontab -e
# 0 2 * * * /opt/tunr/backup.sh >> /var/log/tunr/backup.log 2>&1
```

---

## 14. Troubleshooting

### Tunnel cannot connect

```bash
# 1. Is the relay running?
systemctl status tunr-relay
curl http://localhost:8080/api/v1/health

# 2. Firewall
ufw status verbose
# Ports 443 and 80 must be open

# 3. Is DNS resolving?
dig abc123.tunr.sh
# Should show <SERVER_IP>

# 4. TLS certificate
curl -vI https://tunr.sh 2>&1 | grep "SSL"
```

### PostgreSQL connection lost

```bash
docker compose restart postgres
# The relay will automatically reconnect (connection pool)
```

### Let's Encrypt rate limit

```bash
# There is a limit of 5 certificates per domain per week
# Use the staging CA for testing:
# acme_ca https://acme-staging-v02.api.letsencrypt.org/directory
```

### High memory usage

```bash
# Check the active tunnel count
curl http://localhost:8080/api/v1/status

# Force Go GC
kill -SIGUSR1 $(pidof tunr-relay)
```

### tunr doctor reports errors

```bash
tunr doctor
# Check each line individually
# ✗ Daemon: systemctl start tunr or ./tunr start
# ✗ Login: tunr login
# ✗ Relay: Relay has not been deployed yet
```

---

## Updating

```bash
# 1. Build a new binary
cd /opt/tunr/src
git pull
cd relay
CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" \
  -o /usr/local/bin/tunr-relay-new ./cmd/server

# 2. Zero-downtime restart
mv /usr/local/bin/tunr-relay-new /usr/local/bin/tunr-relay
systemctl reload tunr-relay || systemctl restart tunr-relay

# 3. Apply new migrations
docker compose exec postgres \
  psql -U tunr -d tunr -f /docker-entrypoint-initdb.d/002_update.sql
```
