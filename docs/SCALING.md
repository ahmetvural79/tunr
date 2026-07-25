# Scaling tunr Cloud — adding capacity and new servers

> When to add a machine, which kind to add, and exactly how to add it.
> Companion to the density work in `CLAUDE.md` ("Cloud density levers").

---

## 0. The one-paragraph version

tunr uses a **pool**, not shards. An app has no permanent home: `apps.current_node_id`
records where it is running *right now*, not where it belongs. Because ~95% of apps
are asleep at any moment — costing ~20 MB and, once cold-stopped, nothing but disk —
deciding where an app wakes up is nearly free. That makes adding a node a
configuration change rather than a migration, and removing one harmless to every
sleeping app.

So the question is never "how do we reshard?" It is only: **which resource ran out,
and what do we add?**

---

## 1. Don't add a server yet

Adding a machine is the *last* lever, not the first. Software levers are cheaper,
faster, and reversible. Work down this list before buying hardware:

| Lever | Gain | Cost | Where |
|---|---|---|---|
| zram not enabled | **~2.5× sleeping capacity** | 10 min | `scripts/host-density.sh` |
| `TUNR_IDLE_SLEEP` still high | more apps cold at once | 1 min | env var |
| `TUNR_IDLE_STOP` still high | frees RAM entirely | 1 min | env var |
| Nixpacks images (~886 MB) | ~5× less disk per app | per-app | use the slim buildpacks |
| Build cache growth | tens of GB | automatic | runner prunes every 6h |

A box showing `swap_total_bytes: 0` in `/api/v1/density` is running at roughly
**40% of its real capacity**. Fix that before costing a new server.

> **Check first:** `curl -H "Authorization: Bearer $TUNR_METRICS_TOKEN" \
> https://<relay>/api/v1/density | jq '.host, .routes.by_state'`

---

## 2. Trigger table — what to add, and when

Decide from metrics, not from feel. Use the **7-day p95**, not a spike. All figures
come from `node_metrics` (written every minute) and `/api/v1/density`.

| Saturating resource | Trigger (7-day p95) | Add | Gain |
|---|---|---|---|
| **RAM (hot ceiling)** | `mem_effective / mem_total > 70%` **or** `mem_pressure > 10` | **WORKER** | ~+450 hot-equivalent apps |
| **Disk** | `disk_used > 70%` | **WORKER** (or grow the volume) | ~+9.000 hosted apps |
| **Build queue** | `build_queue_wait_p95 > 60s` | **BUILDER** | +6–8 parallel builds |
| **Wake latency** | `wake.p95 > 800ms` *and* `cpu_pressure` high | **WORKER** | restore parallelism |
| **Edge CPU / TLS** | `edge.cpu > 50%` | **EDGE** | ~10× request capacity |
| **HA (not capacity)** | only one EDGE exists | **EDGE #2** | removes the SPOF |
| **Database** | `pg.cpu > 60%` or `pg.conn > 80%` | PgBouncer → then split DATA | control plane headroom |
| **Geographic latency** | regional `ttfb.p95 > 300ms` | regional **EDGE + WORKER** | regional placement |

**Golden rule:** if several axes saturate at once, **split the BUILDER off first.**
It is the cheapest machine, the noisiest neighbour, and the least critical — if it
dies, deploys pause and serving is untouched.

**Anti-rule:** running four nodes when one would do turns tunr into Coolify. The
whole product claim is density. Resist splitting early.

---

## 3. Split order

```
      1. BUILDER          2. DATA             3. WORKER          4. EDGE
      noisiest,           Postgres + CAS      the actual         removes the
      least critical      off the box         capacity           SPOF
```

Each split is independent and each is reversible by pointing the config back.

---

## 4. Sizing

tunr's bottleneck is **RAM, not CPU** — idle apps have plenty of spare cycles. When
comparing machines, optimise for **RAM per currency unit**, not vCPU.

| Role | Suggested spec | Yields |
|---|---|---|
| **WORKER** (recommended) | 8 vCPU / 32 GB / 640 GB | ~450 hot-equiv, ~9.000 hosted |
| WORKER (small) | 4 vCPU / 16 GB / 320 GB | ~200 hot-equiv, ~4.000 hosted |
| **BUILDER** | 8 vCPU / 16 GB / 240 GB NVMe | 6–8 parallel builds |
| **EDGE** | 2 vCPU / 4 GB | ~10.000 req/s proxy |
| **DATA** | 4 vCPU / 16 GB / 1 TB | Postgres + object store |

