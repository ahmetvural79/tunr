#!/usr/bin/env bash
# tunr-net-heal.sh — restore the relay -> app-container network path.
#
# The `tunr-apps` bridge runs with icc=false so tenant containers cannot reach
# each other. That rule is indiscriminate: it blocks the RELAY too. The relay is
# allowed through by an explicit DOCKER-USER ACCEPT keyed on its current IP.
#
# Two things make that rule fragile:
#
#   • iptables rules are in-memory. A host reboot wipes DOCKER-USER, and nothing
#     puts the rule back — the stack comes up looking healthy while every cloud
#     app returns 503, because the relay cannot dial any of them.
#   • the relay's IP on tunr-apps is assigned by Docker and changes whenever the
#     container is recreated, so a statically saved rule would go stale anyway.
#
# So the rule has to be *recomputed*, not persisted. update.sh already does this
# after every deploy; this script is the same logic on the boot path, installed
# as a systemd oneshot (tunr-net-heal.service).
#
# Idempotent: deletes any prior copy of the rule before inserting, so repeated
# runs cannot stack duplicates.
#
# Usage:
#   tunr-net-heal.sh              # heal now
#   tunr-net-heal.sh --install    # install + enable the systemd unit, then heal

set -euo pipefail

APPS_NETWORK="${TUNR_APPS_NETWORK:-tunr-apps}"
RELAY_CONTAINER="${TUNR_RELAY_CONTAINER:-tunr-relay-1}"
UNIT=/etc/systemd/system/tunr-net-heal.service
SELF=/usr/local/bin/tunr-net-heal.sh

log()  { printf '\033[1;35m[net-heal]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[net-heal]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[net-heal]\033[0m %s\n' "$*" >&2; exit 1; }

[[ "$(id -u)" -eq 0 ]] || die "run as root"

install_unit() {
  log "Installing $UNIT"
  cat > "$UNIT" <<EOF
[Unit]
Description=tunr: restore relay -> tunr-apps network path (icc=false allow rule)
# The rule is keyed on the relay's Docker-assigned IP, so this must run after
# Docker has started the containers, not merely after the daemon socket exists.
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
# Containers may still be starting when docker.service reports ready; the
# script retries, and this gives it room to.
ExecStart=$SELF

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable tunr-net-heal.service >/dev/null
  log "Enabled tunr-net-heal.service (runs on every boot)"
}

if [[ "${1:-}" == "--install" ]]; then
  install -m 0755 "$0" "$SELF"
  install_unit
fi

# ── Wait for Docker and the relay container ──────────────────────────────────
# At boot this races container startup, so poll rather than fail. 60s is well
# past the observed startup time and still bounded.
for i in $(seq 1 60); do
  if docker network inspect "$APPS_NETWORK" >/dev/null 2>&1 \
     && [ -n "$(docker ps -q -f "name=^${RELAY_CONTAINER}$" 2>/dev/null)" ]; then
    break
  fi
  [[ $i -eq 60 ]] && die "timed out waiting for docker + $RELAY_CONTAINER"
  sleep 2
done

# ── Attach the relay to the apps network ─────────────────────────────────────
if ! docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' \
      "$RELAY_CONTAINER" | grep -qw "$APPS_NETWORK"; then
  docker network connect "$APPS_NETWORK" "$RELAY_CONTAINER"
  log "Attached $RELAY_CONTAINER to $APPS_NETWORK"
  sleep 1
fi

# ── Recompute and reinstall the allow rule ───────────────────────────────────
BR="br-$(docker network inspect "$APPS_NETWORK" -f '{{.Id}}' | cut -c1-12)"
RIP=$(docker inspect -f "{{(index .NetworkSettings.Networks \"$APPS_NETWORK\").IPAddress}}" "$RELAY_CONTAINER")
[[ -n "$RIP" ]] || die "relay has no IP on $APPS_NETWORK"

# Drop stale copies for ANY source: an old rule pointing at an IP now owned by
# a tenant container would hand that tenant the relay's cross-container access.
while read -r src; do
  [[ -n "$src" ]] || continue
  iptables -D DOCKER-USER -i "$BR" -o "$BR" -s "$src" -j ACCEPT 2>/dev/null || true
done < <(iptables -S DOCKER-USER 2>/dev/null \
         | awk -v br="$BR" '$0 ~ ("-i " br) && /-j ACCEPT/ {for(i=1;i<=NF;i++) if($i=="-s") print $(i+1)}')

iptables -I DOCKER-USER -i "$BR" -o "$BR" -s "$RIP/32" -j ACCEPT
log "relay-allow rule set ($RIP -> $APPS_NETWORK via $BR)"

# ── Verify ───────────────────────────────────────────────────────────────────
if iptables -S DOCKER-USER | grep -q -- "-s $RIP/32"; then
  log "OK"
else
  die "rule did not take effect — inspect: iptables -S DOCKER-USER"
fi
