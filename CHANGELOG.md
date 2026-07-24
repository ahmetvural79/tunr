# Changelog

All notable changes to tunr are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

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