Measured reference (this codebase, production): a WARM app costs **~20 MB** of real
RAM at a ~3:1 zram ratio, wake p50 **~150 ms**, cold start **~140 ms**.

---

## 5. Adding a BUILDER (do this first)

The runner already separates the two jobs — same binary, one process per role — so
this needs no code change.

**On the current box**, stop building:

```yaml
# docker-compose.runner.yml
environment:
  TUNR_RUNNER_ROLE: agent      # serving only; /v1/deploy is no longer mounted
```

**On the new box:**

```bash
sudo bash scripts/server-setup.sh      # docker + gVisor + tunr-apps network
sudo bash scripts/host-density.sh      # zram (harmless here, keeps hosts uniform)
```

Then run the runner with `TUNR_RUNNER_ROLE: builder` and the same `RUNNER_SECRET`.

> **Prerequisite — do not skip.** A builder only helps if the image it produces is
> reachable from the machine that *runs* it. Today images live in the local Docker
> storage of whichever host built them. Before splitting the builder you need a
> registry (start simple: `registry:2` on the DATA node) and the runner pushing to
> it. Splitting without this produces images nobody can run.

---

## 6. Adding a WORKER

This is the step where the cluster becomes real.

### 6.1 Prepare the machine

```bash
sudo bash scripts/server-setup.sh      # docker + gVisor(runsc) + tunr-apps (icc=false)
sudo bash scripts/host-density.sh      # zram + swappiness — REQUIRED, see below
sudo bash scripts/tunr-net-heal.sh --install
sudo bash scripts/tunr-zram-ensure.sh --install
```

`host-density.sh` is not optional on a worker. Without swap, `memory.reclaim` has
nowhere to evict and `memory.high` is deliberately left unset (a soft cap the kernel
cannot contain escalates to a *system-wide* OOM). A worker without zram runs at
roughly 40% of its rated capacity.

### 6.2 Register the node

```sql
INSERT INTO nodes (id, role, region, url, status, cpu_cores, memory_mb, disk_gb)
VALUES ('n_fsn1_02', 'worker', 'ams', 'http://10.0.0.12:9091', 'ready', 8, 32768, 640);
```

`Scheduler.Pick()` only considers rows with `role IN ('all','worker')` and a
reachable client, so a node is inert until this row exists and its runner answers.

### 6.3 Verify before trusting it

```bash
curl -H "Authorization: Bearer $RUNNER_SECRET" http://10.0.0.12:9091/v1/host
```

Expect a non-zero `swap_total_bytes`. Then check the runner's startup log for
`cgroup levers ON` — if it says `OFF`, the host cgroup mounts are missing and every
density lever is silently a no-op.

### 6.4 Networking invariant — never break this

> App containers must be reachable **only from the EDGE**, over the private network.

- `tunr-apps` keeps `icc=false` on every worker.
- The worker firewall permits app ports **only from EDGE mesh IPs**.
- App↔app traffic across nodes is **forbidden by default**.
- Nodes talk over a private network (WireGuard mesh or a provider private network) —
  never the public internet. Aim for ≥1 Gbit in the same DC: image and snapshot
  pulls run over it, and latency here becomes wake latency.

---

## 7. Adding a second EDGE

EDGE is stateless, so this is mostly DNS. Two things need care:

1. **Route cache per edge.** Each relay keeps its own local mirror
   (`TUNR_ROUTE_CACHE`) and stays live via Postgres `LISTEN/NOTIFY`. This is what
   lets an edge keep serving through a database outage.
2. **Distributed singleflight.** Two edges must not wake the same app
   simultaneously. Restore on the agent side is idempotent, but add a
   `pg_try_advisory_lock(app_id)` around the wake decision before running more than
   one edge in earnest.

---

## 8. Removing a node / maintenance

Checkpoint/restore (Faz 2) makes live migration nearly free, but even today:

1. Set the node `status = 'draining'` — `Scheduler.Pick` stops placing new work.
2. Let idle apps fall to STOPPED naturally, or stop them; they carry no state.
3. Apps whose `current_node_id` was this node are re-placed on first request.
4. Delete the row. `apps.current_node_id` is `ON DELETE SET NULL`, so apps are
   orphaned, **not deleted** — that is the pool model doing its job.

