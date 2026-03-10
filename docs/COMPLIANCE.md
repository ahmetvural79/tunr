# Compliance and Data Security

> **tunr v1.0** | tunr.sh | Updated: March 2026

This document describes tunr's data processing practices, security controls, and compliance posture. Transparency is critical for open-source tools.

---

## Data Classification

| Category | Examples | Protection Level |
|----------|----------|------------------|
| **PII (Personal)** | Email address | SHA-256 hashed, encrypted in transit, anonymized after 90 days |
| **Credentials** | JWT token, auth token | Stored in OS keychain, held in memory briefly, never logged |
| **Tunnel Traffic** | HTTP request/response body | End-to-end encrypted (TLS 1.3), not stored on the relay |
| **Metadata** | URL, method, status code | 30 days (Pro), 1 day (Free), stored in PostgreSQL |
| **Billing** | Plan information | Paddle MoR — card numbers never reach tunr |
| **Logs** | System events | 90 days, PII stripped |

---

## Data Processing Principles

### Data Minimization
- Tunnel content is **never stored** on the relay server — only a temporary in-memory buffer is used
- Request bodies in the inspector buffer are truncated at a maximum of **5 MB** (default)
- No personal data is collected beyond the user's email address

### Data Deletion
- **Free:** All request logs are deleted after 24 hours
- **Pro:** 30 days
- **Team:** 90 days
- Account closure: All data is permanently deleted within 30 days (GDPR Art. 17)

### Data Residency
- Relay server: Fly.io Amsterdam (AMS) — within EU GDPR jurisdiction
- PostgreSQL: Fly.io managed Postgres (same region)
- User data is not transferred outside of the EU

---

## Access Control

### Authentication
- Magic link (email) — no passwords, no credential stuffing risk
- JWT token: 24-hour TTL, stored in the OS keychain
- MFA: On the roadmap (Phase 7)

### Authorization Model
| Resource | Access |
|----------|--------|
| Own tunnels | Full access |
| Another user's tunnel | No access (registry isolation) |
| Inspector records | Own tunnels only |
| Billing information | Account owner only |
| Admin API | None (open source — no admin exists) |

### Header Sanitization
The following headers are displayed as `[REDACTED]` in inspector records:
- `Authorization`, `Cookie`, `Set-Cookie`
- `X-Api-Key`, `X-Auth-Token`, `X-Secret`
- `Proxy-Authorization`

---

## Security Controls

### TLS / Encryption
- All relay traffic uses TLS 1.3 (Caddy/Fly.io)
- HSTS: 1 year + preload
- CLI ↔ Relay: `InsecureSkipVerify: false` (verified by tests)
- Wildcard TLS: Cloudflare DNS challenge (Let's Encrypt)

### Webhook Security
- Paddle webhook: HMAC-SHA256 signature verification
- Replay attack protection: 5-minute timestamp window
- Timing-safe comparison (`hmac.Equal`)

### Network Security
- Dashboard: `127.0.0.1` only (not exposed externally)
- Replay: Localhost target only (SSRF protection)
- Private IP ranges blocked (10.x, 172.16-31.x, 192.168.x)

---

## Incident Response

### Vulnerability Reporting
See [SECURITY.md](./SECURITY.md)

### Incident Process
1. Detection → internal notification within 1 hour
2. Impact analysis → within 4 hours
3. Temporary mitigation → within 24 hours
4. Permanent fix → within 72 hours
5. User notification → within 72 hours if affected (GDPR Art. 33/34)
6. Post-mortem → within 1 week (publicly available)

---

## Third-Party Dependencies

| Service | Purpose | Data Access | Agreement |
|---------|---------|-------------|-----------|
| Paddle | Payment processing | Plan and billing information | Merchant of Record |
| Fly.io | Relay hosting | Encrypted traffic metadata | EU SCC |
| Cloudflare | DNS + TLS | DNS queries | EU SCC |
| Let's Encrypt | TLS certificates | Domain information | Public CA |

**User email addresses and tunnel content are never shared with third parties.**

---

## SOC 2 Roadmap

| Control | Status | Target Date |
|---------|--------|-------------|
| CC1: Control Environment | 🟡 Partial | Q3 2026 |
| CC2: Communication | 🟢 Complete | — |
| CC3: Risk Assessment | 🟡 Partial | Q3 2026 |
| CC6: Logical Access Controls | 🟢 Complete | — |
| CC7: System Operations | 🟡 Partial | Q4 2026 |
| CC8: Change Management | 🟢 Complete (CI/CD) | — |
| CC9: Risk Mitigation | 🟡 Partial | Q4 2026 |
| A1: Availability | 🔴 Planned | Q4 2026 |

**Target:** SOC 2 Type I — Q4 2026 | SOC 2 Type II — Q2 2027
