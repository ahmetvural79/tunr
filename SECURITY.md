# Security Policy

## Welcome to Tunr 👋

tunr is an open-source project. This means both great things (community contributions!) and great responsibility (security!).

## Supported Versions

| Version | Security Support |
|---------|:----------------:|
| 0.x (pre-release) | ✅ Active |
| Development | ✅ Active |

## Reporting a Security Vulnerability

**Please do NOT publicly disclose security vulnerabilities via GitHub Issues.**

Doing so could put other users at risk. Instead:

### Preferred Method: GitHub Private Security Advisory

1. Go to the **"Security"** tab of this repository
2. Click the **"Report a vulnerability"** button
3. Fill in the details and submit

### Alternative: Email

For encrypted communication:
- **Email:** security@tunr.sh
- **PGP:** (Coming soon — to be added in Phase 0)

## What to Expect?

| Timeframe | Expectation |
|-----------|-------------|
| 48 hours | Report acknowledged, under review |
| 7 days | Severity assessment completed |
| 30 days | Fix or mitigation plan in place |
| 90 days | Fix released (coordinated with CVE if applicable) |

## In-Scope

We accept reports on the following topics:

- **Auth bypass** — circumventing token validation
- **SSRF** — gaining internal network access (especially via `tunr share`)
- **Path traversal** — unauthorized file system access
- **Injection** — OS command injection, log injection
- **Information disclosure** — token/secret leakage
- **Tunnel hijacking** — taking over another user's tunnel
- **DoS** — taking down the service with a single payload

## Out-of-Scope

You can open a regular issue for these, but they do not qualify as security advisories:

- Self-XSS (hacking your own browser yourself)
- Rate limiting (unless beyond reasonable thresholds)
- Missing security headers (open as an enhancement request)
- Social engineering
- Physical attacks

## Secure Development Principles

We follow these guidelines in this project:

1. **Secret management:** Tokens are never written to logs; secrets are never stored in plaintext in config files
2. **Input validation:** Every CLI argument is validated
3. **Least privilege:** No more permissions are requested than necessary
4. **Dependency audit:** `govulncheck` is mandatory in CI
5. **TLS verification:** PRs containing `InsecureSkipVerify: true` are rejected
6. **crypto/rand:** `math/rand` is never used where cryptographic security is required

## Dependabot

`dependabot.yml` is enabled for dependency security updates.
Patches for critical CVEs are released within 48 hours.

## Hall of Fame

We are grateful to those who report security vulnerabilities. Your name will be listed here 🏆

(Empty for now — maybe you'll be the first?)

---

*This policy is effective as of [Phase 0]. It will be updated as the project evolves.*
