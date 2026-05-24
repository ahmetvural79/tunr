#!/usr/bin/env bash
# update.sh — push current branch to the tunr Hetzner box and rebuild relay + landing.
#
# Usage:
#   ./update.sh                # full update: relay binary + CLI downloads + static landing
#   ./update.sh --landing-only # static landing only (no relay restart)
#   ./update.sh --relay-only   # relay binary + restart (no static landing)
#
# Requirements:
#   - SSH access to the host alias `tunr-hetzner` (set in ~/.ssh/config).
#   - On the server: /opt/tunr/src as the git checkout, /var/www/tunr for landing,
#     systemd unit `tunr-relay`, Go toolchain on PATH.

set -euo pipefail

REMOTE="${TUNR_REMOTE:-tunr-hetzner}"
SRC_DIR="${TUNR_SRC_DIR:-/opt/tunr/src}"
LANDING_DIR="${TUNR_LANDING_DIR:-/var/www/tunr}"
DOWNLOADS_DIR="${TUNR_DOWNLOADS_DIR:-/var/www/tunr/downloads}"
RELAY_SERVICE="${TUNR_RELAY_SERVICE:-tunr-relay}"
RELAY_BIN="${TUNR_RELAY_BIN:-/usr/local/bin/tunr-relay}"
HEALTH_URL="${TUNR_HEALTH_URL:-http://localhost:8080/api/v1/health}"

MODE="full"
if [[ "${1:-}" == "--landing-only" ]]; then MODE="landing"; fi
if [[ "${1:-}" == "--relay-only"   ]]; then MODE="relay";   fi

log() { printf '\033[1;35m[update]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[update]\033[0m %s\n' "$*" >&2; exit 1; }

log "Target host: $REMOTE  (mode: $MODE)"

# ── Pre-flight: verify SSH ────────────────────────────────────────────────────
if ! ssh -o BatchMode=yes -o ConnectTimeout=10 "$REMOTE" true 2>/dev/null; then
  die "SSH to $REMOTE failed. Check ~/.ssh/config and that your key is in the server's authorized_keys."
fi

# ── Push source via rsync (skips secrets and build artefacts) ─────────────────
log "Syncing source tree → $REMOTE:$SRC_DIR"
rsync -az --delete \
  --exclude='.git/' \
  --exclude='node_modules/' \
  --exclude='dist/' \
  --exclude='.env' \
  --exclude='.env.*' \
  --exclude='coverage.txt' \
  --exclude='tunr' \
  --exclude='tunr.log' \
  --exclude='sdk/node/node_modules/' \
  --exclude='sdk/node/dist/' \
  --exclude='sdk/python/dist/' \
  ./ "$REMOTE:$SRC_DIR/"

# ── Landing ───────────────────────────────────────────────────────────────────
if [[ "$MODE" != "relay" ]]; then
  log "Refreshing static landing → $LANDING_DIR"
  ssh "$REMOTE" "set -e
    mkdir -p '$LANDING_DIR'
    # Copy the static landing files but never overwrite the Next.js dashboard at /app
    rsync -a --delete --exclude='app/' '$SRC_DIR/landing/' '$LANDING_DIR/'
    # Make sure Caddy can serve them
    if id caddy >/dev/null 2>&1; then
      chown -R caddy:caddy '$LANDING_DIR'
    fi
  "
fi

# ── Relay binary + CLI downloads ──────────────────────────────────────────────
if [[ "$MODE" != "landing" ]]; then
  log "Rebuilding relay binary on $REMOTE"
  ssh "$REMOTE" "set -e
    cd '$SRC_DIR/relay'
    export PATH=\$PATH:/usr/local/go/bin
    VERSION=\$(cd '$SRC_DIR' && git describe --tags --always 2>/dev/null || echo dev)
    CGO_ENABLED=0 go build -trimpath -ldflags=\"-w -s -X main.Version=\$VERSION\" \
      -o '${RELAY_BIN}.new' ./cmd/server
    mv '${RELAY_BIN}.new' '$RELAY_BIN'
    chmod 755 '$RELAY_BIN'
    echo 'relay version:' \$('$RELAY_BIN' --help 2>&1 | head -1 || true)
  "

  log "Cross-compiling CLI downloads → $DOWNLOADS_DIR"
  ssh "$REMOTE" "set -e
    mkdir -p '$DOWNLOADS_DIR'
    cd '$SRC_DIR'
    export PATH=\$PATH:/usr/local/go/bin
    VERSION=\$(git describe --tags --always 2>/dev/null || echo dev)
    LDFLAGS=\"-w -s -X main.Version=\$VERSION\"
    CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags=\"\$LDFLAGS\" -o '$DOWNLOADS_DIR/tunr-linux-amd64'      ./cmd/tunr
    CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags=\"\$LDFLAGS\" -o '$DOWNLOADS_DIR/tunr-linux-arm64'      ./cmd/tunr
    CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags=\"\$LDFLAGS\" -o '$DOWNLOADS_DIR/tunr-darwin-amd64'     ./cmd/tunr
    CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags=\"\$LDFLAGS\" -o '$DOWNLOADS_DIR/tunr-darwin-arm64'     ./cmd/tunr
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags=\"\$LDFLAGS\" -o '$DOWNLOADS_DIR/tunr-windows-amd64.exe' ./cmd/tunr
    ls -lh '$DOWNLOADS_DIR'
  "

  log "Restarting $RELAY_SERVICE"
  ssh "$REMOTE" "systemctl daemon-reload || true
    systemctl restart '$RELAY_SERVICE'
    sleep 2
    systemctl --no-pager status '$RELAY_SERVICE' | head -15
  "
fi

# ── Verify ────────────────────────────────────────────────────────────────────
log "Probing $HEALTH_URL"
if ssh "$REMOTE" "curl -fsS --max-time 5 '$HEALTH_URL' >/dev/null"; then
  log "Health check OK"
else
  die "Health check failed. Inspect: ssh $REMOTE journalctl -u $RELAY_SERVICE -n 80"
fi

log "Done."
