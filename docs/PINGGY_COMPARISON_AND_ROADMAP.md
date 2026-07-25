# tunr vs Pinggy — Feature Parity & Roadmap

Last reviewed: 2026-06-11. Pinggy feature set per pinggy.io as of this date; tunr features verified against the shipped build (`internal/`, `relay/internal/`, `cmd/tunr/`).

The goal of this doc: (1) an honest parity matrix against Pinggy, (2) the gaps that actually matter, (3) a prioritized roadmap, and (4) the differentiators tunr should lean into.

---

## 1. Parity matrix

| Capability | Pinggy | tunr | Notes |
|---|:---:|:---:|---|
| HTTP/HTTPS tunnel | ✅ | ✅ | tunr: auto-HTTPS at the relay |
| TCP tunnel | ✅ | ✅ | |
| UDP tunnel | ✅ | ✅ | |
| TLS / SNI pass-through (end-to-end) | ✅ | ✅ | relay can't read payload |
| **SSH-based tunneling (no client download)** | ✅ | ❌ | **Pinggy's signature feature — biggest gap** |
| Random subdomain (free) | ✅ | ✅ | |
| Custom persistent subdomain (Pro) | ✅ | ✅ | |
| Custom domain + auto-HTTPS | ✅ | ✅ | |
| **Wildcard domain routing** | ✅ | ❌ | tunr has path routing, not wildcard subdomain→port |
| Live web debugger / request inspector | ✅ | ✅ | tunr inspector is local (localhost:19842) |
| Request replay | ✅ | ✅ | `tunr replay`, also curl export |
| Password protection (Basic auth) | ✅ | ✅ | |
| Bearer / key auth | ✅ | ✅ | |
| IP allowlist (CIDR) | ✅ | ✅ | |
| Header manipulation | ✅ | ✅ | add/replace/remove |
| QR code | ✅ | ✅ | |
| Multi-region | ✅ (5 regions) | ✅ (3: ams/sea/sin) | tunr has fewer PoPs |
| Auto-reconnect / keep-alive | ✅ | ⚠️ | CLI reconnect exists; verify robustness under flaps |
| No-signup / instant use | ✅ | ✅ | anonymous tunnels supported |
| Free-tier tunnel timeout | 60 min | none (rate-limited) | **tunr advantage** |
| **Open source** | ❌ | ✅ | **tunr advantage** |
| **Self-hostable relay** | ❌ | ✅ | **tunr advantage** |
| **Freeze cache (crash-proof demos)** | ❌ | ✅ | **unique to tunr** |
| **Demo / read-only mode** | ❌ | ✅ | **unique to tunr** |
| **Feedback-widget injection** | ❌ | ✅ | **unique to tunr** |
| **MCP server (AI opens tunnels)** | ❌ | ✅ | **unique to tunr** |
| SDKs (Python/Node/Go) | ❌ | ✅ | **tunr advantage** |
| Team / collaboration | ✅ (Pro/Ent) | ⚠️ | plan exists, no team tunnel ownership UI |
| Cloud dashboard tunnel management | partial | ⚠️ | history + usage; no create/edit/kill from UI |
| API access (programmatic) | ✅ (Ent) | ⚠️ | relay `/api/user/*` exists; not documented as public API |

Legend: ✅ shipped · ⚠️ partial/needs work · ❌ missing

---

## 2. Gaps that matter (ranked)

