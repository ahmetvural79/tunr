# Release → Deploy → Test → Paddle Sandbox Walkthrough

> Step-by-step: create a new GitHub release, connect to server, update landing + relay, test the app, and verify login/Pro using Paddle sandbox.

This guide covers one release cycle end-to-end. For related details, see [PRODUCTION_SETUP.md](./PRODUCTION_SETUP.md), [RELAY_SERVER.md](./RELAY_SERVER.md), [DEPLOYMENT.md](./DEPLOYMENT.md), and [DEPLOY_AND_TEST_WALKTHROUGH.md](./DEPLOY_AND_TEST_WALKTHROUGH.md).

---

## Overall Flow

```
1. GitHub Release (tag → workflow → publish)
2. SSH to server -> update landing -> relay build + restart
3. Smoke test (landing, relay, install script, tunnel)
4. Login (magic link) + Paddle sandbox ile Pro test
```

---

## Section 1: Creating a New GitHub Release

### 1.1 Pre-checks (local)

```bash
cd /path/to/tunr

# Tests and lint
go test ./...
make lint   # or: golangci-lint run ./...
# if relay exists:
cd relay && go test ./... && cd ..

# Choose a version number (e.g. v0.1.1)
VERSION="v0.1.1"
```

### 1.2 Create and push tag

```bash
git tag -a "$VERSION" -m "tunr $VERSION"
git push origin "$VERSION"
```

- Pushed `v*` tags trigger `.github/workflows/release.yml`.
- Workflow runs tests + govulncheck (non-blocking), then builds binaries with **GoReleaser** and uploads to GitHub Release.
- The first run creates a **Draft Release**; publishing is still manual.

### 1.3 Publish the release

1. **GitHub** → **ahmetvural79/tunr** → **Releases**
2. Open the latest **draft** release
3. Edit release notes if needed (you can use the templates below)
4. Click **"Publish release"**

After publishing:

- `https://github.com/ahmetvural79/tunr/releases/latest` is updated
- `curl -sL https://tunr.sh/install.sh | sh` (or `https://tunr.sh/install.sh`) downloads from that release

### 1.4 Release notes (English)

In the release page's "Describe this release" field, use English notes like below. A short intro, change list, and install link are enough.

**First release example (v0.1.0):**

```markdown
## tunr v0.1.0 — First public release

tunr is a zero-config local-to-public tunnel tool. Share your local server with a public URL in under 3 seconds.

### Highlights

- **Zero config** — `tunr share --port 3000` and you're live
- **Auto-HTTPS** — Valid TLS for every tunnel
- **WebSocket & HMR** — Full support for dev servers and hot reload
- **Local inspector** — Inspect requests in real time (`tunr open`)
- **MCP server** — Use tunnels from Claude Desktop and Cursor (`tunr mcp`)

### Install

- `curl -sL https://tunr.sh/install.sh | sh`
- Homebrew (macOS): `brew install ahmetvural79/tap/tunr`

### Links

