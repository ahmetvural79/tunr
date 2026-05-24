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