### P0 — strategic
1. **SSH-based tunneling.** Pinggy's headline is "no download — just `ssh -R 80:localhost:3000 a.pinggy.io`". tunr requires its binary. Supporting `ssh -R` against the relay would remove tunr's single biggest adoption barrier (works on any box, including locked-down servers where you can't install software). Largest effort, largest payoff.
2. **Cloud dashboard tunnel management.** Today the dashboard shows history + (now real) usage but you can't create, inspect live, kill, or reconfigure a tunnel from it. This is the "dashboard yönetimi" gap. Pinggy users expect to manage tunnels from the web.

### P1 — competitive
3. **Wildcard domain routing** (`*.myapp.com` → different local ports by subdomain).
4. **Robust auto-reconnect** with exponential backoff + state resume, surfaced in the CLI.
5. **Public, documented HTTP API** (the relay already has `/api/user/*` — document it, add API tokens, version it).
6. **More relay regions** (Pinggy has 5; tunr has 3).

### P2 — polish / retention
7. **Inspector search & filter** (by method/path/status), **HAR export**.
8. **Outgoing webhooks** on tunnel lifecycle events (created/connected/disconnected) and on matched requests.
9. **Team tunnel ownership** (shared subdomains, seats) to make the Team plan real.
10. **Request/bandwidth metering persisted** (currently in-memory per relay instance — see §4; persist to Postgres for multi-instance + historical charts).

---

## 3. Roadmap (suggested sequencing)

**Milestone A — "manage from the web" (P0 #2, P2 #10 persistence)**
- Persist per-tunnel request/bandwidth counters to Postgres (the in-memory counters added this session are the data source; flush periodically).
- Dashboard: live tunnel list with kill button (relay already exposes per-user tunnels via `/api/user/tunnels`; add a `DELETE`/disconnect endpoint that closes the WS entry).
- Dashboard: usage charts from persisted metering.

**Milestone B — "no install" (P0 #1 SSH)**
- Add an SSH server frontend on the relay that maps `ssh -R` sessions onto the existing tunnel registry. Reuse the subdomain assignment + auth (SSH key ↔ account).
- This is the flagship parity feature; scope it as its own project.

**Milestone C — "power routing" (P1 #3, #5, #6)**
- Wildcard domain → port mapping.
- Document + version the public API; issue API tokens (the `/api/user/token` endpoint is a stub today — make it real).
- Add relay PoPs to match Pinggy's region coverage.

**Milestone D — "retention" (P2 #7, #8, #9)**
- Inspector search/filter + HAR export.
- Outgoing webhooks.
- Team seats + shared tunnel ownership.

---

## 4. Done this session

**Security / correctness fixes (shipped in this branch):**
- **Magic-link token leak** — `/auth/magic` no longer returns the login token in its HTTP response in production (was full account-takeover for any email). Gated behind `TUNR_DEV_MODE=1`. (`relay/cmd/server/main.go`)
- **In-memory auth bypass** — `/auth/verify` no longer mints a JWT for arbitrary tokens when no DB is configured unless `TUNR_DEV_MODE=1`. (`relay/cmd/server/main.go`)
- **Auth endpoint rate limiting** — `/auth/magic` now rate-limited per client IP (token spam / user enumeration / email flood). (`relay/cmd/server/main.go`, `rate_limiter.go`)
- **WebSocket origin bypass** — `/tunnel/connect` & `/tunnel/tcp` used `strings.HasSuffix(origin, "tunr.sh")`, which accepts `eviltunr.sh`. Now uses host-parsing `browserTunnelCheckOrigin` (exact `tunr.sh` or `*.tunr.sh`). (`relay/internal/relay/handler.go`)
- **Rate-limiter memory cap** — bucket map is bounded (`maxBuckets`) with opportunistic eviction so a flood of unique/spoofed keys can't exhaust memory between cleanup ticks; rate-limit headers now report the real per-plan limit. (`relay/internal/relay/rate_limiter.go`)
- **Daemon Windows process detection** — `runCommand` was a stub that always returned empty (broke `tunr stop`/stale-PID cleanup on Windows). Now actually runs the command. (`internal/daemon/daemon.go`)
- **Internal-API CORS** — replaced the invalid `Access-Control-Allow-Origin: http://localhost:*` (ignored by browsers) with proper loopback-origin reflection. (`internal/api/server.go`)

**Feature shipped this session:**
- **Real dashboard usage metering** — per-user request count and bandwidth are now measured server-side (`Registry.RecordRequest`/`UserUsage`) and surfaced through `/api/user/profile` and `/api/user/usage`, which previously always returned `0`. Daily reset; unit-tested. (`relay/internal/relay/{registry,proxy,user_api}.go`, `metrics_test.go`)

**Verified NOT vulnerable (audit false positives worth recording):**
- Paddle webhook **does** verify HMAC-SHA256 with a ±5-min timestamp window before processing → forged/replayed downgrade events are rejected. (`paddle_webhook.go`)
- Magic-token consumption **does** use `SELECT … FOR UPDATE` → no double-use race. (`relay/internal/db/db.go`)
- JWT verification enforces `alg=HS256` only and requires expiry → no `alg:none` / missing-exp bypass. (`relay/internal/auth/jwt.go`)

---

## 5. Positioning takeaway

tunr should **not** try to out-feature Pinggy on raw tunneling — it's already at parity except SSH-mode and wildcard routing. The wedge is **"the tunnel for people who build with AI and demo to clients"**: freeze cache, demo mode, feedback widget, MCP server, open-source/self-hostable. Lead every comparison with those; treat SSH-mode and dashboard management as the parity work that removes reasons *not* to switch.
