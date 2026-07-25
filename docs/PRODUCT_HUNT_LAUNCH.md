# tunr.sh — Product Hunt Launch Walkthrough

A step-by-step playbook to launch tunr.sh on Product Hunt with the best possible odds of hitting **#1 Product of the Day**. Designed for a solo founder; everything is concrete, sequenced, and copy-pasteable.

> **Estimated total prep work:** ~25 hours spread across 3 weeks.
> **Launch day:** 1 long day (target ~18 active hours).

---

## Table of contents

1. [Why Product Hunt for tunr](#1-why-product-hunt-for-tunr)
2. [Pick your launch slot (T-21 days)](#2-pick-your-launch-slot-t-21-days)
3. [Choose / line up a hunter (T-14 days)](#3-choose--line-up-a-hunter-t-14-days)
4. [Build the audience flywheel (T-14 → T-1)](#4-build-the-audience-flywheel-t-14--t-1)
5. [Polish the product surface (T-10 → T-2)](#5-polish-the-product-surface-t-10--t-2)
6. [Write all the copy (T-7)](#6-write-all-the-copy-t-7)
7. [Produce visual assets (T-7 → T-3)](#7-produce-visual-assets-t-7--t-3)
8. [Set up tracking + analytics (T-3)](#8-set-up-tracking--analytics-t-3)
9. [Pre-flight checklist (T-1)](#9-pre-flight-checklist-t-1)
10. [Launch day playbook (T-0)](#10-launch-day-playbook-t-0)
11. [Post-launch: 48 hours](#11-post-launch-48-hours)
12. [Post-launch: 30 days](#12-post-launch-30-days)
13. [Templates](#13-templates)

---

## 1. Why Product Hunt for tunr

tunr is a **developer tool with strong differentiation** (Vibecoder demo features, MCP/AI integration, open source CLI). That's exactly the profile that does well on PH:

- Self-evident value in <5 seconds (`tunr share -p 3000 → public URL`).
- Asynchronous to use (no signup wall to demo).
- Comparable to well-known products (ngrok, Pinggy, Cloudflare Tunnel) so reviewers know how to position it.
- Aimed at builders — who are the people who actually vote.

**Realistic goal:** Top 5 of the day → 1500-3000 site visits, 200-500 GitHub stars, 50-150 dashboard signups, 5-20 Pro conversions in week one.

**Stretch goal:** #1 Product of the Day → 4000-8000 site visits, 500+ stars, sustained organic traffic for 30+ days.

---

## 2. Pick your launch slot (T-21 days)

### Day of week

- **Avoid:** Mondays (too much competition from saved-up launches), Fridays (low engagement).
- **Best:** **Tuesday or Wednesday**. Thursday is fine.

### Time

- Product Hunt's day starts at **00:01 Pacific Time** (12:01 AM PT).
- **Submit at 12:01 AM PT** to maximize daylight on the leaderboard.
  - **Turkey (UTC+3):** that's **10:01 AM** local — easy.
  - Set a calendar event for **09:55 Turkey time** on launch day.

### Avoid clashing launches

- Check [Product Hunt Coming Soon](https://www.producthunt.com/coming-soon) the week before. If a YC darling or huge AI launch is scheduled the same day, slide a day.
- Avoid US holidays (Memorial Day, July 4, Thanksgiving week, Christmas/New Year week).

### Lock in the date

- Pick a date and put it everywhere — calendar, todo, ops doc.
- From this point on, work backwards.

---

## 3. Choose / line up a hunter (T-14 days)

### Do you need a hunter?

- A hunter with a strong follower base **multiplies your day-one reach** (their followers get push notified when they post).
- For tunr you don't strictly need one — you can self-launch. But a hunter helps for the social proof boost.

### Top-tier hunters who like dev tools

(Verify availability on their Twitter/PH profile — don't cold-DM if they say "not taking submissions.")

- Chris Messina (@chrismessina) — top hunter on PH, prolific.
- Kevin William David (@kwdinc) — open-source friendly.
- Pietro Schirano (@skirano) — newer but great with AI tools.
- Andreas Klinger (@andreasklinger) — dev-tool focused.

### How to ask

- **DM on Twitter** with a 3-line pitch + working demo link. Not the dashboard, the **landing**.
- Offer to do all the writing (description, first comment, hashtags). Hunters appreciate when you make their job zero work.
- Send the assets pack 48 hours before — see [section 7](#7-produce-visual-assets-t-7--t-3).

### Backup plan

If no hunter responds, **self-launch**. The hunter is a multiplier, not a requirement.

---

## 4. Build the audience flywheel (T-14 → T-1)

The day before launch is too late to build an audience. Start NOW.

### 4.1 — Twitter / X

- Pin a tweet with `tunr share -p 3000` GIF + landing link.
- Tweet **one tunr feature per day** until launch. Vibecoder Demo Mode, Freeze Mode, the Widget — each is its own thread.
- Engage in `#buildinpublic` and dev-tool conversations daily. Don't shill; build presence.

### 4.2 — LinkedIn

- 2 long-form posts: "Why I built tunr", "tunr v0.4.0 features deep dive".
- Each post drives to landing, not the PH page (PH page doesn't exist yet).

### 4.3 — Hacker News

- **Optional pre-launch:** "Show HN: tunr — Local → Public in <3s" 2-4 weeks before PH. Risk: if it dies on HN, you've burnt a card.
- **Safer:** Post on HN the **morning after** PH launch — see [section 11](#11-post-launch-48-hours).

### 4.4 — Reddit

- r/devops, r/selfhosted, r/golang, r/webdev — but each has strict no-promo rules. Lurk first; comment authentically on related threads. Mention tunr only if directly relevant.

### 4.5 — Indie Hackers, dev.to, Hashnode

- 1 article per week explaining a single tunr feature. Goal: each article ranks for a specific long-tail query (`ngrok alternative`, `vibecoder demo tunnel`, `MCP localhost tunnel`).

### 4.6 — Slack / Discord communities

- Make a list: 5-10 communities you're already in (not new ones; spam tax is real).
- Note which ones have a `#launches` or `#showcase` channel.

### 4.7 — Email list / waitlist

- If you have one: **don't tell them about PH yet**. Email them at 7am Turkey time on launch day — see [section 10](#10-launch-day-playbook-t-0).
- If you don't have one: add a "notify me on launch" form to the landing page **now**. Even 50 signups = 50 guaranteed upvotes.

---

## 5. Polish the product surface (T-10 → T-2)

PH visitors decide in 5 seconds. Make sure the landing + product are launch-grade.

### 5.1 — Landing

- [ ] Hero loads in <1.5s on mobile 4G (use [PageSpeed Insights](https://pagespeed.web.dev/)).
- [ ] First impression states the **one-liner** above the fold: *"Local → Public in <3 seconds"*.
- [ ] Install command is **single click to copy** (already implemented).
- [ ] Below-the-fold has terminal demo (already there) + Vibecoder superpowers (already there).
- [ ] Pricing visible — free tier prominent.
- [ ] Verified: **no `https://tunr.sh` link goes to 404**. Click every footer link.

### 5.2 — Onboarding

- [ ] `curl -sSL https://tunr.sh/install | sh` actually works end-to-end on a fresh mac and a fresh Ubuntu container.
- [ ] First-run `tunr share -p 3000` succeeds against a freshly started `python -m http.server`.
- [ ] `tunr doctor` returns all green on a fresh machine.

### 5.3 — Dashboard

- [ ] Magic link email arrives within 5 seconds. Test from a fresh inbox.
- [ ] After login, the dashboard shows the user's tunnels (apply migration `002_schema_align.sql` first).
- [ ] Settings / billing / domains pages all load without errors.
- [ ] Sign-out works.

### 5.4 — GitHub repo

- [ ] README's first paragraph nails the value prop. ✅ already.
- [ ] Star count is non-embarrassing. Get 5-10 friends to star **before** launch.
- [ ] Latest release is recent (within 4 weeks). v0.4.0 is good.
- [ ] License is clear (PolyForm Shield 1.0.0).
- [ ] Issues are pruned — close anything stale.

### 5.5 — Server

- [ ] Run `./update.sh` 24 hours before launch to verify deploy works.
- [ ] Set up monitoring: `tunr` API health, Caddy uptime, postgres disk. UptimeRobot for `/api/v1/health` is enough.
- [ ] Make sure the Hetzner instance can take 10x normal load. CPX21 (3 vCPU, 4GB) handles ~1000 concurrent tunnels — fine.

---

## 6. Write all the copy (T-7)

PH requires:

### 6.1 — Product name + tagline

| Field | Limit | Suggested |
|------:|-------|-----------|
| Name | 40 chars | `tunr` |
| Tagline | 60 chars | `Local → Public in 3 seconds. With superpowers.` |

Alternates:
- `The Pinggy alternative for vibecoders.`
- `Open-source localhost tunnel, with crash protection.`

### 6.2 — Description (260 char limit)

> Expose any localhost port to the internet in under 3 seconds — HTTPS, WebSocket, TCP, UDP, TLS. Free, open source, single Go binary. Plus exclusive vibecoder features: freeze mode, read-only demos, AI/MCP, request inspector & replay.

### 6.3 — First comment (the maker comment — most important)

Write this in the maker's voice. PH ranks comments by engagement; yours is the first.

> 👋 Hi everyone, I'm Ahmet — solo developer behind tunr.
>
> I built tunr after one too many "the demo just crashed" moments while showing dev work to clients. **Freeze Mode** keeps the last good HTML in memory and serves it if your local server dies mid-demo — your client never sees a 500.
>
> **What tunr does:**
> - Single Go binary. No signup needed for the free tier.
> - HTTPS, WebSocket (with HMR), TCP, UDP, and TLS tunnels.
> - Vibecoder mode: `--demo` blocks destructive writes, `--inject-widget` adds Marker.io-style feedback, `--auto-login` injects auth cookies.
> - **MCP integration** so Claude/Cursor can open and inspect tunnels for you.
> - Free forever for solo use. Pro plan is $5/mo for custom subdomains + reserved domains.
>
> **Built today's features because I needed them:**
> - The day a Postgres prod write would have happened mid-demo if `--demo` wasn't blocking POSTs.
> - The night a Next.js dev server crashed at 11pm before a 9am client demo — freeze mode saved it.
>
> **Try it:** `curl -sSL https://tunr.sh/install | sh && tunr share -p 3000`
>
> Open source on GitHub: https://github.com/ahmetvural79/tunr
>
> Would genuinely love feedback on what's missing — TCP/UDP just shipped in v0.4.0 thanks to community asks.

### 6.4 — Topics / categories

Pick **4-5**:
- Developer Tools
- Open Source
- Productivity
- API
- Web App
- Artificial Intelligence (justified via MCP)

### 6.5 — Pricing

- Select **Freemium**.
- Plan info: Free forever for HTTP/TCP tunnels with random subdomains; Pro $5/mo for custom subdomains, reserved domains, custom domains, unlimited concurrent tunnels.

---

## 7. Produce visual assets (T-7 → T-3)

PH gallery is what most visitors actually look at. Spend real time here.

### 7.1 — Thumbnail (240×240, square)

- The **most important** asset. It's the only thing visible in the daily list.
- Suggested: tunr purple gradient logo + the literal text `Local → Public in 3s` overlaid. White-on-purple, big readable type.
- Tools: Figma, Canva. Don't use an emoji — distinguishable typography beats cute.

### 7.2 — Gallery (up to 6 images, 1270×760)

Suggested sequence:

1. **Hero / pitch slide.** "tunr — Local → Public in 3 seconds" + Vibecoder superpowers tagline.
2. **Terminal demo.** Screenshot or screen-recorded GIF of `tunr share -p 3000` showing the public URL appear.
3. **Vibecoder demo features.** Three-up of Freeze / Demo / Widget side by side with one-line explainer.
4. **Inspector + Replay.** Browser screenshot of the local dashboard with requests captured.
5. **MCP integration.** Claude/Cursor chat showing "open me a tunnel" → working URL.
6. **Pricing / install.** Clean one-card pricing comparison + install command.

### 7.3 — Demo video (60-90s, 1920×1080)

- Optional but bumps engagement ~30%.
- Use Loom for fast turnaround. Voice + screen recording.
- Beat sheet:
  1. (0-10s) "Hi, I'm Ahmet. I built tunr because demos crash."
  2. (10-30s) Show `tunr share` → URL → curl it.
  3. (30-50s) Kill the local server. Show freeze cache still serving.
  4. (50-70s) Show `--inject-widget` adding the feedback overlay.
  5. (70-90s) "Free at tunr.sh. Star us on GitHub. Available right now."
- Upload to YouTube + embed on PH.

### 7.4 — Animated GIF for tweets

- 5-10 second loop of `tunr share -p 3000` working.
- ≤8MB. Use [Gifski](https://gif.ski/) for high-quality compression.

---

## 8. Set up tracking + analytics (T-3)

You need to know what works.

### 8.1 — UTM strategy

Every link out of PH should be tagged:

```
https://tunr.sh/?utm_source=producthunt&utm_medium=referral&utm_campaign=launch
https://tunr.sh/install.sh?utm_source=producthunt   ← detect installs
https://github.com/ahmetvural79/tunr?utm_source=producthunt
```

### 8.2 — Conversion funnels

Pick **one** analytics tool, ideally privacy-friendly and zero-config:

- **Plausible** — recommended, $9/mo, no cookie banner.
- **PostHog** — free up to 1M events, gives you funnels + session recordings.

Track:
- Landing visits → install button clicks → GitHub stars → dashboard signups → Pro conversions.

### 8.3 — Real-time dashboard

Build a single browser tab with:
- PH page (refresh every 30s)
- Twitter mentions search for `tunr.sh`
- Plausible/PostHog live dashboard
- GitHub repo stars page

You'll be staring at it for 18 hours. Get it right.

---

## 9. Pre-flight checklist (T-1)

The day before launch. Do this once, slowly.

### 9.1 — Product

- [ ] Run `./update.sh` and verify health endpoint returns 200.
- [ ] Apply `002_schema_align.sql` migration on the production DB.
- [ ] Fresh `curl -sSL https://tunr.sh/install | sh` in a clean Docker container — works.
- [ ] Fresh `pip install tunr` works.
- [ ] Fresh `npm install -g @tunr/cli` works.
- [ ] Dashboard login → see tunnels → log out → re-login.
- [ ] MCP integration works in Claude Desktop with `tunr mcp` configured.

### 9.2 — Copy + assets

- [ ] All gallery images uploaded as drafts to PH.
- [ ] First comment drafted in a notes app (no surprises typing it live).
- [ ] Twitter announcement thread drafted (5 tweets).
- [ ] LinkedIn announcement post drafted.
- [ ] Email blast to waitlist drafted in your sender (Resend / Mailchimp).
- [ ] HN post draft ready (for tomorrow morning).

### 9.3 — Logistics

- [ ] Alarm set for **08:00 Turkey time**. Eat. Hydrate.
- [ ] Calendar blocked: 09:00 – 23:00 Turkey time, no meetings.
- [ ] Phone notifications for Twitter, PH, GitHub all enabled.
- [ ] Tell family/co-workers: "I'll be heads-down for one day. Don't panic."
- [ ] All canned responses are in a notes file (see [section 13](#13-templates)).

---

## 10. Launch day playbook (T-0)

Time zone notes: **all times are Turkey time (UTC+3)** unless noted. PH's "day" is 00:01 PT → 23:59 PT, which is **10:01 AM → 09:59 AM Turkey time the next day**.

### 10:01 AM — Go live

- Hit "Publish" on PH (you scheduled this for 00:01 PT).
- Immediately verify the listing is correct: name, tagline, gallery, pricing, first comment.

### 10:05 AM — First comment

- Paste your prepared maker comment as the first reply.

### 10:10 AM — Personal network blast

In this order (because each gives a 5-minute boost — stagger them):

1. **Twitter announcement thread** (5 tweets, every 15 minutes).
2. **LinkedIn** post (1 long post).
3. **Email waitlist** — short, one CTA: "We're live on PH. Vote here: [link]"
4. **Direct DMs** to the 30 closest supporters. Personalized, not BCC.

### 10:30 AM — Community drops

- Post in 5-10 dev Slacks/Discords. Mention naturally: "Just launched tunr on PH — open source localhost tunnel with a couple of weird ideas. Would love thoughts."
- **Never** "vote for me" — PH bans for explicit vote solicitation.

### 11:00 AM – 14:00 PM — Engage every comment

- Reply within 5 minutes to every PH comment. Goal: be the first reply on every thread.
- Like every reply (PH counts engagement).
- Steer answers toward the differentiation: Freeze Mode, MCP, open source.

### 14:00 – 18:00 PM — Second wind

- Twitter check-in: "We just crossed 100 upvotes on @ProductHunt. Top X of the day."
- Reply to any indirect mentions on Twitter.
- DM journalists who cover dev tools: TechCrunch's @sarahintampa, Hacker Noon. Don't beg; just inform.

### 18:00 – 23:00 PM — US prime hours

- This is the **biggest traffic window**. US devs wake up and check PH over morning coffee.
- Be online. Reply to every comment within 10 minutes.
- Quote-tweet anyone who shares.

### 23:00 – 02:00 PM — Wind down

- Stop replying to new comments after 02:00. You need sleep.
- Last tweet of the day: "Day 1 wrapping up — thank you. 🙏 [stats]"

### 03:00 PT (= 13:00 PM next day in Turkey) — Day-end snapshot

- Final rank, upvote count, comment count.
- Screenshot the leaderboard.

---

## 11. Post-launch: 48 hours

### Day +1 morning

- **Hacker News.** Title format: `Show HN: tunr – Local → Public in 3s (open source ngrok alternative)`.
  - Post between 7-9am PT (= 17:00–19:00 Turkey).
  - **Do not link to PH.** Link to landing.
- **Reddit:** `r/selfhosted`, `r/golang`, `r/webdev`. Use a "Show" or "I built" flair.
- Thank-you tweet pinned: "We finished [#X] on Product Hunt yesterday. Thank you. Here's what tunr can do: [thread]".

### Day +2

- Write a launch retrospective post (Indie Hackers + Twitter):
  - Final rank, traffic, signups.
  - What worked, what didn't.
  - This becomes evergreen content + builds trust for next launch.

---

## 12. Post-launch: 30 days

### Week +1
- Convert PH visitors to dashboard signups. Email anyone who left their address: 2-touch sequence (welcome → "did you try freeze mode?")
- Fix every legitimate bug surfaced on launch day. Visible responsiveness is half the trust-building.

### Week +2
- Publish "What I learned launching tunr on Product Hunt" longform. Title bait: "tunr hit #X on Product Hunt — here's the unfiltered numbers."

### Week +3
- First **paid acquisition test**: Twitter ads to dev audience, $200 budget, point at the "freeze mode" tweet thread.
- Reach out to YouTubers who review dev tools (Theo, Fireship, ThePrimeagen). Send working tunr setup + USD20 Amazon gift card for "thanks for trying it."

### Week +4
- Plan the next launch milestone: v0.5.0, a major partnership, a new SDK. The goal is to have **another launch** in 3 months — PH lets you re-launch with a major release.

---

## 13. Templates

### 13.1 — Hunter DM (Twitter)

> Hey [name], huge fan of your launches.
>
> I shipped tunr (https://tunr.sh) — a single Go binary that exposes localhost in <3s with crash-protection and AI/MCP support. It's the open-source ngrok alternative I've been missing.
>
> I'm launching on Product Hunt **[date]**. Would you be open to hunting it? I'll do all the writing — first comment, hashtags, copy. Zero work on your end.
>
> Happy to do a 10-min demo on Loom if helpful.
>
> — Ahmet

### 13.2 — Maker first comment

(See [section 6.3](#63--first-comment-the-maker-comment--most-important).)

### 13.3 — Twitter launch thread

```
1/  We're live on @ProductHunt today.

tunr — exposes localhost to the internet in 3 seconds with built-in crash protection, request replay, and AI integration.

It's open source. It's a single Go binary. It's free.

→ [PH link]

2/  Why tunr exists:

I do a lot of client demos. They crash. The freeze mode in tunr caches your last 2xx response — if your dev server dies, the client just keeps seeing the working version.

[GIF of freeze mode]

3/  Why "vibecoder"?

`--demo` blocks POST/PUT/DELETE so your "delete order" button doesn't actually delete the order.
`--inject-widget` adds a Marker.io-style feedback overlay without touching your code.
`--auto-login` injects auth cookies so the client lands at the dashboard.

[Screenshot]

4/  Why MCP?

Tell Claude or Cursor "open me a tunnel on port 3000" and they actually do it — through the Model Context Protocol server `tunr mcp`.

The AI also reads/replays requests captured by the inspector.

5/  Free forever for solo. Pro is $5/mo for custom subdomains.

If this is useful, an upvote on PH would mean a lot today: [PH link]

If it's not, tell me what's missing — I'll build it. → @vural_met
```

### 13.4 — LinkedIn launch post

```
🚀 Today I launched tunr on Product Hunt.

tunr exposes localhost to the internet in under 3 seconds. It's the open-source localhost tunnel I built after one too many demo crashes.

Three differentiators:

1. Freeze mode — if your local server dies mid-demo, tunr keeps serving the last good response. Your client never sees a 500.

2. AI/MCP integration — Claude and Cursor can open tunnels for you. "Open me a tunnel on port 3000" → done.

3. Open source, single Go binary. No signup needed.

If you do localhost demos, webhook testing, or AI app development, give it a try:

→ https://tunr.sh
→ Product Hunt: [link]

Built in Go. Free forever for solo use. Pro is $5/mo.
```

### 13.5 — Show HN

```
Title: Show HN: tunr – Local → Public in 3s (open source ngrok alternative)

URL: https://tunr.sh

Body:
Hi HN — I built tunr because I do a lot of client demos and they crash.

It's a single Go binary that exposes localhost over HTTPS, WebSocket, TCP, UDP, or TLS. Free, open source (PolyForm Shield), no signup for the free tier.

The unique bits:

- `--freeze` keeps the last successful 2xx response in memory and serves it if your local server crashes. Your demo client never sees the error.

- `--demo` blocks POST/PUT/DELETE at the proxy layer so destructive actions in a demo can't actually delete data.

- `tunr mcp` is an MCP server — Claude Desktop / Cursor / Windsurf can open and inspect tunnels for you.

- Built-in HTTP inspector with replay (like ngrok's web UI, but local).

I'd love feedback, particularly on:
- The Vibecoder demo features (freeze / demo / inject-widget) — are they too niche or genuinely useful?
- The MCP integration — anyone using MCP with dev tooling?
- Anything Pinggy / ngrok / Cloudflare Tunnel does better that I should copy.

Repo: https://github.com/ahmetvural79/tunr

Thanks!
```

### 13.6 — Canned replies for PH comments

> **"How is this different from ngrok?"**
> Two things: 1) tunr is open source, no bandwidth cap, no $10/mo on the free tier. 2) Vibecoder features ngrok doesn't have — freeze mode keeps demos alive if your server crashes, demo mode blocks destructive writes, MCP integration lets Claude/Cursor open tunnels for you.

> **"How is this different from Cloudflare Tunnel?"**
> CF Tunnel requires a Cloudflare account, owning a domain, and configuring DNS. tunr is `brew install tunr && tunr share -p 3000` — done, ~10 seconds.

> **"Is this safe? You can see my traffic?"**
> The default relay terminates TLS at our edge, so technically yes. If you don't want that, use `tunr tls --port 8443` (E2E encrypted, our relay can't read the payload) or self-host the relay with the Docker Compose in the repo.

> **"What's the catch on the free tier?"**
> Tunnel duration capped at 2h, 1 concurrent tunnel, random subdomains only. That's it. No bandwidth cap.

---

## Final note

Most launches over-prepare the assets and under-prepare the engagement. **Comments and replies are what move the leaderboard** in the second half of the day. Block your calendar; reply to every single comment.

Good luck. 🚀
