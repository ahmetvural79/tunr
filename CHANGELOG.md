# Changelog

All notable changes to tunr are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## v0.6.0 — 2026-08-12

**Cloud deploy leaves private preview.** `tunr deploy` builds a directory with
Nixpacks and runs it in a gVisor sandbox on tunr's infrastructure — it sleeps
when idle, wakes on request, and keeps serving after you close your laptop. The
same pipeline is now reachable from an agent over MCP.

### Added
- **cli:** `tunr deploy [dir]` — pack, build (Nixpacks, no Dockerfile needed)
  and host a project; `--name`, `--port`, `--env KEY=VALUE`. `.env` files are
  never uploaded.
- **cli:** `tunr apps`, `tunr apps delete <name>` and **`tunr apps logs <name>`**
  (`--follow`, `--tail N`) — the last of which the README had been documenting
  without it existing.
- **cli:** global **`--relay <url>`** flag. The self-hosting docs have taught
  this command since v0.4.0, but the only real knob was `TUNR_RELAY_URL`, so
  the documented command failed with `unknown flag`. The env var still works.
- **mcp:** **`tunr_deploy`**, **`tunr_app_logs`** and **`tunr_delete_app`**.
  Previously the MCP surface was tunnel-only, so an agent asked to "deploy this"
  had no tool to reach for and fell back to opening a temporary tunnel — the
  right-looking answer to the wrong question. The `tunr_deploy` and `tunr_share`
  descriptions now state the distinction explicitly.
- **relay:** `GET /v1/apps/logs` (owner-scoped) and a matching runner endpoint,
  routed through the scheduler rather than the runner client.
- **cloud:** density levers — `HOT → WARM → STOPPED` lifecycle, cgroup memory
  QoS, build slice isolation, PSI safety valve, activity classification
  (probe/pin) so a monitored app can still fall asleep.
- **cloud:** multi-node readiness — `Scheduler`/`NodeClient` indirection, disk
  route-cache mirror (the data plane survives a Postgres outage), startup state
  reconciliation, `--role=all|agent|builder`.

### Fixed
- **mcp:** the server no longer writes log lines to **stdout**, which is the
  JSON-RPC transport. `INFO`/`WARN` now go to stderr in `tunr mcp`, so strict
  MCP clients stop seeing a parse error on connect.
- **runner:** `docker logs` processes are killed and reaped on close instead of
  leaking a zombie per request.
- **relay:** survive a host reboot — DB startup race, route cache, ephemeral
  firewall rules.
- **build:** `ref/` (pivot planning material) was committed by mistake in
  `4a57bb9`; it contains stray `.go` files whose package names collide, which
  broke `go build ./...` and `go test ./...` — the release gate. Untracked and
  ignored.

### Changed
- **licence:** the repository is now **dual-licensed by directory**. The CLI and
  SDKs (`cmd/`, `internal/`, `sdk/`) are **Apache-2.0** — OSI-approved open
  source. The relay, control plane and runner (`relay/`) stay **PolyForm Shield
  1.0.0**, source-available. Previously the README claimed "open source" in
  three places while the whole repo was PolyForm, and the published SDK packages
  declared Apache-2.0 — the split makes all of those statements true. See
  [NOTICE](NOTICE).
- **docs:** README restructured around the cloud; every tunnel feature is kept,
  in collapsible sections. The four tunnel-competitor tables moved to
  [docs/compare-tunnels.md](docs/compare-tunnels.md), with the incorrect
  "Open Source ✅" rows corrected to name the actual licence.
- **docs:** `SELF_HOSTING.md` now covers the cloud runner, not just tunnels.
- **sdk:** Python and Node packages bumped to 0.6.0 and published from CI on tag.

## v0.5.0 — 2026-07-24

This release marks the start of tunr's shift from a pure tunneling CLI toward a
place to **deploy, host and share the apps your agent builds**. The tunnel
(`tunr share`) stays free and first-class — it's now the on-ramp.

> **Cloud deploy (`tunr deploy`) is in private preview** and not part of this
> open-source build yet. This release ships the brand refresh plus build and
> robustness hardening; the tunnel/CLI feature set is unchanged and stable.

### Changed
- **brand:** new "t+c" monogram logo (renders navy in light UI, knocks out to
  white in dark), matching wordmark, and reworked positioning across the README,
  landing and dashboard.

### Fixed
- **cli:** `tcp` — check the forwarded `conn.Write`, close the WebSocket
  handshake response body, and drop an unused struct field.
- **cli:** `service uninstall` — the best-effort `systemctl`/`launchctl` calls no
  longer trip `errcheck`.
- **cli:** `up` — dropped a redundant `fmt.Sprintf("%s", …)`.
- **relay:** guard the `SetWriteDeadline` return in the TCP write path.

### CI / Build
- Pin the CI Go toolchain to 1.23: newer macOS runners' dyld rejected the
  race-instrumented test binaries older Go produced (`missing LC_UUID load
  command / abort trap`). Green across ubuntu/macos/windows again.
- `gofmt` + `golangci-lint` clean on both modules.

## v0.4.1 — 2026-06-11

### Security
- **relay:** `/auth/magic` no longer returns the login token in its HTTP response
  (was an account-takeover vector); the dev link is gated behind `TUNR_DEV_MODE`.
- **relay:** `/auth/verify` no longer mints a JWT for arbitrary tokens when no
  database is configured unless `TUNR_DEV_MODE=1`.
- **relay:** per-IP rate limiting added to `/auth/magic` (token spam / user
  enumeration / email flood).
- **relay:** fixed WebSocket origin bypass on `/tunnel/connect` & `/tunnel/tcp`
  (`strings.HasSuffix(origin, "tunr.sh")` accepted `eviltunr.sh`); now host-parsed.
- **relay:** bounded the in-memory rate-limiter bucket map with opportunistic
  eviction; `X-RateLimit-Limit` now reports the real per-plan value.

### Fixed
- **cli:** implemented the daemon `runCommand` helper (was a stub) so Windows
  process detection / `tunr stop` works.
- **cli:** fixed the invalid `http://localhost:*` CORS value in the internal API;
  loopback origins are now reflected correctly.

### Added
- **relay:** real per-user request/bandwidth usage metering, surfaced via
  `/api/user/profile` and `/api/user/usage` (previously hardcoded to `0`).

### Ops
- **caddy:** removed manual `header_up Upgrade/Connection` from the reverse proxy —
  Caddy v2 proxies WebSocket upgrades natively and those lines broke the handshake.

## v0.4.0 — 2026-04-16
- UDP & TLS tunnels, Docker image, multi-tunnel config, Prometheus metrics,
  Python & Node.js SDKs.