- [Documentation](https://github.com/ahmetvural79/tunr/tree/main/docs)
- [Security policy](https://github.com/ahmetvural79/tunr/blob/main/docs/SECURITY.md)
```

**Patch release example (v0.1.1):**

```markdown
## tunr v0.1.1

Bug fixes and small improvements.

### What's changed

- **Fixed** — Relay connection timeout on slow networks
- **Fixed** — Gofmt compliance in relay user API
- **Improved** — Install script retry logic and error messages
- **Docs** — Release and deploy walkthrough added

### Install or upgrade

- `curl -sL https://tunr.sh/install.sh | sh`
- Existing users: run `tunr update` to get this version.
```

**Minor release example (v0.2.0):**

```markdown
## tunr v0.2.0

New features and relay updates.

### New

- **Custom subdomains** — Reserve a stable URL with Pro (`tunr share -s myapp`)
- **Demo mode** — `--demo` flag blocks mutating requests for safe client demos
- **Relay** — Improved rate limiting and audit logging

### Changed

- **Relay API** — Health endpoint now includes version; see [RELAY_SERVER.md](docs/RELAY_SERVER.md)
- **CLI** — `tunr doctor` checks relay reachability

### Fixed

- WebSocket reconnection when relay restarts
- Install script on Windows (Git Bash path)

### Install

- `curl -sL https://tunr.sh/install.sh | sh`
```

**Rules:**

- Keep the title clear: version + short summary (e.g. "First public release", "Bug fixes", "New features").
- Group changes under **New**, **Changed**, **Fixed**, etc.
- Always include the install command.
- If there is a breaking change, document it clearly in a **Breaking** section.

---

## Section 2: Connect to Server, Update Landing, Restart Relay

### 2.1 Connect to server

```bash
ssh root@<SERVER_IP>
# or: ssh tunr@<SERVER_IP> (user with sudo privileges)
```

### 2.2 Update source code (if using server-side repo)

```bash
cd /opt/tunr/src
git fetch origin
git checkout main   # or target branch
git pull origin main
```

**Alternative (rsync from local machine):**

```bash
# On local machine (macOS example):
rsync -avz --exclude='.git' --exclude='node_modules' --exclude='.next' \
  /Users/ahmetvural/Desktop/vibetunnel/tunr/ root@<SERVER_IP>:/opt/tunr/src/
```

### 2.3 Update landing page files

```bash
# On server
sudo cp -r /opt/tunr/src/landing/* /var/www/tunr/
sudo cp /opt/tunr/src/install.sh /var/www/tunr/ 2>/dev/null || true

sudo chown -R caddy:caddy /var/www/tunr
sudo chmod -R 755 /var/www/tunr
```

### 2.4 Rebuild relay binary and restart service

```bash
cd /opt/tunr/src/relay

# Build (relay module path)
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags="-w -s -X main.Version=$(git describe --tags --always 2>/dev/null || echo 'dev')" \
  -o /tmp/tunr-relay-new \
  ./cmd/server

# Replace previous binary
sudo mv /tmp/tunr-relay-new /usr/local/bin/tunr-relay
sudo chmod 755 /usr/local/bin/tunr-relay

# Restart service
sudo systemctl restart tunr-relay
```

### 2.5 Relay and service checks

```bash
sudo systemctl status tunr-relay
journalctl -u tunr-relay -n 30 --no-pager
curl -s http://localhost:8080/api/v1/health
```

**Expected response:** `{"status":"ok","timestamp":1734567890}` (timestamp should be numeric).

**If no response (empty, connection refused, or timeout):**

1. **Is the service actually running?**
   ```bash
   sudo systemctl status tunr-relay
   ```
   It should be `active (running)`. If it is `failed` or `inactive`:
   ```bash
   journalctl -u tunr-relay -n 50 --no-pager
   ```
   Check startup errors (e.g. missing `TUNR_JWT_SECRET`, `Fatal`, relay startup failure).

2. **Are environment variables loaded?**  
   Relay uses `EnvironmentFile=/opt/tunr/.env`. It should include at least:
   ```bash
   sudo grep -E '^TUNR_JWT_SECRET|^PORT|^TUNR_DOMAIN' /opt/tunr/.env
   ```
   Relay will not start if `TUNR_JWT_SECRET` is empty or shorter than 32 chars.

3. **Port 8080 dinleniyor mu?**
   ```bash
   sudo ss -tlnp | grep 8080
   ```
   If there is no output, relay is likely not running or using a different port.

4. **Run manually to inspect direct error output:**
   ```bash
   cd /opt/tunr && sudo -u tunr env $(sudo cat /opt/tunr/.env | xargs) /usr/local/bin/tunr-relay
   ```
   Terminal error output (Fatal, panic, "secret too short", etc.) reveals the cause.

---

## Section 3: Application Testing

### 3.1 Service status

```bash
sudo systemctl status caddy tunr-relay tunr-dashboard docker
docker compose -f /opt/tunr/docker-compose.yml ps
```

### 3.2 Landing and install script

```bash
# Landing
curl -sI https://tunr.sh | head -5
curl -sI https://www.tunr.sh | head -5

# Install script (check content header)
curl -s https://tunr.sh/install.sh | head -20
```

### 3.3 Relay and TLS

```bash
# Relay health (from server or externally)
curl -s https://relay.tunr.sh/api/v1/health

# TLS
curl -vI https://tunr.sh 2>&1 | grep -E "TLS|subject|expire"
```

### 3.4 Dashboard (varsa)

```bash
curl -sI https://app.tunr.sh | head -5
```

### 3.5 End-to-end tunnel test (on your machine)

```bash
# 1. Update CLI (from latest release)
curl -sL https://tunr.sh/install.sh | sh
# veya: brew upgrade ahmetvural79/tap/tunr

# 2. Test HTTP server
python3 -m http.server 9999 &

# 3. Open tunnel (if relay defaults to tunr.sh)
tunr share --port 9999

# 4. Open output URL in browser; verify content from port 9999
# 5. When done: kill %1 (stop python server)
```

If these steps pass, release + deploy workflow is healthy.

---

## Section 4: Login and Paddle Sandbox Testing

This section is aligned with [DEPLOY_AND_TEST_WALKTHROUGH.md](./DEPLOY_AND_TEST_WALKTHROUGH.md) and [PRODUCTION_SETUP.md](./PRODUCTION_SETUP.md): Magic link (Resend) + Paddle sandbox flow.

### 4.1 Prerequisites

- Dashboard (`app.tunr.sh`) is up and Resend/Paddle env variables are defined in server/dashboard environment.
- Paddle is in **Sandbox** mode with `NEXT_PUBLIC_PADDLE_ENV=sandbox` (or equivalent).
- Paddle webhook is configured to `https://app.tunr.sh/api/webhooks/paddle` (or your dashboard URL) with events: `subscription.created`, `subscription.updated`, `subscription.canceled`, `subscription.past_due`.

### 4.2 Magic link sign-in

1. Open **https://app.tunr.sh** in browser.
2. Enter email and complete "Sign in" / "Send magic link" step.
3. Click the magic link email sent by Resend.
4. Confirm dashboard login succeeds; verify by sign-out/sign-in again.

**Troubleshooting:**

- If magic link does not arrive: check Resend domain verification, `RESEND_API_KEY`, spam folder.
- If dashboard does not load: check `journalctl -u tunr-dashboard -f` and env variables.

### 4.3 Pro verification with Paddle sandbox

1. **Paddle sandbox test card**  
   Use [Paddle sandbox test data](https://developer.paddle.com/concepts/sandbox) (card number, expiry, CVC).

2. **Upgrade to Pro from dashboard**  
   - After sign-in, click the "Upgrade to Pro" (or equivalent) button.  
   - Paddle Checkout (sandbox) should open.

3. **Complete sandbox checkout**  
   - Complete payment using Paddle sandbox test card data.  
   - On successful flow, Paddle should send `subscription.created` / `subscription.updated` webhooks.

4. **Webhook and plan checks**  
   - Verify user plan updates to "Pro" (or target plan) in dashboard.  
   - Confirm webhook requests return 2xx in dashboard/relay logs if needed.

5. **Quick Pro feature test**  
   - Test at least one Pro-only feature (for example custom subdomain or higher limits).

**Webhook debugging:**

- Search dashboard/API logs for `Paddle webhook` / `subscription`.
- Review Paddle Developer Tools -> Notifications -> webhook delivery logs.
- Ensure `PADDLE_WEBHOOK_SECRET` exactly matches Paddle webhook secret.

---

## Quick Reference: Execution Order

| Step | Action | Where |
|------|------------|--------|
| 1 | Run `go test` + lint, create tag (`v0.1.1`), `git push origin v0.1.1` | Local |
| 2 | Publish draft release from GitHub Releases | GitHub |
| 3 | SSH into server, update code with `git pull` or rsync | Server |
| 4 | Update landing files: `cp -r src/landing/* /var/www/tunr/`, copy install.sh | Server |
| 5 | Relay: `cd relay && go build ...`, move binary to `/usr/local/bin`, `systemctl restart tunr-relay` | Server |
| 6 | Smoke tests: landing, install.sh, relay health, tunnel test | Server + local |
| 7 | Test magic link login at app.tunr.sh | Browser |
| 8 | Test Pro upgrade + webhook/plan checks in Paddle sandbox | Browser + logs |

---

## Common Problems

| Symptom | Possible cause | Where to inspect |
|---------|-------------|----------------|
| Missing release binary assets | GoReleaser failed | GitHub Actions -> Release workflow logs |
| install.sh "Failed to fetch latest version" | Latest release only has source code assets | See section "install.sh 404 and missing release binaries" |
| Relay health returns 502/connection refused | `tunr-relay` crashed or port 8080 closed | `systemctl status tunr-relay`, `journalctl -u tunr-relay` |
| Tunnel does not open | Relay reachability or DNS issue (relay.tunr.sh, *.tunr.sh) | Caddy, Cloudflare DNS (relay/* DNS only) |
| Magic link gelmiyor | Resend key, domain, from adresi | Resend dashboard, env, spam |
| Paddle plan does not update | Webhook secret, URL, or event mismatch | Paddle webhook logs, backend webhook handler logs |

---

## install.sh 404 and Missing Release Binaries

**Symptom:** After `curl -sL https://tunr.sh/install.sh | sh`, you see "Installing tunr 0.1.3 (darwin/arm64)..." followed by **curl: (56) 404**.

**Cause:** Install script uses GitHub **latest** release. Latest (v0.1.3) has only 2 assets (source code). GoReleaser binaries (`tunr_0.1.3_darwin_arm64.tar.gz`, `checksums.txt`, etc.) are uploaded only by tag-triggered Release workflows, not manual release creation.

**What should exist in a release (similar to [slim](https://github.com/kamranahmedse/slim/releases)):** `checksums.txt` + `tunr_<ver>_darwin_amd64.tar.gz`, darwin_arm64, linux_amd64, linux_arm64, windows_amd64.zip + source code (7+ assets).

**Fix:**  
1. Push a **new tag** for a binary-backed release: `git tag -a v0.1.4 -m "tunr v0.1.4"` -> `git push origin v0.1.4`  
2. After GitHub Actions **Release** workflow completes, publish the generated draft release.  
3. If you want to fix v0.1.3, delete release/tag, push tag again (`git push origin :refs/tags/v0.1.3` then recreate+push), and publish the new draft.
4. Workflow hata veriyorsa: Actions → Release → son run log (go test, goreleaser, relay build).

---

## If Relay Still Won't Run (Quick Diagnosis)

The most common startup issue is missing **`TUNR_JWT_SECRET`** or a value shorter than 32 characters. Relay exits with Fatal in that case.

**1. Check logs (identify startup failure):**
```bash
journalctl -u tunr-relay -n 50 --no-pager
```
If you see lines like these, root cause is clear:
- `TUNR_JWT_SECRET env variable not set` -> secret missing
- `TUNR_JWT_SECRET too short (N characters)` -> must be at least 32 chars

**2. Generate a secret and update `.env`:**
```bash
# On server - generate a random 32-char secret
NEW_SECRET=$(openssl rand -hex 32)
echo "TUNR_JWT_SECRET=$NEW_SECRET"
```
Copy this output into `/opt/tunr/.env` as `TUNR_JWT_SECRET=...` (or add it). If dashboard (Next.js) verifies same JWT signature, use the **same** value as `JWT_SECRET` / `NEXTAUTH_SECRET`.

**3. Restart service:**
```bash
sudo systemctl restart tunr-relay
sleep 2
curl -s http://localhost:8080/api/v1/health
```
Response should be `{"status":"ok","timestamp":...}`.

**4. Still failing?**  
Apply checks from Section 2.5 (port listening, manual run). If `DATABASE_URL` is invalid, relay may continue in-memory mode; if JWT secret is missing, relay will **not** start.

---

*This guide is aligned with [PRODUCTION_SETUP.md](./PRODUCTION_SETUP.md), [RELAY_SERVER.md](./RELAY_SERVER.md), [DEPLOY_AND_TEST_WALKTHROUGH.md](./DEPLOY_AND_TEST_WALKTHROUGH.md), and [LAUNCH_WALKTHROUGH.md](./LAUNCH_WALKTHROUGH.md). Last updated: March 2026.*
