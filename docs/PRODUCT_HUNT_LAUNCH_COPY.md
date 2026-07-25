# tunr — Product Hunt Launch Copy

Ready-to-paste content for the Product Hunt submission form. This is the *what you publish* companion to `PRODUCT_HUNT_LAUNCH.md` (the *how you launch* playbook). Everything below is copy you can drop straight into the PH form, Twitter/X, and email on launch day.

> Verify every feature claim against the shipped build before posting — only list what works end-to-end today. The "Feature checklist" at the bottom maps each claim to where it lives in the code.

---

## 1. Core listing fields

**Name:** tunr

**Tagline (≤60 chars) — pick one:**
- `Ngrok for vibecoders — share localhost in one command` (53)
- `Public URL for your localhost. Open-source. One command.` (56)
- `Localhost tunnels with a freeze cache and AI/MCP built in` (57)

**Recommended:** the first — it names the audience and the closest reference product in one line.

**Topics / categories:** Developer Tools, Open Source, SaaS, Productivity, Artificial Intelligence

**Links:**
- Website: https://tunr.sh
- GitHub: https://github.com/ahmetvural79/tunr
- Docs: https://tunr.sh/docs
- Pricing: https://tunr.sh/#pricing

---

## 2. Description (≤260 chars)

> tunr gives your localhost a public HTTPS URL in one command. HTTP, TCP, UDP & TLS tunnels, a built-in request inspector, freeze cache that survives crashes, password/IP/token protection, and an MCP server so Claude & Cursor can share ports for you. Open-source CLI.

(258 chars — trim the last sentence if PH counts differently.)

---

## 3. First comment (the maker's comment)

This is the highest-leverage text on the page. It runs immediately after you publish.

```
Hey Product Hunt 👋

I built tunr because every localhost tunnel I tried did the boring part well
(give me a public URL) and then stopped right where my actual workflow started.

So tunr ships the boring part — `tunr share --port 3000` → instant HTTPS URL —
plus the things I kept wishing for:

🧊  Freeze cache — when your dev server crashes mid-demo, tunr keeps serving the
    last good response instead of a connection-refused page. Demos don't die.

🔎  Built-in request inspector — every request/response, replayable, exportable
    as curl. No second tab, no third-party debugger.

🔌  More than HTTP — raw TCP, UDP and end-to-end TLS tunnels (databases, SSH,
    game servers, SNI pass-through the relay can't read).

🛡️  Protect any tunnel — password, bearer token, IP allowlist, header rewriting,
    custom subdomains/domains, auto-expiry — all flags, no dashboard required.

🤖  MCP server — point Claude or Cursor at tunr and the AI can open/close tunnels
    for you while it codes. This is the part I use every day now.

📦  Open-source CLI, single static Go binary, self-hostable relay.

Free tier is generous; Pro adds custom subdomains, higher limits and the
inspector history. Would love your honest feedback — especially on what would
make you switch from whatever you use today.

— Ahmet
```

---

## 4. Gallery captions

One line per gallery image/GIF. Lead with the hero GIF.

1. **Hero GIF** — `tunr share --port 3000` → public HTTPS URL appears in <1s, opened in a browser.
2. **Freeze cache** — split screen: dev server crashes, public URL still serves the last good page (`X-Tunr-Freeze-Cache: 1`).
3. **Request inspector** — local dashboard showing live requests, click → full headers/body, "Replay" and "Copy as curl".
4. **Protect a tunnel** — `tunr share -p 8080 --password admin:secret --allow-ip 1.2.3.0/24 --ttl 1h`.
5. **TCP / UDP / TLS** — `tunr tcp -p 5432` exposing Postgres; `tunr tls -p 8443` for SNI pass-through.
6. **MCP / AI** — Claude opening a tunnel via the tunr MCP server inside an editor.
7. **Dashboard** — account view: active tunnels, today's requests & bandwidth, plan/usage.

---

## 5. Social copy

**Launch tweet/X (with hero GIF):**
```
tunr is live on Product Hunt 🚀

Your localhost → public HTTPS URL in one command:

  tunr share --port 3000

Open-source. HTTP/TCP/UDP/TLS. Request inspector. Freeze cache so demos
survive a crash. And an MCP server so Claude can open tunnels for you.

Would love your support 👇
[PH link]
```

