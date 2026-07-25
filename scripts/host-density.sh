#!/usr/bin/env bash
# host-density.sh — host-layer prerequisites for the tunr density levers
# (Yoğunluk planı Faz 1, lever L1).
#
# This sets up compressed RAM swap (zram) and the VM tuning that goes with it.
# It is the FIRST thing to run, because the other levers depend on it:
#
#   • memory.reclaim (reclaim-on-pause, the 3× density win) needs somewhere to
#     put the pages it evicts. With no swap the kernel finds nothing to reclaim
#     and a sleeping app keeps its full resident set — exactly today's behaviour.
#
#   • memory.high (soft cap) is WORSE than useless without swap. The kernel
#     cannot contain a cgroup it has nowhere to page out to, so pressure
#     escalates past the soft cap into a system-wide OOM instead of throttling
#     the one app responsible. (Chris Down / LKML.) Ordering is not optional:
#     zram first, soft limits second.
#
# Why zram rather than a disk swapfile: pages go to a compressed block device in
# RAM, so a "swap in" is a zstd decompress (microseconds) rather than a disk read
# (milliseconds). An idle application heap compresses ~3:1. Waking a WARM app
# therefore stays fast, which is the whole point — swap that hurts wake latency
# would trade the metric users feel for one they don't.
#
# Idempotent: safe to re-run. Run as root on the app-hosting box.
#
#   sudo bash scripts/host-density.sh
#   sudo bash scripts/host-density.sh --size 12288    # explicit zram size in MB

set -euo pipefail

