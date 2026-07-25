#!/usr/bin/env bash
# tunr-zram-ensure.sh — keep zram swap working across kernel upgrades.
#
# zram is the prerequisite for the whole density story: without swap,
# reclaim-on-pause has nowhere to evict to, and memory.high is deliberately left
# unset (a soft cap the kernel cannot contain escalates to a system-wide OOM).
# Losing it silently drops the box back to pre-Faz-1 capacity.
#
# It is easy to lose. Ubuntu's minimal cloud images (linux-image-virtual, which
# is what Hetzner ships) do NOT include the zram module — it lives in
# linux-modules-extra-<version>, which is versioned PER KERNEL. So every kernel
# upgrade silently removes zram until that package is installed for the new
# version, and there is no `-virtual` metapackage to pull it in automatically.
#
# The symptom is quiet: the box boots fine, swap is simply 0, and the only
# signal is a warning in the runner's startup log. Hence this unit — it runs on
# every boot, does nothing when swap is already up, and repairs it when a
# kernel upgrade has taken the module away.
#
# Usage:
#   tunr-zram-ensure.sh              # check/repair now
#   tunr-zram-ensure.sh --install    # install + enable the systemd unit

set -uo pipefail   # deliberately NOT -e: this must never block boot

UNIT=/etc/systemd/system/tunr-zram-ensure.service
SELF=/usr/local/bin/tunr-zram-ensure.sh

log()  { printf '\033[1;35m[zram-ensure]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[zram-ensure]\033[0m %s\n' "$*" >&2; }

if [[ "$(id -u)" -ne 0 ]]; then
  warn "run as root"; exit 1
fi

if [[ "${1:-}" == "--install" ]]; then
  install -m 0755 "$0" "$SELF"
  cat > "$UNIT" <<EOF
[Unit]
Description=tunr: ensure zram swap survives kernel upgrades
# Needs the network in case the module package must be fetched, and must run
# before anything starts sizing memory against available swap.
After=network-online.target local-fs.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
# Never let a repair attempt hold up the boot.
TimeoutStartSec=300
ExecStart=$SELF

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable tunr-zram-ensure.service >/dev/null
  log "Enabled tunr-zram-ensure.service (runs on every boot)"
fi

# ── Already healthy? Then do nothing. ────────────────────────────────────────
if swapon --show 2>/dev/null | grep -q zram; then
  log "zram swap active — nothing to do ($(awk '/SwapTotal/ {printf "%d MB", $2/1024}' /proc/meminfo))"
  exit 0
fi

log "zram swap not active — repairing for kernel $(uname -r)"

# ── Make sure the module exists for THIS kernel ──────────────────────────────
if ! modprobe zram 2>/dev/null; then
  PKG="linux-modules-extra-$(uname -r)"
  log "module missing; installing $PKG"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq >/dev/null 2>&1
  if ! apt-get install -y -qq "$PKG" >/dev/null 2>&1; then
    warn "could not install $PKG — zram stays OFF."
    warn "  Capacity falls back to pre-Faz-1 (sleeping apps keep their full RSS)."
    warn "  The runner logs 'no swap configured' at startup while this is true."
    exit 0   # not fatal: the box still serves, just less densely
  fi
  modprobe zram 2>/dev/null || { warn "modprobe zram still failing"; exit 0; }
fi
log "zram module loaded"

# Load on every boot, so the generator's device dependency resolves.
echo zram > /etc/modules-load.d/zram.conf

# ── Bring the device up ──────────────────────────────────────────────────────
# A half-initialised zram0 makes comp_algorithm read-only and the generator
# aborts with EBUSY, so clear it before configuring.
if [[ -e /sys/block/zram0 ]]; then
  swapoff /dev/zram0 2>/dev/null
  echo 1 > /sys/block/zram0/reset 2>/dev/null
fi
systemctl daemon-reload
systemctl restart systemd-zram-setup@zram0.service 2>/dev/null
sleep 2

if swapon --show 2>/dev/null | grep -q zram; then
  log "zram swap restored: $(awk '/SwapTotal/ {printf "%d MB", $2/1024}' /proc/meminfo)"
else
  warn "zram still not active — inspect: journalctl -u systemd-zram-setup@zram0 -n 30"
fi
exit 0
