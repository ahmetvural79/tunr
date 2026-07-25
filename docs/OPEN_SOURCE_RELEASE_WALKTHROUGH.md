# tunr Open Source Release Walkthrough

Version: **0.3.0** — TCP Tunnels, Multi-Region, Python & Node.js SDKs

---

## Release Overview

This release marks the most significant feature update to `tunr` since its initial launch. It introduces TCP tunnel support, multi-region relay routing, and official Python & Node.js SDKs for programmatic tunnel management.

### What's New

| Category | Feature | Description |
|----------|---------|-------------|
| **Protocol** | TCP Tunnels | Expose databases, SSH servers, Redis, any TCP service |
| **Protocol** | Multi-Region Routing | `--region` flag for relay selection (ams, sea, sin) |
| **SDK** | Python SDK (`pip install tunr`) | Programmatic tunnel creation, request inspection, replay |
| **SDK** | Node.js SDK (`npm i @tunr/cli`) | EventEmitter-based tunnel lifecycle management |
| **Security** | IP Whitelisting | CIDR-based access control (`--allow-ip`) |
| **Security** | Bearer Token Auth | API key protection (`--auth-token`) |
| **Debugging** | Live Header Modification | Add, replace, remove headers on the fly |
| **Debugging** | QR Code Sharing | Scannable QR codes for mobile testing |
| **Developer** | CORS Preflight | `--cors-origin` for cross-origin requests |
| **Developer** | Forwarded Headers | `X-Forwarded-For`, `X-Original-URL` injection |

---

## New Feature Deep Dive

### TCP Tunnels

TCP tunnels allow raw byte forwarding — no HTTP parsing. Perfect for:

- **Databases**: PostgreSQL, MySQL, MongoDB, Redis
- **SSH**: Remote server access from anywhere
- **Game Servers**: Minecraft, Valheim, custom game backends
- **IoT Devices**: MQTT, custom binary protocols

#### Usage

```bash
# Simple TCP tunnel
tunr tcp --port 5432

# TCP tunnel with QR code
tunr tcp --port 22 --qr

# TCP tunnel with IP restriction
tunr tcp --port 6379 --allow-ip 10.0.0.0/8

# TCP tunnel in specific region
tunr tcp --port 5432 --region ams
```

#### Architecture

```
Application  ──TCP──>  [tunr relay edge]  ──WebSocket──>  [tunr CLI]  ──TCP──>  localhost:PORT
   (browser)               (relay.tunr.sh)                  (your machine)
```

TCP data is tunneled over the same WebSocket control channel using base64-encoded payloads with stream ID multiplexing.

#### Relay Message Types

| Message Type | Direction | Purpose |
|--------------|-----------|---------|
| `tcp_open` | Relay → CLI | New inbound TCP connection |
| `tcp_data` | Bidirectional | Raw TCP data (base64) |
| `tcp_close` | Relay → CLI | Connection shutdown |

---

### Multi-Region Routing

The `--region` flag allows you to select a preferred relay region. This matters for latency-sensitive applications and global teams.

#### Usage

```bash
# European relay (Amsterdam)
tunr share --port 3000 --region ams

# US West relay (Seattle)
tunr share --port 3000 --region sea

# Asia relay (Singapore)
tunr share --port 3000 --region sin

# TCP tunnel to specific region
tunr tcp --port 5432 --region sin
```

#### How It Works

1. CLI sends `helloData` with `Region: "ams"` during connection handshake
2. Relay reads the region and registers the tunnel with metadata
3. Future multi-region infrastructure will route to the appropriate edge
4. Currently single relay, so region is stored but not enforced

#### Registry Fields

The relay now tracks:
- `Protocol`: "http" or "tcp" (for routing decisions)
- `Region`: Preferred relay region (e.g., "ams", "sea", "sin")

---

## SDK Documentation

### Python SDK

