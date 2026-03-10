# Vibecoder Client Demo Features

The biggest fear when demoing your product — mobile, web, or desktop API — to clients or external teams is a local error derailing the entire presentation. Tunr eliminates this anxiety. In the Phase 8 update, we built four purpose-built, intelligent proxy middleware layers that sit between your client and your localhost. This document covers the technical architecture and usage of each feature.

---

## `--demo`: Safe Read-Only Mode

```bash
tunr share --port 8080 --demo
```

When developers let a client interact with a project running on their local machine, the last thing they want is forms being submitted, fake orders polluting the database, or test data corrupting real state.

**How It Works**

1. An HTTP request enters Tunr's LocalProxy layer.
2. The request method is inspected. If it is `POST`, `PUT`, `PATCH`, or `DELETE`:
3. The proxy **drops the request** — it is never forwarded to the backend application on localhost (port 8080). This is a hard request drop at the proxy level.
4. Instead, the proxy crafts a frontend-friendly mock JSON response — `{"status": "demo_success", "message": "Demo mode: Request mocked"}` — and returns it immediately with a `200 OK` HTTP status code.
5. Your site continues to feel fully interactive. The client can click buttons, submit forms, and navigate freely — but absolutely nothing is written to your database.

The result: your client gets a realistic, hands-on experience, and your data stays pristine.

---

## `--freeze`: Localhost Shield (Snapshot Cache)

```bash
tunr share --port 8080 --freeze
```

You are mid-demo. You push a code change, or your app (Nodemon, Air, etc.) crashes and starts a restart cycle. With conventional tunnels, the remote client immediately sees a `502 Bad Gateway` or "Connection Refused" screen. Panic ensues.

**How It Works**

1. While the tunnel is active, the proxy silently caches every successful `2xx` response (HTML, CSS, JSON, images) from your localhost in a memory-backed hash table.
2. When things go wrong — your localhost returns a `500 Internal Server Error`, or the proxy cannot establish a TCP connection at all (`dial error`) — the proxy **never** exposes the failure to the client.
3. It falls back to the cache and serves the last known healthy version of each requested endpoint.
4. The client continues browsing the demo seamlessly, completely unaware of the interruption, while you get precious minutes to fix and restart your backend.

The `X-Tunr-Freeze-Cache` response header is added to cached responses so you can tell, at a glance, when a response is being served from the snapshot layer.

---

## `--inject-widget`: Transparent Feedback UI & Remote Log Capture

```bash
tunr share --port 8080 --inject-widget
```

The traditional feedback loop — the client takes a screenshot, sends it over WhatsApp, and tries to describe the problem in words — is a developer's nightmare.

**How It Works**

1. Every time your server returns an HTML document (`text/html`), the proxy intercepts the response in memory ("response intercepting").
2. If the HTML body is GZip or Deflate compressed, it is decoded to a raw byte array on the fly.
3. The HTML content is scanned with a regex-backed parser to locate the closing `</body>` tag.
4. A two-part remote JavaScript bundle, hosted on Tunr's CDN, is injected just before the closing tag. The modified HTML is then re-compressed (GZip) to match the original encoding and sent to the client.
5. The client sees a subtle, floating feedback button. Clicking it opens an overlay where they can drop pins on the screen to mark exactly where the issue is and describe it in text (visual pinning).
6. In the background, the injected script silently captures all `window.onerror` events and unhandled promise rejections from the client's browser console.
7. All feedback and error data is `POST`-ed to Tunr's internal `/__tunr/feedback` route — a virtual endpoint intercepted by the proxy that is invisible to your localhost application. The data appears instantly in your CLI monitor as color-coded log entries (yellow for feedback, blue for captured errors).

This gives you pixel-precise bug reports and real-time JavaScript error telemetry without asking the client to install anything.

---

## `--auto-login`: Automatic Visitor Identity

```bash
tunr share --port 8080 --auto-login "Cookie: user_session_token=abcdef5551"
```

When demoing B2B or SaaS applications, the first obstacle is always the auth/login wall. Your client should not have to deal with "Forgot Password" flows — they should land directly on the main dashboard.

**How It Works**

1. The identity credential is provided as a CLI argument when the tunnel is created and stored in the tunnel's runtime state. Alternatively, a query parameter can be captured from the browser.
2. When a raw, unauthenticated HTTP request arrives from the client's browser (Chrome, Safari, etc.), Tunr's LocalProxy layer performs header manipulation: the specified identity token — whether a `Cookie` or `Authorization: Bearer` header — is injected into the request before it is forwarded to localhost.
3. Your local server sees the request as if it came from an already-authenticated admin user and grants full access. The client's signup and login process is completely bypassed.

No more sharing temporary passwords or walking clients through a registration flow during a demo.

---

## Combining Everything

These flags are designed to be composable. For the ultimate client demo experience, combine them all:

```bash
tunr share -p 3000 --demo --freeze --inject-widget --auto-login "Cookie: session=xyz"
```

This single command gives your client:
- **Full interactivity** without any risk to your data (`--demo`)
- **Zero-downtime resilience** even if your server crashes (`--freeze`)
- **Built-in bug reporting** with visual pinning and remote error capture (`--inject-widget`)
- **Instant access** with no login required (`--auto-login`)

Ship with confidence. Demo without fear.
