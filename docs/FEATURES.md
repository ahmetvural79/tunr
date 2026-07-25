# Tunr — Core Features

Tunr is designed to be the ultimate, zero-configuration local-to-public tunnel tool, specially crafted for developers, freelancers, and agencies ("Vibecoders"). 

Here is a comprehensive list of Tunr's features:

## 🚀 Core Tunneling
- **Zero Config:** Share your local server with a public URL in `< 3 seconds`. No configs, no signup required for basic usage (`tunr share --port 3000`).
- **Auto-HTTPS:** Every tunnel gets a secure, valid Let's Encrypt / Cloudflare HTTPS certificate out of the box.
- **WebSocket & HMR:** Flawless support for WebSockets and Hot Module Replacement (React, Vue, Vite, Next.js).
- **Custom Subdomains:** Claim your own permanent endpoints (e.g., `myapp.tunr.sh`) with a Pro account.

## 🛠 Developer Experience (DX)
- **Multi-Platform Single Binary:** Runs natively on macOS, Linux, and Windows with zero dependencies. No Node.js or Python required.
- **Local HTTP Inspector:** A built-in, beautifully crafted dashboard to inspect incoming requests, headers, and payloads in real-time.
- **Request Replay:** Easily replay intercepted requests from the CLI (`tunr replay <id>`) or export them as `curl` commands.
- **Secure Secret Management:** Auth tokens are securely stored in the native OS Keychain—no insecure plaintext files.

## 🤖 AI & Ecosystem
- **MCP Server Support:** Native Model Context Protocol (MCP) server. Claude Desktop and Cursor IDE can automatically spin up tunnels and inspect requests directly from chat!
- **AI-Friendly CLI:** The `--json` flag formats all outputs in machine-readable JSON for seamless automation. 
- **VS Code Extension:** Control your tunnels right from your editor's status bar and sidebar.

## 💼 Vibecoder Client Demo Features
Taking client presentations to the next level. Tunr proxy dynamically enhances your local app for safe, impressive client demos.

- **Snapshot / Freeze Mode (`--freeze`):** The proxy caches successful successful responses. If your local server crashes mid-presentation, the proxy falls back to the cache so the client never notices an error.
- **Demo Mode (`--demo`):** A read-only proxy layer. Blocks unsafe mutating requests (`POST`, `PUT`, `DELETE`) and returns a mocked success JSON. Your clients can click "Submit Order" without actually messing up your local database!
- **Feedback & Error Widget (`--inject-widget`):** Transparently injects a floating feedback UI and a JS error catcher into the HTML. Clients drop pins and notes on the UI, and JavaScript errors are caught and logged directly to your local terminal.
- **Auto-Login (`--auto-login`):** Bypasses auth screens for clients by automatically injecting required session Cookies or Headers into the proxy stream. `tunr share --auto-login "Cookie: session=demo"`

## 🔒 Enterprise & Security
- **OAuth2 SSO:** Support for Google, GitHub, and custom SAML/Okta sign-ons for Enterprise teams.
- **Strict Validation:** SSRF protection, private IP blocking, and TLS strict verification built-in.
- **Audit Logging:** Every tunnel lifecycle event is tracked for SOC2 compliance.

## 🌐 Multi-Region Routing
- **Region Selection:** Choose your preferred relay region via `--region` flag (`ams`, `sea`, `sin`).
- **Latency Optimization:** Route traffic to the nearest edge server for your users.
- **Cross-Protocol Support:** Works with HTTP, TCP, UDP, and TLS tunnels.

## 🔌 TCP Tunnels
- **Raw TCP Forwarding:** Expose databases, SSH servers, Redis, or any TCP service.
- **No HTTP Parsing:** The relay acts as a pure byte pipe — no HTTP layer overhead.
- **IP Access Control:** Restrict TCP tunnel access via CIDR whitelisting.
- **QR Code Sharing:** Generate scannable QR codes for easy mobile/device sharing.

## 🎮 UDP Tunnels (New in v0.4.0)
- **Raw UDP Forwarding:** Expose DNS servers, game servers, and real-time audio/video services.
- **Fire-and-Forget Support:** Works with both request/response and one-way datagram patterns.
- **Low Overhead:** UDP datagrams are forwarded with minimal latency through the WebSocket control channel.
- **CLI:** `tunr udp --port 53 --region ams`

## 🔐 TLS Tunnels — End-to-End Encryption (New in v0.4.0)
- **Zero-Knowledge Mode:** The relay cannot read your traffic — TLS is passed through without termination.
- **SNI-Based Routing:** Traffic is routed based on the Server Name Indication (SNI) field.
- **Compliance Ready:** Perfect for HIPAA, PCI-DSS, and other zero-trust requirements.
- **CLI:** `tunr tls --port 8443`

## 🛡️ Tunnel Security
- **Password Protection:** HTTP Basic Authentication with `--password` flag.
- **Bearer Token Auth:** Simple API key-style protection with `--auth-token`.
- **IP Whitelisting:** CIDR-based access control with `--allow-ip`.
- **Auto-Expiry:** TTL-based tunnel expiration with `--ttl 1h`.

