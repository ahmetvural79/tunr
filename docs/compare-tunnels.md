# tunr's tunnel vs the alternatives

This page is about **tunnels only** — `tunr share`, the free on-ramp. It is
deliberately not on the front page, because tunnelling is not what tunr is for.
tunr is for [hosting the apps your agent builds](../README.md); the tunnel is
how you preview one before you ship it.

If a tunnel is genuinely all you need, any of the tools below will do the job,
and two of them are free. What follows is an honest feature comparison, not an
argument that you should switch.

A note on licensing, since these tables get quoted: **tunr's CLI is Apache-2.0**
(OSI-approved open source), while **the relay is PolyForm Shield 1.0.0**
(source-available — you can run it, you can't compete with it). Where a row
below says "open-source client", that is what it means. See [NOTICE](../NOTICE).

---

## tunr vs ngrok

| | tunr | ngrok (Personal) |
|--|------|------------------|
| Price | Free / $9 Pro | $10/month |
| Bandwidth | Unlimited | 5 GB/month |
| Demo features (freeze, read-only, feedback widget) | ✅ | ❌ |
| IP whitelisting | ✅ | Enterprise only |
| Bearer token auth | ✅ | ❌ |
| Header rewriting | ✅ | ❌ |
| QR code sharing | ✅ | ❌ |
| MCP / agent integration | ✅ | ❌ |
| Open-source client | ✅ Apache-2.0 | ❌ |
| Deploy & host apps | ✅ (preview) | ❌ |
| **Brand maturity, uptime record, enterprise support** | ⚠️ new | ✅ the category leader |

ngrok is the incumbent for a reason: it has years of operational history and a
support org. If that's what you're buying, buy that.

---

## tunr vs Cloudflare Tunnel

| | tunr | Cloudflare Tunnel |
|--|------|-------------------|
| Setup | 1 command | Cloudflare account + DNS config |
| Managed subdomain | ✅ `*.tunr.sh` | ❌ you must own a domain |
| Request inspector + replay | ✅ built in | ❌ |
| Demo features | ✅ | ❌ |
| IP whitelisting | ✅ CLI flag | ✅ via Access policies |
| Price | Free / $9 Pro | **Free, unlimited** |
| **Global edge network** | 3 regions | ✅ 300+ PoPs |

Cloudflare Tunnel is free and runs on the largest edge network in existence.
It's the right answer for permanent infrastructure; tunr is the faster answer
for "show me this in 3 seconds".

---

## tunr vs LocalXpose

| | tunr | LocalXpose (Pro) |
|--|------|------------------|
| Price | Free / $9 Pro | $8/month |
| Bearer token auth | ✅ | ❌ |
| MCP integration | ✅ | ❌ |
| Demo features | ✅ | ❌ |
| Header rewriting | ✅ | ❌ |
| Open-source client | ✅ Apache-2.0 | ❌ |

---

## tunr vs localtunnel

| | tunr | localtunnel |
|--|------|-------------|
| HTTPS tunnel | ✅ | ✅ |
| WebSocket / HMR | ✅ | ❌ |
| Custom domains | ✅ | ❌ |
| Persistent subdomains | ✅ | ❌ |
| IP whitelisting | ✅ | ❌ |
| Bearer token auth | ✅ | ❌ |
| Request inspector | ✅ | ❌ |
| Password protection | ✅ | ❌ |
| Demo / freeze / widget | ✅ | ❌ |
| Price | Free / $9 Pro | **Free** |

localtunnel is free, tiny and has zero expectations attached. If you need a URL
for ten minutes and nothing else, it's hard to beat.

---

## Self-hosting

All four alternatives above are someone else's service. tunr's tunnel relay can
be run on your own box:

```bash
cp .env.example .env      # TUNR_DOMAIN, TUNR_JWT_SECRET
docker compose up -d
tunr share --port 3000 --relay https://tunnel.yourcompany.com
```

See [SELF_HOSTING.md](SELF_HOSTING.md). The relay is PolyForm Shield licensed:
running it for yourself or your company is fine, reselling it as a competing
tunnel service is not.
