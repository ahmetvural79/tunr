# Updating the app and landing via SSH

How to deploy code and env changes to the server over SSH.

---

## Prerequisites

- Server reachable at `<SERVER_IP>` (e.g. Hetzner).
- App source at `/opt/tunr/src` (or your path).
- Dashboard systemd service: `tunr-dashboard`.
- Static landing at `/var/www/tunr/`.

---

## 1. SSH into the server

```bash
ssh root@<SERVER_IP>
# or
ssh tunr@<SERVER_IP>
```

---

## 2. Update the Next.js app (Dashboard)

### Option A: rsync from your machine

From your **local** project root (e.g. `~/Desktop/vibetunnel/tunr`):

```bash
rsync -avz --exclude='.git' --exclude='node_modules' --exclude='.next' \
  ./landing/ root@<SERVER_IP>:/opt/tunr/src/landing/
```

Then on the **server**:

```bash
cd /opt/tunr/src/landing/app
npm ci
npm run build
systemctl restart tunr-dashboard
```

### Option B: git pull on the server

On the **server**:

```bash
cd /opt/tunr/src
git pull origin main

cd landing/app
npm ci
npm run build
systemctl restart tunr-dashboard
```

### Verify

```bash
systemctl status tunr-dashboard
journalctl -u tunr-dashboard -f
```

---

## 3. Update static landing (tunr.sh)

Static files are served by Caddy from `/var/www/tunr/`. No restart needed after file changes.

From your **local** project:

```bash
rsync -avz --exclude='.git' --exclude='node_modules' --exclude='app' \
  ./landing/ root@<SERVER_IP>:/var/www/tunr/
```

Or on the **server** after a git pull:

```bash
cp -r /opt/tunr/src/landing/* /var/www/tunr/
# Do not overwrite app/ if it’s only Next.js; copy only static files (e.g. *.html, install.sh, style.css)
```

If you use **Cloudflare**, purge cache for changed URLs (e.g. `https://tunr.sh/use-scenarios.html`).

---

## 4. Environment variables

- `.env` for the dashboard is usually at `/opt/tunr/.env` or `/opt/tunr/src/landing/app/.env`.
- After changing **any** env (e.g. `ADMIN_EMAILS`, `NEXT_PUBLIC_*`, `FIREBASE_*`), **rebuild and restart**:

```bash
cd /opt/tunr/src/landing/app
npm run build
systemctl restart tunr-dashboard
```

`NEXT_PUBLIC_*` values are baked in at **build** time, so they only change after a new build.

---

## 5. One-liner (local → server, then build)

From your **local** machine (replace `<SERVER_IP>`):

```bash
rsync -avz --exclude='.git' --exclude='node_modules' --exclude='.next' \
  ./landing/ root@<SERVER_IP>:/opt/tunr/src/landing/ && \
ssh root@<SERVER_IP> 'cd /opt/tunr/src/landing/app && npm ci && npm run build && systemctl restart tunr-dashboard'
```

---

## 6. Rollback

If a deploy breaks the app:

```bash
cd /opt/tunr/src
git log -1
git checkout <previous-commit>
cd landing/app && npm ci && npm run build && systemctl restart tunr-dashboard
```

Then fix forward or revert again as needed.
