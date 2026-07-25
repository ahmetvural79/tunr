# tunr.sh — Deploy & Test Walkthrough

> **Prerequisites done:** Hetzner VPS running, tunr.sh domain in Cloudflare.

This walkthrough provides step-by-step guidance to go live with tunr.sh, test it, and make auth work with Paddle ($5 Pro) + Resend (magic link).

---

## Architecture Summary

```
┌─────────────────────────────────────────────────────────────────────┐
│  tunr.sh (Cloudflare)                                               │
├─────────────────────────────────────────────────────────────────────┤
│  tunr.sh, www.tunr.sh     →  Hetzner (Caddy)  →  Landing (static)   │
│  app.tunr.sh              →  Netlify          →  Next.js Dashboard   │
│  relay.tunr.sh (opsiyonel) →  Hetzner         →  Placeholder / API   │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  GitHub Repositories                                                │
│  • ahmetvural79/tunr        — CLI, relay, landing (static)          │
│  • ahmetvural79/tunr-dashboard — Next.js dashboard (separate repo) │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  External Services                                                  │
│  • Paddle  — $5/mo Pro billing                                      │
│  • Resend  — magic-link email auth                                  │
│  • GitHub  — CLI binary releases (install script downloads here)    │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Prerequisite: GitHub Release

Install script downloads binaries from GitHub Releases. Create a release after your first push:

```bash
# On local machine
cd /path/to/tunr
git tag -a v0.1.0 -m "tunr v0.1.0"
git push origin v0.1.0
```

GitHub Actions release workflow builds and uploads binaries. After release publish, `curl -sL https://tunr.sh/install.sh | sh` works.

---

## Section A: Hetzner — Landing and Caddy

### Step A.1 — Connect to Server via SSH

```bash
ssh root@<HETZNER_IP>
```

### Step A.2 — Base Setup (If Not Done Yet)

```bash
apt update && apt upgrade -y

# Firewall
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw enable

# Cloudflare DNS plugin for Caddy
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
xcaddy build --with github.com/caddy-dns/cloudflare --output /usr/local/bin/caddy
setcap cap_net_bind_service=+ep /usr/local/bin/caddy

useradd -r -s /usr/sbin/nologin caddy
mkdir -p /etc/caddy /var/log/caddy
chown caddy:caddy /var/log/caddy
```

### Step A.3 — Cloudflare API Token