**Follow-up tweet (thread, the differentiator):**
```
The feature I didn't know I needed: freeze cache.

Dev server crashes mid-demo → tunr serves the last good response instead of
"connection refused". Your client never sees the error.

Every other tunnel just dies. 🧊
```

**LinkedIn / email blast (short):**
```
I just launched tunr on Product Hunt — an open-source localhost tunnel for
developers. One command for a public HTTPS URL, plus a request inspector,
crash-proof freeze cache, TCP/UDP/TLS support, and an MCP server so AI
assistants can open tunnels while they code.

If you've ever used ngrok, Pinggy or Cloudflare Tunnel, I'd love your take:
[PH link]
```

---

## 6. Answers to likely PH questions

Pre-write these so you reply in seconds, not minutes.

**"How is this different from ngrok / Pinggy / Cloudflare Tunnel?"**
> Three things: (1) it's open-source with a self-hostable relay and a single static binary; (2) demo-survival features other tunnels don't have — freeze cache, demo (read-only) mode, feedback-widget injection; (3) a first-class MCP server so AI coding assistants open tunnels for you. Plus the usual: TCP/UDP/TLS, inspector, custom domains, password/IP/token auth.

**"Is it really free?"**
> Yes — the free tier covers everyday local dev (1 tunnel, generous request limits). Pro unlocks custom subdomains, higher concurrency/limits and inspector history.

**"Can I self-host?"**
> Yes. The relay is in the repo; `docs/SELF_HOSTING.md` walks through it. The CLI points at any relay via `--domain`.

**"Which platforms?"**
> macOS, Linux, Windows (amd64 + arm64). One static binary, no dependencies. SDKs for Python, Node and Go shell out to it.

**"Telemetry / privacy?"**
> Auth tokens live in the OS keychain, never on disk. TLS tunnels are end-to-end — the relay can't read them. Self-host if you want zero third-party hops.

---

## 7. Pre-launch checklist (product surface)

- [ ] `tunr.sh` landing loads fast, hero shows the one-liner, PH badge embedded.
- [ ] `https://tunr.sh/docs` reachable; quick-start copy-pastes cleanly.
- [ ] `install.sh` works on a clean macOS + Linux box (`curl … | sh`).
- [ ] Free signup → first tunnel works without hitting a paywall.
- [ ] Dashboard shows **real** usage numbers (requests/bandwidth now metered server-side).
- [ ] Relay health: `curl https://relay.tunr.sh/api/v1/health` → `{"status":"ok"}`.
- [ ] Paddle checkout opens for Pro and Team (price IDs wired — see §8).
- [ ] Status page / fallback ready in case launch traffic spikes the relay.

---

## 8. Feature checklist → code (verify before you claim it)

| Claim | Where it lives | Status |
|---|---|---|
| `tunr share` HTTP/HTTPS tunnel | `cmd/tunr/share.go`, `relay/internal/relay/proxy.go` | ✅ shipped |
| TCP / UDP / TLS tunnels | `cmd/tunr/{tcp,udp,tls_cmd}.go`, `relay/.../handler.go` | ✅ shipped |
| Request inspector + replay + curl export | `internal/inspector/`, `cmd/tunr/replay.go` | ✅ shipped |
| Freeze cache | `internal/proxy/freeze.go` | ✅ shipped |
| Demo (read-only) mode | `internal/proxy/demo.go` | ✅ shipped |
| Password / bearer token / IP allowlist | `internal/proxy/auth_*_middleware.go` | ✅ shipped |
| Header rewrite, CORS, X-Forwarded-For | `internal/proxy/auth_token_middleware.go` | ✅ shipped |
| Custom subdomain / domain (Pro) | `cmd/tunr/share.go`, `relay/.../handler.go` | ✅ shipped (Pro-gated) |
| TTL / auto-expiry, QR code | `cmd/tunr/share.go` | ✅ shipped |
| MCP server | `internal/mcp/`, `cmd/tunr/mcp.go` | ✅ shipped |
| Dashboard usage (requests/bandwidth) | `relay/.../registry.go` (`RecordRequest`/`UserUsage`), `user_api.go` | ✅ now metered |
| SDKs (Python/Node/Go) | `sdk/{python,node,go}/` | ✅ shipped |
| SAML/Okta SSO | — | ⚠️ not shipped — don't claim |
| Outgoing webhooks (tunnel events) | — | ⚠️ not shipped — don't claim |

> Anything marked ⚠️ must **not** appear in the listing, gallery, or comments until it ships. Over-claiming on PH gets called out fast.
