#!/usr/bin/env bash
# update.sh — sync this repo to the tunr production box and rebuild the
# docker-compose stack (relay + dashboard) and refresh the static landing.
#
# The server (91.98.42.7) runs everything as docker compose project "tunr"
# under /opt/tunr:
#   docker-compose.yml            postgres + relay   (relay built from src/relay)
#   docker-compose.dashboard.yml  dashboard          (Next.js, built from src/landing/app)
#   docker-compose.caddy.yml      caddy              (TLS + reverse proxy, ports 80/443)
# Static marketing HTML is served by caddy from the /var/www/tunr/landing bind mount.
# (CLI binary distribution is NOT served from this box — there is no Go toolchain
# on the host and Caddy exposes no /downloads route, so that step was removed.)
#
# Usage:
#   ./update.sh                  # full: sync + landing + migrations + relay + dashboard
#   ./update.sh --landing-only   # static landing only (no container restarts)
#   ./update.sh --relay-only     # migrations + relay rebuild/restart
#   ./update.sh --dashboard-only # dashboard rebuild/restart (e.g. after editing its .env.local)
#
# Requirements:
#   - SSH host alias `tunr-prod` in ~/.ssh/config → 91.98.42.7
#     (User root, IdentityFile ~/.ssh/id_ed25519). Override with
#     TUNR_REMOTE=root@91.98.42.7 to bypass the alias.
#   - On the server: /opt/tunr (compose project root), /opt/tunr/src the rsync
#     checkout, Docker Engine + Compose v2. The server keeps its own secrets in
#     /opt/tunr/.env and /opt/tunr/src/landing/app/.env.local — this script never
#     syncs or overwrites those (.env / .env.* are excluded from rsync), so
#     server-side config such as ADMIN_EMAILS survives every deploy.

set -euo pipefail

REMOTE="${TUNR_REMOTE:-tunr-prod}"
COMPOSE_DIR="${TUNR_COMPOSE_DIR:-/opt/tunr}"
SRC_DIR="${TUNR_SRC_DIR:-/opt/tunr/src}"
LANDING_DIR="${TUNR_LANDING_DIR:-/var/www/tunr/landing}"
HEALTH_URL="${TUNR_HEALTH_URL:-http://127.0.0.1:8080/api/v1/health}"

# compose -f flag sets. The base file holds postgres+relay; the dashboard add-on
# shares the same project name ("tunr") so services compose together. Caddy
# (docker-compose.caddy.yml) is left running untouched by this script.
DC_BASE="-f docker-compose.yml"
DC_DASH="-f docker-compose.yml -f docker-compose.dashboard.yml"

MODE="full"
case "${1:-}" in
  --landing-only)   MODE="landing"   ;;
  --relay-only)     MODE="relay"     ;;
  --dashboard-only) MODE="dashboard" ;;
  "")               MODE="full"      ;;
  *) printf 'unknown flag: %s\n' "$1" >&2; exit 2 ;;
esac

log() { printf '\033[1;35m[update]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[update]\033[0m %s\n' "$*" >&2; exit 1; }

log "Target host: $REMOTE  (mode: $MODE)"

# ── Pre-flight: verify SSH ────────────────────────────────────────────────────
if ! ssh -o BatchMode=yes -o ConnectTimeout=10 "$REMOTE" true 2>/dev/null; then
  die "SSH to $REMOTE failed. Check ~/.ssh/config (Host tunr-prod → 91.98.42.7, User root) and that your key is in the server's authorized_keys."
fi

# ── Push source via rsync (skips secrets and build artefacts) ─────────────────
# Intentionally NOT using --delete: the server keeps files locally that aren't in
# the repo (e.g. .env, .env.local, locally-applied migration journals).
log "Syncing source tree → $REMOTE:$SRC_DIR"
rsync -az \
  --exclude='.git/' \
  --exclude='node_modules/' \
  --exclude='dist/' \
  --exclude='.next/' \
  --exclude='.env' \
  --exclude='.env.*' \
  --exclude='.DS_Store' \
  --exclude='coverage.txt' \
  --exclude='tunr' \
  --exclude='tunr.log' \
  --exclude='sdk/node/node_modules/' \
  --exclude='sdk/node/dist/' \
  --exclude='sdk/python/dist/' \
  --exclude='__pycache__/' \
  ./ "$REMOTE:$SRC_DIR/"

# ── Static landing ────────────────────────────────────────────────────────────
# Caddy serves these files live from the bind mount, so no restart is needed.
# Never copy app/ — that's the Next.js dashboard, which runs as its own container.
if [[ "$MODE" == "full" || "$MODE" == "landing" ]]; then
  log "Refreshing static landing → $LANDING_DIR"
  ssh "$REMOTE" "set -e
    mkdir -p '$LANDING_DIR'
    rsync -a --exclude='app/' '$SRC_DIR/landing/' '$LANDING_DIR/'
  "
fi

# ── Apply pending DB migrations (idempotent) ──────────────────────────────────
if [[ "$MODE" == "full" || "$MODE" == "relay" ]]; then
  log "Applying relay/migrations/*.sql via docker compose exec postgres"
  ssh "$REMOTE" "set -e
    cd '$COMPOSE_DIR'
    if [ -n \"\$(docker compose $DC_BASE ps -q postgres 2>/dev/null)\" ]; then
      for f in \$(ls '$SRC_DIR/relay/migrations'/*.sql 2>/dev/null | sort); do
        echo \"  applying \$(basename \$f)\"
        docker compose $DC_BASE exec -T postgres psql -U tunr -d tunr -v ON_ERROR_STOP=1 < \"\$f\" > /dev/null
      done
    else
      echo '  postgres container not running, skipping migrations'
    fi
  "
fi

# ── Relay container (rebuild image from src/relay, recreate) ──────────────────
if [[ "$MODE" == "full" || "$MODE" == "relay" ]]; then
  log "Rebuilding + restarting relay container"
  ssh "$REMOTE" "set -e
    cd '$COMPOSE_DIR'
    docker compose $DC_BASE build relay
    docker compose $DC_BASE up -d relay
  "
fi

# ── Dashboard container (rebuild from src/landing/app; picks up .env.local) ───
if [[ "$MODE" == "full" || "$MODE" == "dashboard" ]]; then
  log "Rebuilding + restarting dashboard container"
  ssh "$REMOTE" "set -e
    cd '$COMPOSE_DIR'
    docker compose $DC_DASH build dashboard
    docker compose $DC_DASH up -d --no-deps dashboard
  "
fi

# ── Verify ────────────────────────────────────────────────────────────────────
if [[ "$MODE" != "landing" ]]; then
  log "Probing relay health ($HEALTH_URL)"
  if ssh "$REMOTE" "curl -fsS --max-time 5 '$HEALTH_URL' >/dev/null"; then
    log "Relay health OK"
  else
    die "Relay health failed. Inspect: ssh $REMOTE 'cd $COMPOSE_DIR && docker compose $DC_BASE logs --tail 80 relay'"
  fi
fi

if [[ "$MODE" == "full" || "$MODE" == "dashboard" ]]; then
  log "Dashboard container status:"
  ssh "$REMOTE" "docker ps --filter name=tunr-dashboard-1 --format '  {{.Names}}: {{.Status}}'"
fi

log "Done."
