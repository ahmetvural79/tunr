# Changelog

All notable changes to tunr are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

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
