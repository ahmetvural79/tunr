# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Layout

This repo is a **single product split into two Go modules**:

- **`./` (module `github.com/ahmetvural79/tunr`)** — the **CLI** users install. Entry point `cmd/tunr/main.go` wires Cobra subcommands in `cmd/tunr/root.go`. All client logic lives in `internal/`.
- **`./relay/` (module `github.com/ahmetvural79/tunr/relay`)** — the **relay server** users connect to (`relay.tunr.sh`). Entry point `relay/cmd/server/main.go`. Lives in its own `go.mod` and must be built/tested separately.
- **`./sdk/`** — published wrappers around the CLI: `sdk/python` (`pip install tunr`, hatchling), `sdk/node` (`@tunr/cli`, tsc), plus a thin `sdk/go` and `sdk/js`. They shell out to the binary or hit the relay HTTP API; they do not import `internal/`.

Anything under `internal/` is CLI-only; anything under `relay/internal/` is relay-only. Don't cross-import — the modules have separate dependency trees (the relay pulls `pgx` and `golang-jwt`; the CLI does not).

## Build & Test Commands

The `Makefile` is the canonical entry point for the CLI module:

```bash
make build            # CGO_ENABLED=0 build of ./cmd/tunr → ./tunr
make build-dist       # release-flag build under dist/, verifies --version
make test             # go test -race -timeout 60s ./...
make lint             # golangci-lint v1.63.0 via `go run`
make vet              # go vet ./...
make security         # govulncheck ./...
make check            # vet + lint + test + security (CI parity)
make pre-push         # check + build — run before pushing
```

Single-test invocation: `go test -run TestName ./internal/proxy/...` (use `-race` to match CI).

The **relay module is not covered by the Makefile** — operate on it directly:

```bash
cd relay && go build ./cmd/server && go test ./...
cd relay && golangci-lint run --timeout=5m ./...
```

CI (`.github/workflows/ci.yml`) lints both modules, runs tests on `ubuntu/macos/windows`, and cross-compiles the CLI for linux/darwin/windows × amd64/arm64.

## Architecture (Big Picture)

```
Browser → relay.tunr.sh  ──[WebSocket control channel]──  tunr CLI  →  localhost:PORT
              │                                                │
        registry + auth                              LocalProxy + Inspector
              │                                                │
          Postgres                                         OS keychain
```

### CLI side (`internal/`)

- **`internal/tunnel/`** — owns tunnel lifecycle. `Manager.Start` (`tunnel.go`) creates a `Tunnel`, then `relay_client.go` opens a `gorilla/websocket` connection to the relay and exchanges framed messages (`MsgTypeHello/Welcome/Request/Response/Ping/Pong/WsOpen/WsFrame/TCPOpen/TCPData/...`). HTTP, WebSocket (HMR), TCP, UDP, and TLS protocols all multiplex over **one** WebSocket — the message-type discriminator routes them. `ws_bridge.go` bridges relay-side WS frames to/from a local `ws://` dev-server connection.
- **`internal/proxy/`** — the local middleware hub. `LocalProxy` (proxy.go) sits between the WS bridge and the user's dev server and runs a pre-built handler chain composed of: IP whitelist, bearer token (`auth_token_middleware.go`), basic auth (`auth_middleware.go`), header mutation, demo (`demo.go`, intercepts POST/PUT/DELETE), freeze cache (`freeze.go`, serves last-known-good 2xx on 5xx/crash, marks `X-Tunr-Freeze-Cache: 1`), HTML widget injection (`inject.go`, gunzip → splice before `</body>` → regzip), CORS, X-Forwarded-For, etc. **Edit middleware ordering carefully** — features interact (e.g. freeze must see real upstream responses; inject must run after decompression).
- **`internal/inspector/`** — ring-buffer of the last N requests (default 1000, configurable via `.tunr.json` `logRetention`), served on the local dashboard port (default `19842`).
- **`internal/webui/`** — embedded static dashboard (`tunr open`).
- **`internal/mcp/`** — Model Context Protocol server (`tunr mcp`) for Claude/Cursor/Windsurf integrations.
- **`internal/daemon/`** — `tunr start/stop/status` background mode (PID + socket-based control).
- **`internal/api/`** — local control API used by daemon + SDKs.
- **`internal/auth/`** — OS-keychain-backed token store; never write tokens to disk.
- **`internal/config/`** — `.tunr.json` parsing (schema in `.tunr.schema.json` at repo root).
- **`internal/billing/`** — Paddle integration (used by CLI to surface plan info).
- **`internal/term/`** — lipgloss terminal styling.

### Relay side (`relay/`)

