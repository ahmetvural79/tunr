# Tunr Architecture & Under The Hood

This document outlines the high-level architecture of `tunr`, its relay infrastructure, and local binary system.

## 1. Local CLI Binary (The Core)
The heart of Tunr is a singular, statically compiled Go binary (`cmd/tunr`). It is completely cross-platform, natively compiled for Linux, macOS (Intel & Silicon), and Windows with CGO disabled. 
- **No Node.js/Python dependencies:** A pure Go client utilizing `Cobra` for CLI, and the native `net/http` package.
- **WebSocket Gateway:** Creates an encrypted tunnel with the Remote Relay using WebSockets and handles concurrent bidirectional HTTP transmission.

## 2. Advanced Local Proxy (The Middleware Hub)
Inside `internal/proxy`, Tunr hosts an intelligent `ReverseProxy`. It is not just a dumb pipe. It houses several custom middleware for the modern web:

### Real-time Inspector Ring Buffer (Faz 3)
A continuous `bytes.Buffer` acting as an interceptor ring. The last 1,000 requests are captured with exact body, headers (sanitized), and response times, readable locally through a streaming websocket embedded web-UI (`tunr open`).

### Vibecoder Proxy (Faz 8)
An arsenal of proxy-level manipulations specifically designed for safe, bullet-proof client demos.
1. **The Freeze Cache:** Implements a localized 5MB threshold `InMemory` Cache Layer. Every successful HTTP `200` GET request (html, scripts, assets) is securely hashed and stored. If local Next.js/Go server process gets killed or issues 5xx errors, the proxy silently routes from cache and marks the response with `X-Tunr-Freeze-Cache: 1`. 
2. **Read-Only / Mutate-Blocker:** Identifies incoming `POST`, `PUT`, `DELETE` operations using the `http.Method` attribute. Blocks database altering behavior. Synthesizes an identical artificial `200 Success` or `201 Created` HTTP Response with placeholder json values to satisfy frontend application states smoothly.
3. **HTML AST Injection:** The HTTP Response Writer is overridden (`http.ResponseWriter`). The compressed data (e.g., `gzip`) is automatically decoded via `compress/gzip` reader natively, parsed to find `</body>`, injected with `<script scr="...">` feedback/error UI modules, and cleanly re-encoded back to the network chunk. All transpiring under 12ms proxy latency.

## 3. The Relay Server (tunr.sh Anycast)
Tunr natively hosts an open-source Relay architecture (`relay/cmd/server/main.go`).
- **Connection Registry:** Stores active node pairs maintaining real-time subdomains mapped to respective client websocket IDs. Memory bounded and horizontally scalable.
- **Fly.io Multi-Region Anycast:** Deployed across datacenters (ams, sjc, sin), matching clients with the geographically closest relay over BGP routing. 
- **Postgres Metadata:** Retains historical tunnel footprints, registered client keys, and webhook subscription plans managed by Paddle billing architecture (`tunr/internal/billing`).

## 4. Auth & Secrets
- `tunr` implements its own `auth.go` module utilizing standard OS-keychain methodologies to avoid laying plain-text `.env` tokens in workspace directories. 
- Tunnel initiation requires either proper Cloudflare free/token mapping or the `tunr.sh` PKCE / Magic Link logic encapsulated inside `relay/internal/auth`.
