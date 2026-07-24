# YC Fall 2026 Application — Answer Drafts for tunr
*Cevaplar İngilizce (forma yapıştırılacak metin), her sorunun altındaki italik notlar Türkçe koçluk notudur — forma girmeyin.*
*✎ [KÖŞELİ PARANTEZ] = gerçek veriyle doldurun. Sayı uyduramayız; YC mülakatı bunların üstüne kurulur.*

**Genel ilkeler:** Kısa yaz (çoğu cevap 1–4 cümle). Jargon yok. Traction küçükse küçük söyle — netlik, dürüstlük ve hız sinyali abartıdan bin kat değerlidir. Son gönderim: **27 Temmuz, 20:00 PT.**

---

## COMPANY

**Company name:** tunr

**Company URL:** https://tunr.sh

**Describe what your company does in 50 characters or less.**

> The cloud where agent-built software lives

*(42 karakter. Alternatifler: "Deploy and share small software like a Google Doc" — 49; "Ship agent-built apps in one command" — 36. İlkini öneririm: kategori tanımlıyor.)*

**What is your company going to make? Please describe your product and what it does or will do.**

> tunr is a cloud for small software — the personal and internal tools people now build with coding agents. Today tunr is an open-source tunneling CLI ("localhost live in 3 seconds"). We're extending the same relay into a full runtime: `tunr deploy` — or one MCP call from Claude Code or Cursor — builds and runs the app on our infrastructure, where it sleeps when idle and wakes on request. Next, apps become shareable like a Google Doc: login, viewer/editor roles, and comments enforced at our proxy, so the app itself never contains auth code. Long term: the default place agent-built software lives.

*(4 cümle + vizyon cümlesi — tam YC formatı. "MCP call from Claude Code" somutluğu önemli; partner bunu her gün yaşıyor.)*

**If you have a demo, what's the url?**

> Product: https://tunr.sh · Code: https://github.com/ahmetvural79/tunr · 60-sec deploy demo: ✎ [YouTube unlisted URL — göndermeden önce çek]

**Where do you live now, and where would the company be based after YC?**

> ✎ [Gaziantep], Turkey. During the batch: San Francisco, in person. After YC: ✎ [San Francisco / SF + remote — gerçek niyetinizi yazın].

*(YC F26 tamamen yüz yüze. Vize planınızı burada anlatmayın — sorulursa mülakatta dürüstçe söylersiniz; YC uluslararası kurucularla deneyimli.)*

**Explain your decision regarding location.**

> Our users and the agent-tooling ecosystem we integrate with (Claude Code, Cursor, MCP registries) are US-centric; being in SF during and after the batch shortens every loop. We build a global product in English from day one.

---

## FOUNDERS

**Please enter the url of a 1 minute unlisted YouTube video introducing the founder(s).**

> ✎ [URL]

*Önerilen 60 saniyelik akış (ezberlemeyin, doğal konuşun):*
*0–20 sn — kim olduğunuz: "I'm Ahmet, this is ✎[isim]. We're both engineers; we built and operate tunr, an open-source tunneling tool written in Go — CLI, relay, SDKs, all of it, just the two of us."*
*20–45 sn — içgörü: "Watching our users, we realized the features they loved had nothing to do with tunneling — read-only demo mode, crash-proof freeze mode, a feedback widget. Their real job was sharing software, not moving packets. So we're turning tunr into the cloud where agent-built apps live."*
*45–60 sn — kanıt + kapanış: "This week we shipped `tunr deploy`: one command — or one MCP call from Claude Code — and your app runs on our infra after you close your laptop. We ship fast, and we're just getting started."*

**Who writes code, or does other technical work on your product? Was any of it done by a non-founder?**

> Both founders. All code was written by us — ✎ [doğruysa:] no non-founder work.

**How long have the founders known one another and how did you meet? Have any of the founders not met in person?**

> ✎ [Gerçek hikâye, 2–3 cümle. Yüz yüze tanıştıysanız belirtin.]