- **`relay/cmd/server/main.go`** — registers HTTP routes: `/tunnel/connect` (CLI WS), `/tunnel/tcp` (browser WS for TCP tunnels), `/auth/magic` + `/auth/verify`, `/api/v1/*`, `/webhook/paddle`, and `/` → `proxy.ServeHTTP` (subdomain-based dispatch).
- **`relay/internal/relay/`** — `Registry` maps subdomains → CLI WebSocket sessions; `Handler` accepts CLI connections; `Proxy` routes inbound HTTP by subdomain; `proxy_ws.go` handles browser WS upgrades and bridges frames to the CLI; `tcp_handler.go` handles raw TCP tunnels; `rate_limiter.go` is in-memory per-IP; `balancer.go` carries multi-region routing metadata (regions `ams`, `sea`, `sin`).
- **`relay/internal/auth/`** — JWT issuance + magic-link tokens.
- **`relay/internal/db/`** — `pgx`-backed Postgres access. Schema in `relay/migrations/001_init.sql`. Relay runs in **in-memory mode** if `DATABASE_URL` is unset (tunnels work but nothing persists).
- **`relay/internal/runner/`** — the app-lifecycle driver behind `tunr-runner` (`relay/cmd/runner`). `driver.go` is the Docker/gVisor driver; `cgroup.go` and `buildslice.go` implement the density levers (see below).

### Cloud density levers (Yoğunluk planı Faz 0–1)

Apps live in a **3-state lifecycle**, driven by `relay/internal/relay/sweeper.go`:

```
HOT --45s idle--> WARM --20m idle--> STOPPED
 ▲                  │                   │
 └────── request ───┴───────────────────┘
```

- **WARM** is `memory.reclaim` **then** `docker pause` (`runner.DockerDriver.Sleep`). The pause alone leaves the full RSS resident; the reclaim pushes cold pages into zram. Measured in production: 48 MB → **~20 MB real cost, 55% saving**, wake p50 ~150 ms.

**Three lifecycle traps — all verified in production, all easy to re-introduce:**