**Package**: [`tunr` on PyPI](https://pypi.org/project/tunr/)

```bash
pip install tunr
```

#### Quick Start

```python
from tunr import TunrClient

client = TunrClient()

# Simple tunnel
tunnel = client.share(port=3000)
print(f"Tunnel URL: {tunnel.public_url}")

# With options
from tunr.client import TunnelOptions

opts = TunnelOptions(
    subdomain="myapp",
    password="demo123",
    allow_ips=["10.0.0.0/8"],
    qr=False,
    x_forwarded_for=True,
)
tunnel = client.share(port=8080, opts=opts)
```

#### API Reference

```python
# Get active tunnels
tunnels = client.get_active_tunnels()

# Fetch HTTP requests for inspection
requests = client.get_requests(tunnel_id)

# Replay a request
client.replay_request(request_id)

# Clean shutdown
tunnel.close()
```

---

### Node.js SDK

**Package**: [`@tunr/cli` on npm](https://www.npmjs.com/package/@tunr/cli)

```bash
npm install @tunr/cli
```

#### Quick Start

```typescript
import { TunrClient, TunnelOptions } from '@tunr/cli'

const client = new TunrClient()

// Simple tunnel
const tunnel = await client.share(3000)
console.log(`Tunnel URL: ${tunnel.publicUrl}`)

// With options
const opts: TunnelOptions = {
  subdomain: 'myapp',
  password: 'demo123',
  allowIps: ['10.0.0.0/8'],
  xForwardedFor: true,
}

const tunnel = await client.share(8080, opts)
```

#### Event-Based Lifecycle

```typescript
tunnel.on('ready', () => console.log('Tunnel is live'))
tunnel.on('error', (err) => console.error('Tunnel error:', err))
tunnel.on('exit', () => console.log('Tunnel closed'))

// Clean shutdown
await tunnel.close()
```

#### API Reference

```typescript
// Get active tunnels
const tunnels = await client.getActiveTunnels()

// Fetch HTTP requests
const requests = await client.getRequests(tunnelId)

// Replay a request
await client.replayRequest(requestId)

// Clean shutdown
await tunnel.close()
```

---

## Release Checklist

### Pre-Release

- [ ] All tests pass (`go test ./...`)
- [ ] Linter clean (`golangci-lint run`)
- [ ] CLI builds on all platforms (`go build -o tunr ./cmd/tunr/...`)
- [ ] Relay builds (`cd relay && go build ./cmd/server/...`)
- [ ] Python SDK version bumped (`sdk/python/pyproject.toml`)
- [ ] Node.js SDK version bumped (`sdk/node/package.json`)
- [ ] `README.md` updated with new features
- [ ] `docs/CLI.md` updated with new commands
- [ ] `docs/FEATURES.md` updated with new features
- [ ] Comparison pages created/updated vs competitors
- [ ] Landing page (`index.html`) updated
- [ ] Docs page (`docs.html`) updated

### GitHub Release

- [ ] Create new tag: `git tag v0.3.0 && git push origin v0.3.0`
- [ ] Create GitHub Release with changelog
- [ ] Attach compiled binaries (macOS, Linux, Windows, ARM64)
- [ ] Update `SECURITY.md` if needed
- [ ] Review `LICENSE` for any changes

### Python SDK Publish

- [ ] Build wheel: `cd sdk/python && python -m build`
- [ ] Test upload: `twine upload --repository testpypi dist/*`
- [ ] Production upload: `twine upload dist/*`
- [ ] Verify on PyPI
- [ ] Update `CHANGELOG.md` in Python SDK

### Node.js SDK Publish

- [ ] Build: `cd sdk/node && npm run build`
- [ ] Login: `npm login --registry https://registry.npmjs.org/`
- [ ] Test publish: `npm publish --access public --dry-run`
- [ ] Production publish: `npm publish --access public`
- [ ] Verify on npmjs.com

---

## Migration Guide (0.2.x → 0.3.0)

### Breaking Changes

**None.** All new features are additive and backward compatible.

### New CLI Flags

| Flag | Command | Default | Description |
|------|---------|---------|-------------|
| `--tcp` | `tunr share` | `false` | Create TCP tunnel instead of HTTP |
| `--region` | `tunr share/tcp` | empty | Preferred relay region |
| `--qr` | `tunr share/tcp` | `false` | Display QR code |
| `--allow-ip` | `tunr share/tcp` | none | CIDR whitelist |
| `--auth-token` | `tunr share/tcp` | none | Bearer token |

### SDK Adoption

If you were previously using undocumented CLI subprocess calls:
- **Python**: Replace subprocess with `tunr.TunrClient().share()`
- **Node.js**: Replace child_process with `Tunnel` class and events

### SDK Version Schema

Both SDKs follow the same version as the CLI: `0.3.0`

---

## Testing Strategy

### Manual Testing

```bash
# TCP tunnel test
tunr tcp --port 8080 --qr

# Multi-region test
tunr share --port 3000 --region ams
tunr share --port 3000 --region sea
tunr share --port 3000 --region sin

# Security features
tunr share --port 3000 --allow-ip 10.0.0.0/8 --auth-token secret
```

### SDK Testing

```python
# Python
python -c "
from tunr import TunrClient
client = TunrClient()
t = client.share(3000)
print(t.public_url)
t.close()
"
```

```typescript
// Node.js
npx ts-node -e "
import { TunrClient } from '@tunr/cli'
async function main() {
  const client = new TunrClient()
  const tunnel = await client.share(3000)
  console.log(tunnel.publicUrl)
  await tunnel.close()
}
main()
"
```

---

## Deployment

### Relay Server

```bash
cd relay
go build -o tunr-relay ./cmd/server/
# Deploy to Fly.io or your infrastructure
# Ensure TCP endpoints are configured
```

### Landing Pages

```bash
# After editing HTML files
python3 -m http.server 8080
# Verify all pages render correctly
```

### GitHub

```bash
git add .
git commit -m "release: v0.3.0 - TCP tunnels, multi-region, Python & Node.js SDKs"
git push origin main
git tag v0.3.0
git push origin v0.3.0
```
