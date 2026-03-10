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
