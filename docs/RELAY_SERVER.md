# Running Relay on Server

Relay is the server-side component that routes traffic for tunnels opened with `tunr share`. This document summarizes how to build and run relay in production.

## Requirements

- Go 1.22+
- Required env: `TUNR_JWT_SECRET` (32+ chars), `TUNR_DOMAIN=tunr.sh`, `PORT=8080`
- Optional: `DATABASE_URL` (PostgreSQL; runs in-memory when unset)

## Steps

### 1. Copy relay source to the server

If `relay/` is not in the public repo, copy it from your private repository or your local machine:

```bash
# Example: rsync from local machine
rsync -avz --exclude '.git' /Users/ahmetvural/Desktop/vibetunnel/tunr/relay/ user@89.167.112.148:/opt/tunr/src/relay/
```

Or clone a separate private repository directly on the server.

### 2. Build

```bash
cd /opt/tunr/src/relay
CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /usr/local/bin/tunr-relay ./cmd/server
```

### 3. Environment variables

Set these in `/opt/tunr/.env` (or in the systemd `EnvironmentFile`):

```
TUNR_JWT_SECRET=...   # minimum 32 chars (must match dashboard setup)
TUNR_DOMAIN=tunr.sh
PORT=8080
DATABASE_URL=         # optional
```

### 4. Systemd service

```bash
sudo tee /etc/systemd/system/tunr-relay.service << 'EOF'
[Unit]
Description=tunr Relay Server
After=network.target

[Service]
User=tunr
WorkingDirectory=/opt/tunr
EnvironmentFile=/opt/tunr/.env
ExecStart=/usr/local/bin/tunr-relay
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tunr-relay

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now tunr-relay
```

### 5. Verify

```bash
systemctl status tunr-relay
journalctl -u tunr-relay -f -n 30
curl -s http://localhost:8080/api/v1/health
```

Ensure Caddy routes `relay.tunr.sh` and `*.tunr.sh` to port `8080` on this server.