1. **Reclaim BEFORE pause, never after.** Writing `memory.reclaim` to a *frozen* cgroup with a large resident set blocks — reclaim needs the cgroup's own tasks for writeback/unmapping, and the freezer has stopped them. Measured: >60 s on a frozen cgroup vs 68 ms running. Worse, `os.WriteFile` ignores context cancellation, so it outlives the handler deadline and wedges the caller's goroutine. (The plan's own pseudo-code has this bug.) `reclaimBounded` also caps the wait with a watchdog.
2. **Unpause BEFORE stop.** `docker stop` sends SIGTERM and a paused container cannot be signalled; under runsc this leaves a stale containerd task, and every later `docker start` fails with *"could not delete stale containerd task object"* — the app is wedged until `docker rm -f` + redeploy. This is why the history says *"scale-to-zero is pause-only for now"*.
3. **Every runner call needs a deadline.** `RunnerClient.http` has no timeout (Deploy streams for minutes), so lifecycle calls impose their own via `withTimeout`. Without it, one slow call permanently wedges the sweeper's single goroutine — scale-to-zero silently stops box-wide, and already-paused apps stay marked "awake" so requests to them hang.

Docker lifecycle operations must be **idempotent**: "already paused", "is not paused", "is not running" are all desired end states, not failures. Returning an error makes the sweeper retry forever *and* desyncs relay state from reality.
- **Memory QoS** is written directly to cgroup files after `docker run` (`Cgroups.ApplyQoS`), not via Docker flags — Docker has no stable mapping for `memory.high`/`memory.min`. Default tier: `memory.max=384M`, `memory.high=192M`.
- **`scripts/host-density.sh` is a hard prerequisite.** Without zram, `memory.reclaim` has nowhere to evict to *and* `memory.high` containment fails outright, escalating to system-wide OOM instead of throttling. Order is not negotiable: zram first, soft limits second.
- **The runner must see the host hierarchy.** It runs as a container; `docker-compose.runner.yml` bind-mounts `/sys/fs/cgroup` **rw** and `/proc` with `pid: host`. Without those every lever silently no-ops — the runner logs `cgroup levers OFF` at startup, and `update.sh` surfaces it.
- **Activity classification** (`relay/internal/relay/activity.go`) splits requests into Normal / Probe / Pin. Probes (health endpoints, uptime robots, crawlers) neither wake an app nor reset its idle clock, and a *sleeping* app's probe is answered at the edge with a synthetic 200 — without this, any monitored app never sleeps. Pins (WebSocket/SSE) forbid sleep while the connection is open; **the sweeper must never freeze a pinned app.**
- **Builds** run under `tunr-build.slice` (`cpu.idle=1`, low `io.weight`, 3 GB cap) with a 3-slot semaphore, so a deploy can't spike wake latency.
- **Telemetry**: runner exposes `/v1/host` (cheap, polled by the sweeper's PSI safety valve) and `/v1/stats` (full per-app cgroup picture). Relay exposes `/api/v1/density`, gated behind `TUNR_METRICS_TOKEN` (unset → route not mounted at all).

Env knobs: `TUNR_IDLE_SLEEP`, `TUNR_IDLE_STOP`, `TUNR_METRICS_TOKEN` (relay); `TUNR_BUILD_SLOTS`, `TUNR_CGROUP_ROOT`, `TUNR_PROC_ROOT` (runner).

### Multi-node readiness (Çok-node planı Faz A)

> Operational runbook for adding/removing capacity: **`docs/SCALING.md`** — trigger
> metrics, split order (Builder → Data → Worker → Edge), sizing, and the checklist.
> (`docs/` is gitignored, so that file lives locally and is rsync'd by `update.sh`.)

Still one box — these exist so growing the cluster is a config change rather than a refactor.

- **`relay/internal/relay/scheduler.go`** — `RUNNER_URL` is no longer reachable as a bare string. Every wake/sleep/stop/status call goes through `NodeClient` + `Scheduler.Pick(app)`, which today always returns the same node. **Add new lifecycle calls through the scheduler, never against `RunnerClient` directly.** Compile-time assertions at the bottom of the file enforce the boundary.
- **Pool, not shard.** `apps.current_node_id` is where an app runs *right now*, not where it belongs (`ON DELETE SET NULL` — a dead node orphans apps rather than destroying them). `Pick` is called on every wake.
- **`relay/internal/relay/route_cache.go`** — the relay mirrors the routes table to disk (`TUNR_ROUTE_CACHE`, default `/var/lib/tunr/routes.json`, mode 0600 because it holds edge secrets). If Postgres is down at boot it serves from the mirror: existing apps work, new deploys correctly stall. **Design rule: a control-plane failure must not stop the data plane.**
- **`scheduler.ReconcileStates`** runs at startup. Upstreams default to "awake", but containers may be paused from before the restart — and a paused container still completes a TCP handshake, so probing cannot detect it. Without reconciliation the first request hangs on a frozen process.
- **`relay/internal/runner/cpubaseline.go`** — plumbing to pin `dev.gvisor.internal.cpufeatures`, **shipped disabled**. Verified on runsc `release-20260721.0`: passing `x86-64-v2` makes sandbox creation fail outright (`cannot read client sync file`), breaking every deploy. It buys nothing before Faz 2, so confirm the accepted syntax against the deployed runsc build before enabling — then treat the value as frozen, because changing it after snapshots exist invalidates all of them.
- **Runner `--role=all|agent|builder`** — one binary, one process per role. An agent-only runner doesn't mount `/v1/deploy`. Splitting builds onto their own machine is a compose change.
- **`node_metrics`** is written every minute by `node_reporter.go` and nothing reads it yet. That's deliberate: the "when do we add a node?" thresholds are 7-day p95s, so collection has to predate the decision.

Relay required env: `TUNR_DOMAIN`, `TUNR_JWT_SECRET` (32+ chars), `DATABASE_URL`, `PORT` (default 8080). Optional: `TUNR_LOG_LEVEL`, `PADDLE_WEBHOOK_SECRET` (+ price/product IDs).

### Protocol invariant

The wire protocol between CLI and relay is the typed message stream in `internal/tunnel/relay_client.go` (`MsgType*` constants) and its mirror in `relay/internal/relay/`. **Any new message type must be added to both modules** and both must tolerate unknown types from older peers.

## Conventions & Gotchas

- **Go 1.22+**, `CGO_ENABLED=0` everywhere — the CLI must stay a single static binary.
- **Linter config** (`.golangci.yml`) disables `revive`'s `exported` rule, silences `gosec` G107/G110/G306/G404, and excludes `_test.go` from `errcheck`/`gosec`/`noctx`/`bodyclose`. Don't churn code to satisfy rules that are deliberately off.
- **Auth tokens** must go through `internal/auth` (OS keychain). Never log `Manager.authToken` or write tokens to files — the codebase has `SECURITY:` comments marking these spots.
- **Versioning** — `cmd/tunr/main.go` injects `Version` into `internal/tunnel` via its `init()`. The `-ldflags "-X main.Version=..."` in the Makefile is how releases get their version string; both CLI and SDK package versions (`sdk/python/pyproject.toml`, `sdk/node/package.json`) currently track `v0.4.0`.
- **Some doc comments are in Turkish** (notably `relay/cmd/server/main.go` and the JSON schema). This is intentional — preserve the language when editing those files unless the user asks for a translation.
- **Tests near the CLI exercise CLI flag parsing**; tests under `internal/proxy/` use a real loopback HTTP server. There's a mock relay at `scripts/e2e/mock_relay.go` for end-to-end shell scripts (`scripts/full_*.sh`).
- **License is PolyForm Shield 1.0.0** — contributions are accepted but the license forbids building a competing tunnel service. Keep this in mind when proposing scope changes.