Drain cheapest-first: ARCHIVED → CHECKPOINTED → WARM → HOT.

---

## 9. Failure modes

| Failure | Impact | Recovery |
|---|---|---|
| **WORKER dies** | HOT/WARM apps on it drop (~5% of the population). Stopped apps unaffected | Scheduler re-places them; restored on first request |
| **BUILDER dies** | Deploys stop. **Serving is untouched** | Rebuild or fail over |
| **EDGE dies** | Its traffic shifts | DNS/LB, seconds |
| **Postgres dies** | Edges keep serving from the local route cache. New deploys and route changes stall. **Running apps are unaffected** | Failover; no relay restart needed — the pool reconnects itself |
| **Node unreachable** | Its samples drop out of the aggregate | Pressure aggregation takes the **max** across nodes, never the average, so one stalling node still trips the safety valve |

**Design rule:** *a control-plane failure must never stop the data plane.* Test it
by stopping Postgres and confirming existing apps still serve.

---

## 10. Before you scale: three things that bite

These are verified on real hardware, not theory. Each one silently costs capacity
or availability.

1. **zram needs a per-kernel module.** Ubuntu's `linux-image-virtual` omits it; it
   lives in `linux-modules-extra-$(uname -r)`, and there is no metapackage to track
   it. Every kernel upgrade silently removes swap. `tunr-zram-ensure.service`
   repairs this on boot — install it on every worker.
2. **The `icc=false` allow rule cannot be persisted.** It is keyed on the relay's
   Docker-assigned IP, which changes when the container is recreated. It must be
   *recomputed* at boot — that is `tunr-net-heal.service`. Without it a rebooted
   host comes up looking healthy while every app returns 503.
3. **Container start order is not guaranteed after a reboot.** Docker restart
   policies ignore `depends_on`, so the relay can start before Postgres. The pool is
   retained across a failed initial ping and reconnects itself; don't "fix" that by
   restoring a hard failure on startup.

---

## 11. Cost model

Rough European (Hetzner-class) monthly figures, for deciding whether a split is
worth it:

| Node | Spec | ~Monthly | Buys |
|---|---|---|---|
| WORKER | 8 vCPU / 32 GB / 640 GB | €40–70 | ~450 hot-equiv, ~9.000 hosted |
| BUILDER | 8 vCPU / 16 GB / 240 GB NVMe | €25–40 | 6–8 parallel builds |
| EDGE | 2 vCPU / 4 GB | €5–10 | ~10.000 req/s + HA |
| DATA | 4 vCPU / 16 GB / 1 TB | €30–50 | Postgres + object store |

Derived unit cost at maturity: **~€0.005–0.01 per hosted app per month.** That number
is what makes "personal tools free, team tools cheap" viable — and it only holds if
the density levers are actually on.

### Growth path

| Stage | Nodes | Hosted | Hot-equivalent |
|---|---|---|---|
| Today | 1 | ~1.000 | ~150 |
| + density levers (software only) | 1 | ~4.000 | ~400 |
| + Builder + Data | 3 | ~4.500 | ~450 |
| + 2 Workers | 5 | ~22.000 | ~1.300 |
| + 5 Workers, 2 Edges | 10 | ~50.000 | ~3.000 |

---

## 12. Checklist

Adding a node:

- [ ] Confirmed the trigger metric on a **7-day p95**, not a spike
- [ ] Verified the software levers are already exhausted (zram on, thresholds tuned)
- [ ] `server-setup.sh` (docker + gVisor + `tunr-apps` with `icc=false`)
- [ ] `host-density.sh` — **verify non-zero swap afterwards**
- [ ] `tunr-net-heal.sh --install` and `tunr-zram-ensure.sh --install`
- [ ] Runner up; startup log says `cgroup levers ON`
- [ ] `/v1/host` returns non-zero `swap_total_bytes`
- [ ] Private network reachable from the EDGE; app ports closed to everything else
- [ ] Row inserted into `nodes`
- [ ] A test app placed, woken, and served through the public URL
- [ ] `node_metrics` accumulating for the new `node_id`

Removing a node:

- [ ] `status = 'draining'`
- [ ] No apps report `current_node_id` for it
- [ ] Row deleted; confirmed apps were orphaned rather than deleted