ZRAM_SIZE_MB="${TUNR_ZRAM_SIZE_MB:-}"
ZRAM_ALGO="${TUNR_ZRAM_ALGO:-zstd}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --size) ZRAM_SIZE_MB="$2"; shift 2 ;;
    --algo) ZRAM_ALGO="$2";    shift 2 ;;
    -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
    *) printf 'unknown flag: %s\n' "$1" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[1;35m[density]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[density]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[density]\033[0m %s\n' "$*" >&2; exit 1; }

[[ "$(id -u)" -eq 0 ]] || die "run as root (sudo bash scripts/host-density.sh)"
[[ "$(uname -s)" == "Linux" ]] || die "Linux only — the levers are cgroup v2 features"

# ── 0. Preflight: the kernel features the levers require ─────────────────────
log "Checking kernel capabilities"

if [[ ! -f /sys/fs/cgroup/cgroup.controllers ]]; then
  die "cgroup v2 is not the active hierarchy. Boot with systemd.unified_cgroup_hierarchy=1 (Ubuntu 22.04+ and Debian 12+ default to v2)."
fi
log "  cgroup v2: OK"

KVER="$(uname -r)"
KMAJ="${KVER%%.*}"; KREST="${KVER#*.}"; KMIN="${KREST%%.*}"
# memory.reclaim landed in 5.19; without it reclaim-on-pause cannot work and
# the WARM state degrades to a plain pause.
if (( KMAJ > 5 || (KMAJ == 5 && KMIN >= 19) )); then
  log "  kernel $KVER: memory.reclaim supported"
else
  warn "  kernel $KVER is older than 5.19 — memory.reclaim unavailable."
  warn "  WARM apps will keep their full RSS. Upgrade the kernel for the 3× density win."
fi

if [[ -f /proc/pressure/memory ]]; then
  log "  PSI: OK (pressure-based safety valve available)"
else
  warn "  PSI unavailable (no CONFIG_PSI) — the sweeper's pressure valve stays disarmed."
fi

# ── 1. Size the zram device ──────────────────────────────────────────────────
# Default to 75% of RAM of *uncompressed* capacity. At the ~3:1 zstd ratio an
# idle heap achieves, that costs roughly 25% of RAM in real pages while giving
# the reclaim path plenty of room. Going much higher risks the compressed data
# itself becoming the memory problem.
MEM_TOTAL_MB=$(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo)
if [[ -z "$ZRAM_SIZE_MB" ]]; then
  ZRAM_SIZE_MB=$(( MEM_TOTAL_MB * 3 / 4 ))
fi
log "RAM: ${MEM_TOTAL_MB} MB → zram capacity: ${ZRAM_SIZE_MB} MB (${ZRAM_ALGO})"

# ── 2. zram swap ─────────────────────────────────────────────────────────────

# ensure_zram_module makes /dev/zram0 possible.
#
# Minimal cloud images (Hetzner's Ubuntu among them) ship linux-image-virtual,
# whose module set does NOT include zram — it lives in linux-modules-extra. The
# generator's unit then fails with a bare "dependency failed", because the
# device it wants never appears. Install for the RUNNING kernel specifically:
# the generic metapackage tracks the newest installed kernel, which on a box
# that is overdue a reboot is not the one currently running.
ensure_zram_module() {
  if modprobe zram 2>/dev/null; then
    log "  zram module loaded"
  else
    log "  zram module missing — installing linux-modules-extra-$(uname -r)"
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "linux-modules-extra-$(uname -r)" >/dev/null 2>&1 \
      || warn "  could not install linux-modules-extra-$(uname -r)"
    modprobe zram 2>/dev/null || die "zram module unavailable for kernel $(uname -r). Install linux-modules-extra for this kernel, or reboot into one that has it."
    log "  zram module loaded"
  fi
  # Load it on every boot, otherwise the generator fails again after a reboot.
  echo zram > /etc/modules-load.d/zram.conf
}

# reset_zram_device clears a half-initialised device.
#
# Once zram0 has a disksize, comp_algorithm becomes read-only and the generator
# aborts with EBUSY. A partial earlier run leaves exactly that state, so reset
# before configuring rather than after failing.
reset_zram_device() {
  [[ -e /sys/block/zram0 ]] || return 0
  swapoff /dev/zram0 2>/dev/null || true
  echo 1 > /sys/block/zram0/reset 2>/dev/null || true
}

# Preferred path is systemd's zram-generator (declarative, survives reboot).
# Fall back to a hand-rolled unit when the package isn't available.
setup_zram_generator() {
  command -v systemctl >/dev/null 2>&1 || return 1
  if [[ ! -f /usr/lib/systemd/system-generators/zram-generator ]]; then
    log "Installing systemd-zram-generator"
    DEBIAN_FRONTEND=noninteractive apt-get update -qq || return 1
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd-zram-generator >/dev/null 2>&1 \
      || apt-get install -y -qq zram-tools >/dev/null 2>&1 || return 1
  fi
  [[ -f /usr/lib/systemd/system-generators/zram-generator ]] || return 1

  cat > /etc/systemd/zram-generator.conf <<EOF
# Managed by tunr scripts/host-density.sh — Faz 1 lever L1.
# zram is the prerequisite for reclaim-on-pause and for memory.high working at all.
[zram0]
zram-size = ${ZRAM_SIZE_MB}
compression-algorithm = ${ZRAM_ALGO}
swap-priority = 100
EOF
  systemctl daemon-reload
  reset_zram_device
  systemctl restart systemd-zram-setup@zram0.service 2>/dev/null || true
  return 0
}

setup_zram_manual() {
  log "Falling back to a manual zram systemd unit"
  reset_zram_device

  cat > /etc/systemd/system/tunr-zram.service <<EOF
[Unit]
Description=tunr zram swap (density lever L1)
DefaultDependencies=no
Before=swap.target
After=systemd-modules-load.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStartPre=-/sbin/modprobe zram num_devices=1
ExecStart=/bin/sh -c 'swapoff /dev/zram0 2>/dev/null || true; \
  echo 1 > /sys/block/zram0/reset 2>/dev/null || true; \
  echo ${ZRAM_ALGO} > /sys/block/zram0/comp_algorithm; \
  echo ${ZRAM_SIZE_MB}M > /sys/block/zram0/disksize; \
  /sbin/mkswap /dev/zram0; \
  /sbin/swapon -p 100 /dev/zram0'
ExecStop=/bin/sh -c 'swapoff /dev/zram0 || true'

[Install]
WantedBy=swap.target
EOF
  systemctl daemon-reload
  systemctl enable --now tunr-zram.service
}

log "Configuring zram swap"
ensure_zram_module
if setup_zram_generator; then
  log "  zram configured via systemd-zram-generator"
else
  setup_zram_manual
fi

# ── 3. VM tuning for a zram-backed system ────────────────────────────────────
# swappiness=180: the kernel's own recommendation once swap is RAM-backed and
#   therefore cheap. The default 60 assumes swap means a slow disk.
# page-cluster=0: read one page at a time. Batched readahead is a win for disk
#   swap and a pure latency cost for zram, where each page is an independent
#   decompress — this directly protects wake latency.
# watermark_boost_factor=0: stop the kernel over-reclaiming after fragmentation
#   events, which shows up as latency spikes under memory pressure.
log "Applying VM tuning"
cat > /etc/sysctl.d/60-tunr-density.conf <<'EOF'
# Managed by tunr scripts/host-density.sh — tuned for zram-backed swap.
vm.swappiness = 180
vm.page-cluster = 0
vm.watermark_boost_factor = 0
EOF
sysctl -q --load /etc/sysctl.d/60-tunr-density.conf

# ── 4. Protect the control plane from the OOM killer ─────────────────────────
# App containers get oom_score_adj=500 from the DockerDriver, which already
# makes them the preferred victim. This pushes the other way for the processes
# whose loss would be an outage rather than an incident.
log "Lowering OOM priority for the control plane"
for svc in dockerd containerd; do
  pid=$(pgrep -x "$svc" 2>/dev/null | head -1 || true)
  [[ -n "$pid" ]] && echo -500 > "/proc/$pid/oom_score_adj" 2>/dev/null || true
done
for name in tunr-relay-1 tunr-postgres-1 tunr-runner-1; do
  pid=$(docker inspect -f '{{.State.Pid}}' "$name" 2>/dev/null || true)
  if [[ -n "$pid" && "$pid" != "0" ]]; then
    echo -500 > "/proc/$pid/oom_score_adj" 2>/dev/null \
      && log "  protected $name (pid $pid)" || true
  fi
done
warn "  oom_score_adj on containers resets when they restart."
warn "  Make it permanent by adding 'oom_score_adj: -500' to the relay/postgres/runner services in docker-compose.yml."

# ── 5. Verify ────────────────────────────────────────────────────────────────
log "Verifying"
if ! swapon --show 2>/dev/null | grep -q zram; then
  die "zram swap is not active. Inspect: swapon --show; journalctl -u systemd-zram-setup@zram0 -n 50"
fi
swapon --show

if command -v zramctl >/dev/null 2>&1; then
  log "zram device state:"
  zramctl || true
fi

SWAP_TOTAL_MB=$(awk '/SwapTotal/ {printf "%d", $2/1024}' /proc/meminfo)
cat <<EOF

$(log "Host density layer ready.")

  RAM:            ${MEM_TOTAL_MB} MB
  zram swap:      ${SWAP_TOTAL_MB} MB (algorithm: ${ZRAM_ALGO}, priority 100)
  swappiness:     $(sysctl -n vm.swappiness)
  page-cluster:   $(sysctl -n vm.page-cluster)
  cgroup v2:      yes
  kernel:         ${KVER}

Next:
  1. The runner needs to SEE this hierarchy. It runs as a container, so
     docker-compose.runner.yml must give it:
         volumes:
           - /sys/fs/cgroup:/sys/fs/cgroup:rw    # memory.reclaim / memory.high writes
           - /proc:/host/proc:ro                 # host PSI + meminfo
         environment:
           - TUNR_PROC_ROOT=/host/proc
     Without the rw cgroup mount every density lever silently no-ops — the
     runner logs "cgroup levers OFF" at startup when that happens.
     (No 'pid: host' needed: the cgroup path resolver falls back to the
     standard Docker layout, which the systemd cgroup driver provides.)

  2. Deploy and confirm:  ./update.sh --relay-only
     Expect "density levers: ON" from the script, and in
     'docker logs tunr-runner': "cgroup levers ON" plus a non-zero swap line.

  3. Watch the payoff (RUNNER_SECRET is in /opt/tunr/.env):
       curl -H "Authorization: Bearer \$RUNNER_SECRET" http://<runner-ip>:9091/v1/stats
     totals.reclaim_saving_percent is the WARM-state win: the share of app RAM
     that zram gave back. It only moves once apps have gone idle and been slept.
EOF