## 🔧 Header Manipulation
- **Add Headers:** Inject custom headers on the fly (`--header-add`).
- **Replace Headers:** Override incoming headers (`--header-replace`).
- **Remove Headers:** Strip sensitive headers before forwarding (`--header-remove`).
- **Forwarded Headers:** Inject `X-Forwarded-For` and `X-Original-URL` automatically.
- **CORS Preflight:** Allow cross-origin requests with `--cors-origin`.

## 📱 Quick Sharing
- **QR Code Generation:** Instantly generate scannable QR codes for tunnel URLs (`--qr`).
- **Machine-Readable JSON Output:** All commands support `--json` for CI/CD integration.

## 🐳 Docker & Self-Hosting (New in v0.4.0)
- **Docker Image:** Run tunr from a ~15MB Alpine container: `docker run tunr share -p 3000`
- **Docker Compose Stack:** Full self-hosted deployment with relay + caddy + postgres.
- **Self-Hosting Guide:** Complete docs for running your own relay on any VPS.
- **Systemd/Launchd Service:** `tunr service install --port 3000` for auto-start on boot.

## 📊 Observability (New in v0.4.0)
- **Prometheus Metrics:** `/metrics` endpoint on port 19842 with `tunr_requests_total`, `tunr_active_tunnels`, `tunr_bytes_transferred`.
- **Health Probes:** `/healthz` and `/readyz` endpoints for Kubernetes integration.
- **Connection Draining:** Graceful shutdown waits for in-flight requests before closing.

## 🔀 Multi-Tunnel Config (New in v0.4.0)
- **Named Tunnels:** Define multiple tunnels in `.tunr.json` and start them all at once.
- **`tunr up`:** Start all tunnels from your project config file.
- **`tunr down`:** Stop all running daemon tunnels.
- **Mixed Protocols:** Run HTTP, TCP, UDP, and TLS tunnels simultaneously.

## 🌐 Corporate Proxy Support (New in v0.4.0)
- **HTTP Proxy:** `tunr share -p 3000 --proxy http://proxy:8080`
- **Environment Variables:** Respects `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` automatically.
- **SOCKS5 Support:** Works with SOCKS5 proxies for corporate firewalls.

---

# SDK Documentation

## Python SDK (`pip install tunr`)

### Tunnel Creation
```python
from tunr import TunrClient, TunnelOptions
client = TunrClient()
tunnel = client.share(port=3000)
```

### TCP / UDP / TLS Tunnels
```python
tcp_tunnel = client.tcp(port=5432)
udp_tunnel = client.udp(port=53)
tls_tunnel = client.tls(port=8443)
```

### With Options
```python
opts = TunnelOptions(
    subdomain="myapp",
    auth_token="secret",
    allow_ips=["10.0.0.0/8"],
    qr=False,
    password="demo123",
    freeze=True,
    inject_widget=True,
    x_forwarded_for=True,
    cors_origins=["https://example.com"],
    proxy="http://proxy:8080",
    ttl="2h",
)
tunnel = client.share(port=8080, opts=opts)
```

### Inspect Requests
```python
requests = client.get_requests(tunnel.subdomain)
for req in requests:
    print(f"{req['method']} {req['path']} - {req['status_code']}")
```

### Replay Request
```python
client.replay_request(subdomain, requests[0]['id'], port=3000)
```

### Observability
```python
metrics = client.get_metrics()    # Prometheus text format
health = client.health_check()    # {"status": "ok", ...}
```

## Node.js SDK (`npm install @tunr/cli`)

### Tunnel Creation
```typescript
import { TunrClient } from '@tunr/cli'
const client = new TunrClient()
const tunnel = await client.share(3000)
```

### TCP / UDP / TLS Tunnels
```typescript
const tcpTunnel = await client.tcp(5432)
const udpTunnel = await client.udp(53)
const tlsTunnel = await client.tls(8443)
```

### With Options
```typescript
const tunnel = await client.share(8080, {
  subdomain: 'myapp',
  authToken: 'secret',
  allowIps: ['10.0.0.0/8'],
  password: 'demo123',
  freeze: true,
  injectWidget: true,
  xForwardedFor: true,
  corsOrigins: ['https://example.com'],
  proxy: 'http://proxy:8080',
  ttl: '2h',
})
```

### Event-Based Lifecycle
```typescript
tunnel.on('ready', () => console.log('Tunnel live'))
tunnel.on('error', (err) => console.error('Error:', err))
tunnel.on('exit', () => console.log('Tunnel closed'))
await tunnel.close()
```

### Inspect Requests
```typescript
const requests = await client.getRequests(tunnel.subdomain)
for (const req of requests) {
  console.log(`${req.method} ${req.path} - ${req.response?.status}`)
}
```

### Replay Request
```typescript
await client.replayRequest(subdomain, requests[0].id, 3000)
```

### Observability
```typescript
const metrics = await client.getMetrics()    // Prometheus text
const health = await client.healthCheck()    // {status: "ok", ...}
```