1. [Cloudflare Dashboard](https://dash.cloudflare.com/) → Profile → API Tokens → Create Token  
2. Template: **Edit zone DNS**  
3. Zone: `tunr.sh`  
4. Copy the token -> we will add it to server `.env`

### Step A.4 — DNS Records (Cloudflare)

Using your server IP:

| Type | Name | Content       | Proxy  |
|------|------|---------------|--------|
| A    | `@`  | `<SERVER_IP>` | ☁️ Proxied |
| A    | `www`| `<SERVER_IP>` | ☁️ Proxied |
| CNAME| `app`| Netlify URL | ☁️ Proxied |

> **Note:** `app.tunr.sh` should point to Netlify. Netlify provides this CNAME when you add custom domain.

### Step A.5 — Copy Landing Files to Server

**From local machine:**

```bash
# From project directory
rsync -avz --exclude='.git' --exclude='node_modules' \
  ./landing/ root@<HETZNER_IP>:/var/www/tunr/
rsync -avz install.sh root@<HETZNER_IP>:/var/www/tunr/
```

**Alternative (if repo is public):**

```bash
ssh root@<HETZNER_IP>
mkdir -p /var/www/tunr
cd /tmp
git clone --depth 1 https://github.com/ahmetvural79/tunr.git
cp -r tunr/landing/* /var/www/tunr/
cp tunr/install.sh /var/www/tunr/
chown -R caddy:caddy /var/www/tunr
chmod -R 755 /var/www/tunr
chmod 644 /var/www/tunr/install.sh
```

### Step A.6 — Caddyfile and Caddy Service

```bash
# Environment file (CF token here)
cat > /opt/tunr/.env << 'ENV'
CF_API_TOKEN=<CLOUDFLARE_API_TOKEN>
ENV
chmod 600 /opt/tunr/.env

# Caddyfile
cat > /etc/caddy/Caddyfile << 'CADDY'
tunr.sh, www.tunr.sh {
    tls {
        dns cloudflare {env.CF_API_TOKEN}
    }

    root * /var/www/tunr
    file_server

    handle /install.sh {
        root * /var/www/tunr
        file_server
    }

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
    }
}
CADDY

# Caddy systemd
cat > /etc/systemd/system/caddy.service << 'SVC'
[Unit]
Description=Caddy Web Server
After=network.target

[Service]
User=caddy
Group=caddy
ExecStart=/usr/local/bin/caddy run --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile
EnvironmentFile=/opt/tunr/.env
Restart=on-failure
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
SVC

systemctl daemon-reload
systemctl enable --now caddy
systemctl status caddy
```

### Step A.7 — Landing Test

```bash
curl -sI https://tunr.sh | head -5
curl -s https://tunr.sh/install.sh | head -10
```

`https://tunr.sh` should open in browser.

---

## Section B: Netlify — Next.js Dashboard

> **Note:** Dashboard is developed in `landing/app/` but **not committed to tunr repo** (`.gitignore`). It should be pushed to a separate GitHub repo (`tunr-dashboard`, etc.) and deployed by Netlify from there.

### Step B.1 — Create Separate GitHub Repo and Push Dashboard

1. Create a new GitHub repo: **ahmetvural79/tunr-dashboard** (public, empty or with README).
2. Push local `landing/app/` contents to this repo:

```bash
cd /path/to/tunr/landing/app

# Add new repo as remote
git init
git remote add origin git@github.com:ahmetvural79/tunr-dashboard.git

# `.gitignore` already exists; add everything except node_modules
git add .
git commit -m "feat: initial dashboard"
git branch -M main
git push -u origin main
```

> `landing/app/` is already a Next.js project (`package.json`, `next.config.ts`, etc.). In tunr repo it is ignored, so you do not push it there.

### Step B.2 — Create Site on Netlify

1. [Netlify](https://app.netlify.com/) → Add new site → Import an existing project  
2. Connect GitHub -> select **ahmetvural79/tunr-dashboard**  
3. Build settings (entire repo is Next.js, so base directory is empty):
   - **Base directory:** *(leave empty)*
   - **Build command:** `npm run build`
   - **Publish directory:** `.next` (Netlify auto-detects Next.js)

### Step B.3 — Custom Domain: app.tunr.sh

1. Netlify → Site settings → Domain management → Add custom domain  
2. `app.tunr.sh` gir  
3. Add Netlify-provided CNAME in Cloudflare:
   - Type: `CNAME`
   - Name: `app`
   - Target: `xxxx.netlify.app` (copy from Netlify)  
   - Proxy: ☁️ Proxied (or DNS only - Netlify handles TLS)

### Step B.4 — Netlify Environment Variables

Netlify → Site settings → Environment variables:

| Variable | Value | Description |
|----------|-------|-------------|
| `NEXT_PUBLIC_APP_URL` | `https://app.tunr.sh` | Dashboard URL |
| `RESEND_API_KEY` | `<Resend API Key>` | Magic link delivery |
| `RESEND_FROM` | `tunr@tunr.sh` or `noreply@tunr.sh` | Sender address |
| `PADDLE_API_KEY` | `<Paddle API Key>` | Pro plan pricing |
| `PADDLE_WEBHOOK_SECRET` | `<Paddle Webhook Secret>` | Webhook signature verification |
| `PADDLE_ENVIRONMENT` | `sandbox` or `production` | Test / prod |
| `NEXTAUTH_SECRET` or `JWT_SECRET` | `<random 32 byte hex>` | Session / JWT signing |

> If your Resend and Paddle accounts are ready, fill these values.

---

## Section C: Paddle — $5 Pro Plan

### Step C.1 — Create Product in Paddle

1. [Paddle Dashboard](https://vendor.paddle.com/) → Products → Create product  
2. **Name:** tunr Pro  
3. **Price:** $5/month (recurring)  
4. **Billing cycle:** Monthly  
5. Note the Product ID

### Step C.2 — Checkout Link

1. Products → tunr Pro → Pricing  
2. Create a checkout link or copy one from Checkout Links  
3. Use this link in the dashboard "Upgrade to Pro" button

### Step C.3 — Webhook

1. Paddle → Developer Tools → Webhooks  
2. Add endpoint: `https://app.tunr.sh/api/webhooks/paddle`  
3. Events: `subscription.created`, `subscription.updated`, `subscription.canceled`, `subscription.past_due`  
4. Copy webhook secret -> add as Netlify `PADDLE_WEBHOOK_SECRET`

> **Note:** Webhook URL should point to a Next.js API route. Since `app.tunr.sh` is hosted on Netlify, Netlify receives this request.

---

## Section D: Resend — Magic Link Auth

### Step D.1 — Resend Domain

1. [Resend](https://resend.com/) → Domains  
2. Add `tunr.sh`  
3. Add provided DNS records to Cloudflare (SPF, DKIM, etc.)  
4. Wait until verification completes

### Step D.2 — API Key

1. Resend → API Keys → Create  
2. Copy key -> add as Netlify `RESEND_API_KEY`  

### Step D.3 — Magic Link Flow (Next.js side)

In the Next.js app:

1. `/api/auth/send-magic-link` -> receives email, sends link via Resend  
2. Link: `https://app.tunr.sh/auth/verify?token=xxx`  
3. `/auth/verify` -> validates token, creates JWT/session, redirects to dashboard  

Example Resend usage:

```javascript
// pages/api/auth/send-magic-link.js veya app/api/auth/send-magic-link/route.js
import { Resend } from 'resend';

const resend = new Resend(process.env.RESEND_API_KEY);

const token = generateSecureToken(); // crypto.randomBytes(32).toString('hex')
await saveMagicLinkToken(email, token, expiresAt);

await resend.emails.send({
  from: 'tunr@tunr.sh',
  to: email,
  subject: 'Sign in to tunr',
  html: `Click to sign in: <a href="https://app.tunr.sh/auth/verify?token=${token}">Sign in</a>`,
});
```

---

## Section E: Test Plan

### E.1 — Landing

- [ ] `https://tunr.sh` opens  
- [ ] `https://www.tunr.sh` opens  
- [ ] `https://tunr.sh/install.sh` works (validated with curl)

### E.2 — Install Script

```bash
curl -sL https://tunr.sh/install.sh | sh
```

> **Required:** At least one GitHub release must exist (`v0.1.0`, etc.). Without a release, install script fails.

### E.3 — Tunnel via CLI

```bash
tunr share --port 3000
```

> **Note:** CLI currently uses Cloudflare quicktunnel. `relay.tunr.sh` is not yet implemented here; tunnel URLs are in `*.trycloudflare.com` format.

### E.4 — Dashboard (app.tunr.sh)

- [ ] `https://app.tunr.sh` opens  
- [ ] Magic link sign-in works  
- [ ] Pro upgrade button goes to Paddle checkout  
- [ ] Paddle webhook receives subscription updates (verifiable in logs)

### E.5 — Paddle Sandbox

1. Use Paddle test card in sandbox mode  
2. Attempt Pro upgrade from dashboard  
3. Paddle checkout tamamla  
4. Verify webhook is processed successfully (subscription status should update)

---

## Quick Start Summary

| Order | Task | Location |
|------|--------|-------|
| 1 | Landing + install.sh deploy | Hetzner |
| 2 | Caddy + TLS | Hetzner |
| 3 | app.tunr.sh → Netlify | Netlify + Cloudflare |
| 4 | Paddle $5 product + webhook | Paddle |
| 5 | Resend domain + magic link | Resend + Next.js |
| 6 | First release on GitHub | GitHub |

---

## Common Errors and Fixes

### Caddy TLS error

- Verify Cloudflare token has Zone:DNS:Edit permission  
- Verify `CF_API_TOKEN` value and that Caddy reads `/opt/tunr/.env`  

### app.tunr.sh not loading

- Verify Cloudflare CNAME points to correct Netlify domain  
- Verify custom domain is validated in Netlify  

### Install script hata veriyor

- Verify a release exists in `ahmetvural79/tunr`  
- Verify release has binary for target OS/arch (e.g. `tunr_0.1.0_linux_amd64.tar.gz`)  

### Magic link gelmiyor

- Resend domain verify edildi mi  
- Check spam folder  
- Check delivery status in Resend logs  

---

*Last updated: March 2026*
