# tunr relay — Docker deployment (live setup)

This documents the **actual running production deployment** on the relay server, brought up with Docker Compose. It complements `DEPLOYMENT.md` (the native systemd/Go model). The live box runs the Docker model described here.

## Host layout

```
/opt/tunr/
├── .env                       # secrets (chmod 600, git-ignored) — see "Environment" below
├── docker-compose.yml         # postgres + relay
├── docker-compose.caddy.yml   # caddy (TLS front) — opt-in overlay
├── caddy/
│   ├── Dockerfile             # caddy:2 + caddy-dns/cloudflare plugin
│   └── Caddyfile              # apex landing + relay proxy + *.tunr.sh wildcard
└── src/                       # rsynced repo working tree (relay built from src/relay)
/var/www/tunr/landing/         # static landing served by caddy at tunr.sh
```

## Services

- **postgres** (`postgres:16-alpine`) — schema from `src/relay/migrations/*.sql`, applied via `psql`.
- **relay** (built from `src/relay/Dockerfile`, scratch image) — bound to `127.0.0.1:8080`; only Caddy reaches it.
- **dashboard** (built from `src/landing/app/Dockerfile`, Next.js 16) — serves `app.tunr.sh`; reached by Caddy on the compose network (`dashboard:3000`). Env in `src/landing/app/.env.local`.
- **caddy** (custom image, Cloudflare DNS plugin) — terminates TLS on 80/443, obtains a real Let's Encrypt **wildcard cert** for `*.tunr.sh` + `tunr.sh` via **DNS-01** challenge. Routes `tunr.sh`→landing+relay, `app.tunr.sh`→dashboard, `*.tunr.sh`→relay (tunnels).

Compose spans three files: `docker-compose.yml` (postgres+relay), `docker-compose.dashboard.yml` (dashboard), `docker-compose.caddy.yml` (caddy). Bring everything up with all three `-f` flags.

### Dashboard env still TODO (login/checkout)
`src/landing/app/.env.local` reuses the relay's `JWT_SECRET` (so CLI tokens validate) and shares Postgres. These are **build-time** (`NEXT_PUBLIC_*`) — after filling them, rebuild: `docker compose -f docker-compose.yml -f docker-compose.dashboard.yml up -d --build dashboard`:
- `NEXT_PUBLIC_FIREBASE_API_KEY`, `NEXT_PUBLIC_FIREBASE_APP_ID`, `NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID` — Firebase Console → project `tunr-app-627e3` → Project settings → Web app SDK config. (Also add `app.tunr.sh` to Firebase Auth → Authorized domains.)
- `NEXT_PUBLIC_PADDLE_PRICE_ID_MONTHLY` / `_YEARLY` — Paddle **price** IDs (`pri_…`) for checkout.
- `RESEND_API_KEY` — if magic-link email login is used.
- Confirm `NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET` suffix (`.appspot.com` vs `.firebasestorage.app`).

## Environment (`/opt/tunr/.env`)

Generated secrets (JWT, Postgres password) + the operator-supplied values:

```
TUNR_DOMAIN=tunr.sh
TUNR_JWT_SECRET=<generated 64-hex>
POSTGRES_PASSWORD=<generated>
DATABASE_URL=postgres://tunr:<pw>@postgres:5432/tunr?sslmode=disable
PORT=8080
TUNR_LOG_LEVEL=info
# TUNR_DEV_MODE — MUST stay unset in prod (leaks magic-link token / auth bypass)

PADDLE_WEBHOOK_SECRET=<pdl_ntfset_...>
PADDLE_PRO_PRODUCT_ID=<pro_...>          # Paddle product IDs (pro_), not price IDs
PADDLE_DEFAULT_PAID_PLAN=pro
CLOUDFLARE_API_TOKEN=<cf token for DNS-01>   # NOT an origin certificate
```

> **Paddle note:** the operator provided **product** IDs (`pro_…`). The relay grants `pro` on any active subscription via product-ID match + `PADDLE_DEFAULT_PAID_PLAN=pro`. The **dashboard checkout** (Next.js app) needs **price** IDs (`pri_…`) + the client token — separate from the relay.

## Bring-up / operations

```bash
cd /opt/tunr

# Start/refresh relay + postgres
docker compose up -d --build

# Apply DB migrations (idempotent)
for f in src/relay/migrations/*.sql; do
  docker compose exec -T postgres psql -U tunr -d tunr -v ON_ERROR_STOP=1 < "$f"
done

# Start Caddy (TLS) — needs CLOUDFLARE_API_TOKEN + DNS pointing here
docker compose -f docker-compose.yml -f docker-compose.caddy.yml up -d caddy

# Update relay after a code change (rsync src/ from dev first)
docker compose up -d --build relay

# Logs
docker logs -f tunr-relay-1
docker logs -f tunr-caddy-1
```

## DNS (Cloudflare → tunr.sh)

| Type | Name | Content | Proxy |
|------|------|---------|-------|
| A | `@` | 167.233.102.96 | 🟠 Proxied |
| A | `www` | 167.233.102.96 | 🟠 Proxied |
| A | `*` | 167.233.102.96 | 🟠 Proxied (or grey) |
| A | `relay` | 167.233.102.96 | ⚪ DNS only |

`relay` is grey so the CLI connects directly (needs the public LE cert, which DNS-01 provides). SSL/TLS mode: **Full (Strict)**.

## ⚠️ Gotchas learned during this deploy

1. **WebSocket through Caddy:** do **not** add `header_up Upgrade {>Upgrade}` / `header_up Connection {>Connection}` to the reverse_proxy — Caddy v2 handles WS natively and those lines break the handshake (`'upgrade' token not found in 'Connection' header`). Use a bare `reverse_proxy`. (Fixed in `relay/caddy/Caddyfile`.)
2. **Docker single-file bind-mount + `sed -i`:** editing a bind-mounted file with `sed -i`/`vim` changes its inode, so the container keeps serving the *old* file. After editing `caddy/Caddyfile`, **recreate** the container (`docker compose ... up -d --force-recreate caddy`) — a `caddy reload` alone reads the stale inode.
3. **Wildcard TLS = Cloudflare API token, not an Origin Certificate.** `*.tunr.sh` needs DNS-01, which needs a `Zone:DNS:Edit` API token. An Origin Certificate is only trusted by Cloudflare's edge and fails for the grey-cloud `relay.tunr.sh` the CLI connects to directly.

## Verified live (2026-06-11)

- `https://relay.tunr.sh/api/v1/health` → 200
- TLS: `CN=*.tunr.sh`, Let's Encrypt
- WS `/tunnel/connect` → 101 Switching Protocols through Caddy
- End-to-end: `tunr share` → public `*.tunr.sh` URL served local content
- `/webhook/paddle` → 401 on unsigned (HMAC verification active)
- `TUNR_DEV_MODE` unset in relay container
