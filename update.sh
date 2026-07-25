#!/usr/bin/env bash
# update.sh — sync this repo to the tunr production box and rebuild the
# docker-compose stack (relay + dashboard) and refresh the static landing.
#
# The server (167.233.102.96) runs everything as docker compose project "tunr"
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
#   - SSH host alias `tunr-prod` in ~/.ssh/config → 167.233.102.96
#     (User root, IdentityFile ~/.ssh/id_ed25519). Override with
#     TUNR_REMOTE=root@167.233.102.96 to bypass the alias.
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
APPS_NETWORK="${TUNR_APPS_NETWORK:-tunr-apps}"  # pivot: isolated cloud-runner bridge

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
  die "SSH to $REMOTE failed. Check ~/.ssh/config (Host tunr-prod → 167.233.102.96, User root) and that your key is in the server's authorized_keys."
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

# ── Cloud runner: network + runner sidecar + relay attachment + relay-allow ──
# Idempotent every deploy — self-heals the relay→app path. Host-level install
# (Docker + gVisor) is done once by scripts/server-setup.sh.
#   1) tunr-apps network (icc=false: tenants can't reach each other)
#   2) tunr-runner sidecar up (drives Docker: build/run/wake app containers)
#   3) relay attached to tunr-apps (so it can dial app container IPs)
#   4) DOCKER-USER "relay-allow" rule: icc=false blocks relay→app too, so we
#      explicitly permit the relay's IP → the apps bridge.
if [[ "$MODE" == "full" || "$MODE" == "relay" ]]; then
  log "Ensuring cloud-runner (network + sidecar + relay attach + relay-allow)"
  ssh "$REMOTE" "set -e
    cd '$COMPOSE_DIR'
    command -v runsc >/dev/null 2>&1 || echo '  [warn] gVisor (runsc) not installed — run scripts/server-setup.sh'
    if ! docker network inspect '$APPS_NETWORK' >/dev/null 2>&1; then
      echo '  creating network $APPS_NETWORK (icc=false)'
      docker network create --opt com.docker.network.bridge.enable_icc=false '$APPS_NETWORK' >/dev/null
    fi
    # The runner compose file is version-controlled now (it carries the host
    # cgroup/proc mounts the density levers need). Sync it, keeping a timestamped
    # backup of whatever was there — the server copy may have local edits.
    if [ -f '$SRC_DIR/docker-compose.runner.yml' ]; then
      if [ -f docker-compose.runner.yml ] && ! cmp -s '$SRC_DIR/docker-compose.runner.yml' docker-compose.runner.yml; then
        cp docker-compose.runner.yml \"docker-compose.runner.yml.bak.\$(date +%Y%m%d-%H%M%S)\"
        echo '  backed up existing docker-compose.runner.yml'
      fi
      cp '$SRC_DIR/docker-compose.runner.yml' docker-compose.runner.yml
    fi
    if [ -f docker-compose.runner.yml ]; then
      docker compose $DC_BASE -f docker-compose.runner.yml up -d --build runner >/dev/null 2>&1 && echo '  runner sidecar up' || echo '  [warn] runner up failed'
      # Faz 1 levers are silent when they fail — surface the runner's verdict.
      sleep 2
      if docker logs tunr-runner 2>&1 | tail -30 | grep -q 'cgroup levers ON'; then
        echo '  density levers: ON'
      else
        echo '  [warn] density levers OFF — sleeping apps keep their full RSS.'
        echo '  [warn]   check: docker logs tunr-runner | head -20'
        echo '  [warn]   host prerequisite: bash $SRC_DIR/scripts/host-density.sh'
      fi
    else
      echo '  [warn] docker-compose.runner.yml missing — deploy pipeline offline'
    fi
    rid=\$(docker compose $DC_BASE ps -q relay 2>/dev/null || true)
    if [ -n \"\$rid\" ]; then
      docker inspect -f '{{range \$k,\$v := .NetworkSettings.Networks}}{{\$k}} {{end}}' \"\$rid\" | grep -qw '$APPS_NETWORK' \
        || { docker network connect '$APPS_NETWORK' \"\$rid\"; echo '  attached relay to $APPS_NETWORK'; }
      BR=\"br-\$(docker network inspect '$APPS_NETWORK' -f '{{.Id}}' | cut -c1-12)\"
      RIP=\$(docker inspect -f '{{(index .NetworkSettings.Networks \"$APPS_NETWORK\").IPAddress}}' \"\$rid\")
      if [ -n \"\$RIP\" ]; then
        iptables -D DOCKER-USER -i \"\$BR\" -o \"\$BR\" -s \"\$RIP/32\" -j ACCEPT 2>/dev/null || true
        iptables -I DOCKER-USER -i \"\$BR\" -o \"\$BR\" -s \"\$RIP/32\" -j ACCEPT
        echo \"  relay-allow iptables rule set (\$RIP → $APPS_NETWORK)\"
      fi
    fi
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