**Please tell us about an interesting project, preferably outside of class or work, that two or more of you created together. Include urls if possible.**

> ✎ [tunr öncesi ortak bir proje varsa yazın + URL. Yoksa: tunr'ın kendisini anlatın — "We designed and built the entire tunr stack together: Go relay, CLI, Python/Node SDKs, MCP server, self-hosting stack — 5 releases in ✎[X] months." Bu da geçerli bir cevap.]

**Are you looking for a cofounder?**

> No.

---

## PROGRESS

**How far along are you?**

> Shipped and in production: open-source CLI + relay (Go, v0.4.1) — HTTPS/WebSocket, TCP, UDP and TLS tunnels; multi-region relay (Amsterdam, Seattle, Singapore); request inspector; Python/Node SDKs; an MCP server so agents can drive tunnels; a self-hosting stack. Usage: ✎ [gerçek sayılar: install/hafta, aktif tünel/hafta, GitHub yıldızı, Pro abone]. This week we shipped `tunr deploy` v0: agent-built apps deploy to our infra with one command or one MCP call, sleep when idle, wake on request — demo: ✎ [URL].

*(Sayılar küçükse küçük yazın ve yanına bir kalite sinyali koyun: "small but weekly-active" gibi. Sayı YOKSA o metriği hiç yazmayın; boş övgü cümlesiyle doldurmayın.)*

**How long have each of you been working on this? How much of that has been full-time? Please explain.**

> ✎ [Örn: "Ahmet: X months, full-time since Y. ✎[İsim]: X months, nights/weekends until Z, full-time since…"] — dürüst yazın; part-time ise sebebiyle birlikte.

**What tech stack are you using, or planning to use, to build this product? Are you using AI coding tools?**

> Go (CLI, relay, control plane), Caddy, Postgres, Docker + gVisor on our own hardware for app sandboxes, Nixpacks for buildpacks, SQLite-per-app planned for the data layer. We build with Claude Code and Cursor daily; roughly ✎ [%X] of recent code is AI-written and human-reviewed. We are our own target user — tunr's MCP server exists because *we* wanted our agents to ship for us.

**Are people using your product?**

> Yes — ✎ [1 cümle gerçek kullanım: kim, ne için. Örn: "freelancers demoing client work and developers testing webhooks; N weekly active tunnels."]

**Do you have revenue?**

> ✎ [Varsa: "Yes — $X MRR from N Pro subscribers ($5/mo)." Yoksa: "Not yet. A $5/mo Pro plan is live; monetization shifts to the deploy/share layer we're building."]

**If you are applying with the same idea as a previous batch, did anything change?**

> ✎ [İlk başvuruysa: "First application." Değilse değişeni yazın.]

**If you have already participated or committed to participate in an incubator, "accelerator" or "pre-accelerator" program, please tell us about it.**

> ✎ [Yoksa: "No."]

---

## IDEA

**Why did you pick this idea to work on? Do you have domain expertise in this area? How do you know people need what you're making?**

> We built tunr for freelancers demoing AI-built apps to clients. Watching real usage, we noticed the features people loved had nothing to do with tunneling: read-only demo mode, freeze mode (serve the last good response when the dev server crashes mid-demo), an injected feedback widget, auto-login for viewers. Their real job was *sharing software*, not moving packets — we'd been building the primitives of a sharing layer without naming it. The missing piece is the body: tunneled apps die when the laptop closes. ✎ [1 kanıt cümlesi: kullanıcı talebi/alıntı — örn. "Users kept asking 'can it stay up after I shut my laptop?'" — GERÇEKSE yazın.] Domain expertise: we designed, wrote and operate the entire relay infrastructure in production ourselves.

**Who are your competitors? What do you understand about your business that they don't?**

> Val Town (JS/TS-only, lives in a browser editor), Replit/Lovable/Bolt (closed loops: their agent + their hosting), Vercel/Netlify (big-software ergonomics; per-seat pricing breaks at 50 tiny apps), Fly.io/Railway (compute with no sharing or identity layer), Cloudflare Workers for Platforms (infra for platform builders, not end users). What we understand: (1) the largest cohort of serious AI-assisted builders works *locally* with Claude Code and Cursor and has no native publish button — the agent-agnostic seat is empty; (2) for small software, sharing is the product and hosting is the feature — auth, roles and comments belong in the proxy, not in AI-generated app code; (3) small-software economics require wake-on-request, and a relay that already fronts every request — which we operate today — is half of that machine.

**How do or will you make money? How much could you make?**

> Free tunnels and sleeping apps are the funnel. Pro at ~$15/mo (custom domains, always-on apps, more apps); Team per-seat (SSO, org app directory, environment policies, private access to internal APIs); usage beyond quota; later a platform API for AI app-builder products. The unit that prices our market is "apps created by agents," which is compounding far faster than apps created by hand — if agent-built internal tools become as common as spreadsheets, the ceiling is a Vercel-scale infrastructure business.

**Which category best applies to your company?**

> Developer Tools *(alternatif: B2B / Infrastructure)*

**If you had any other ideas you considered applying with, please list them.**

> ✎ [Varsa 1–2 tanesini 50 karakter + 1 cümne formatında dürüstçe; yoksa: "None — we're committed to this."]

---

## EQUITY

**Have you formed ANY legal entity yet? / Please list all legal entities.**

> ✎ [Varsa listeleyin (ülke + tür). Yoksa: "No legal entity yet; we'll form a Delaware C-Corp upon acceptance." — YC için tamamen normal bir cevap.]

**Equity breakdown among founders.**

> ✎ [Örn: "Ahmet Vural (CEO) X% — ✎[İsim] (CTO) Y%." Aranızda ŞİMDİ netleştirin; mülakatta sorulur.]

**Have you taken any investment yet? / Spend per month, cash in bank, runway.**

> ✎ [Dürüst rakamlar. Yatırım yoksa: "No outside investment; bootstrapped." Aylık gider ~$X, bankada $Y, runway Z ay.]

---

## CURIOUS

**Please tell us something surprising or amusing that one of you has discovered.**

> ✎ [GERÇEK bir hikâyeyle değiştirin. Şablon önerisi — doğruysa kullanın:] The most-loved feature in our tunneling tool has nothing to do with tunneling. Freeze mode serves the last good response when your dev server crashes mid-demo — we built it after watching a user's demo die in front of their client. It turns out demos crash constantly; nobody admits it, and everybody paid us to hide it.

**What convinced you to apply to Y Combinator?**

> Pete Koomen's "A Cloud for Small Software" RFS describes, almost line by line, what our users have been pushing us toward — we were building it without naming it. ✎ ["[İsim] encouraged us to apply" / "No one encouraged us to apply."] ✎ ["We have not been to a YC event." / etkinlik adı]

**How did you hear about Y Combinator?**

> ✎ [Dürüst kaynak: "Hacker News and Paul Graham's essays" vb.]

**Is there anything else we should know about your company?**

> Two engineers in Turkey shipped a production tunneling stack — CLI, multi-region relay, SDKs, MCP server — in ✎[X] months, on ✎[~$Y/mo] of infrastructure. Capital efficiency and shipping speed are the company culture, not a phase.

---

## Göndermeden Önce Kontrol Listesi

1. Demo videosu çekildi ve unlisted YouTube linki forma girildi (founder videosu AYRI bir video).
2. Tüm ✎ alanları gerçek veriyle dolduruldu; kalan tek bir köşeli parantez yok.
3. Her cevap yüksek sesle okundu; 5 cümleyi aşanlar kısaltıldı.
4. İngilizcesi güçlü birine 30 dakikalık son okuma yaptırıldı.
5. Kurucu ortakla equity/tam-zaman cevapları sözlü teyit edildi (mülakat tutarlılığı).
6. 27 Temmuz 20:00 PT = 28 Temmuz 06:00 (TR saati) — son güne bırakmayın, 26'sında gönderin.
