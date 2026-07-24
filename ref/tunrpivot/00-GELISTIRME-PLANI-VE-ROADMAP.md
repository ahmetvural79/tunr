# tunr Pivot — Geliştirme Planı ve Roadmap
## "Küçük Yazılım Bulutu" — Ajan-Yerel, Vibecoder-Güçlendirilmiş

*Hazırlanma: 2026-07-23. Girdi dokümanları: `ref/tunrpivot/tunr-kucuk-yazilim-bulutu-pivot-raporu.md`, `ref/tunrpivot/tunr-deploy-v0-teknik-tasarim.md`, ve kod iskeletleri (`cloud_upstream.go`, `driver.go`, `main.go`/buildd). Bu doküman, o vizyonu **mevcut kod tabanının gerçek dosya/paketlerine bağlayan** uygulanabilir mühendislik planıdır.*

---

## 0. Baseline ve çalışma kuralı

- **`main` = stabil v0.4.1 baseline** (commit `e43fce2`, `origin/main`'e push edilmiş — güvende). Pivot çalışması ayrı bir `pivot/cloud` branch'inde yapılacak; `main` dokunulmaz kalır ("bu halini ana branchda tut" karşılığı).
- Bu doküman `ref/` altında (git-tracked değil) tutulur; ürün dokümantasyonuna (docs/) pivot olgunlaştıkça taşınır.
- Sürüm hedefi: pivot serisi **v0.5.x → v0.6.x** olarak numaralandırılır (tünel = v0.4.x sonlanır, "deploy" = v0.5.0 minör atlama).

---

## 1. Tek cümlelik hedef ve üç katman

**Eski:** "Expose your local server in 3 seconds."
**Yeni:** *"localhost'ta doğan yazılımın yaşamaya devam ettiği yer — ajanın kurar, tunr çalıştırır ve Google Dokümanı gibi paylaştırır."*

Tünel silinmez; **huninin tepesine** konur ve altına üç katman eklenir:

1. **Kalıcı çalıştırma** (compute + veri) — app, laptop kapansa da yaşar; boştayken uyur, istekte uyanır.
2. **Kimlik & paylaşım** (Gate: auth, roller, davet) — auth uygulamanın *içinde* değil *önünde*.
3. **Ajan-yerel kontrol düzlemi** (MCP + API) — **kullanıcının 2. ana isteği**: dashboard'da yapılabilen her şey MCP aracı olarak da yapılabilir; ajan deploy/db/secret/share/rollback'i kendisi çağırır.

Mevcut dört "vibecoder süper gücü" (freeze / demo / inject-widget / auto-login) **atılmaz, evrilir** — her biri bu üç katmanın ilkel hâli (bkz. §3.2).

---

## 2. Mevcut Sistem Envanteri (keşif sonucu)

### 2.1 İki Go modülü + frontend + paylaşılan Postgres

| Katman | Konum | Bugün ne yapıyor | Pivotta rolü |
|---|---|---|---|
| **CLI** | `./` modülü `github.com/ahmetvural79/tunr`, `cmd/tunr/` + `internal/` | Tünel aç/yönet, MCP (stdio), inspector, daemon, proxy middleware zinciri | `deploy`/`apps` komut ailesi + genişletilmiş MCP eklenir |
| **Relay** | `./relay/` modülü, `relay/cmd/server/main.go` | subdomain → canlı WS tünel oturumu; JWT+magic auth; pgx Postgres; rate limiter | subdomain → **CloudUpstream** (reverse proxy + wake-on-request) eklenir; kontrol düzlemi API'si buraya konur |
| **Dashboard** | `landing/app/` (Next.js 16, App Router, React 19, TS, CSS Modules) | Firebase OAuth + magic-link, JWT session, **`pg` ile paylaşılan Postgres**, Paddle billing | "Tunnels" merkezli → **"Apps" merkezli** dashboard'a evrilir |
| **Landing (statik)** | `landing/*.html` + `app.js` + `style.css` | Marketing, karşılaştırma sayfaları | Anlatı yeniden çerçevelenir; "deploy + share" hero'su |
| **Local inspector** | `internal/webui/` (Go embed, WS) | `tunr open` — localhost istekleri canlı log | Bulut app durumları eklenebilir (opsiyonel) |
| **`web/`** | boş `dist/` | Kullanılmıyor | Silinebilir |

**Kritik mimari gerçek:** Dashboard (Next.js) ve relay (Go) **aynı PostgreSQL veritabanını** paylaşıyor (Firebase yalnız OAuth için). Yani yeni `apps` / `deployments` / `routes` tabloları hem relay (Go/pgx) hem dashboard (Next.js/pg) tarafından **doğrudan** okunabilir — pivot için temiz zemin.

### 2.2 Relay dispatch — CloudUpstream'in gireceği tek nokta

`relay/internal/relay/proxy.go` → `Proxy.ServeHTTP()`:

```go
subdomain := extractSubdomain(host, p.domain)
entry, ok := p.registry.Lookup(subdomain)     // in-memory tünel kaydı
if !ok {
    writeTunnelNotFound(w, subdomain)          // ← CloudUpstream lookup TAM BURAYA girer
    return
}
```

Entegrasyon: `!ok` dalında, 404'ten önce `routes.LookupCloud(subdomain)` → varsa `CloudUpstream.ServeHTTP`. `hybrid` route (tünel varsa tünel, yoksa cloud) `detach`'in kalbi olur.

### 2.3 Mevcut DB (relay + dashboard ortak)

`users`, `magic_tokens`, `tunnels`, `subscriptions`, `feedback`, `audit_log`, `user_domains`, (+ dashboard: `promo_codes`). Eklenecek: `apps`, `deployments`, `routes` + `LISTEN/NOTIFY routes_changed`.

### 2.4 Mevcut MCP (stdio, `internal/mcp/server.go`)

5 araç: `tunr_share`, `tunr_status`, `tunr_inspect`, `tunr_replay`, `tunr_stop`. JSON-RPC 2.0, `handleToolCall` dispatch. Yeni araçlar aynı desenle eklenir.

### 2.5 Proxy middleware zinciri (`internal/proxy/`)

`demo.go`, `freeze.go`, `inject.go`, `auth_middleware.go`, `auth_token_middleware.go`, `ip_whitelist.go`, `metrics.go`. **Freeze yalnızca CLI tarafında** — relay'de yok; CloudUpstream için relay'e taşınacak (bkz. `cloud_upstream.go`'daki `FreezeCache` arayüzü).

### 2.6 Prod altyapı

Sunucu `167.233.102.96` (`tunr-prod`), `/opt/tunr` altında docker-compose: **postgres + relay + Next.js dashboard + Caddy** (TLS + `*.tunr.sh` routing). Deploy: `./update.sh` (rsync + migration + rebuild). Ayrıca `relay/fly.toml` mevcut (Fly opsiyonu).

---

## 3. Hedef Mimari

### 3.1 Kuş bakışı

```
  Kullanıcı/Ajan tarayıcısı
          │  https://{app}.tunr.sh
          ▼
  ┌───────────────────────────────────────────┐
  │  tunr Edge  (relay'in evrimi, Caddy arkası) │
  │  • TLS + subdomain routing                  │
  │  • RouteStore: tunnel | cloud | hybrid      │
  │  • CloudUpstream: probe→wake→reverse-proxy  │
  │  • Freeze failover (redeploy/uyanma boşluğu)│
  │  • [Faz2] Gate: OAuth, roller, X-Tunr-User  │
  │  • [Faz2] widget/yorum enjeksiyonu          │
  └───┬─────────────────────────────┬───────────┘
      │ (canlı tünel)               │ (kalıcı app)
      ▼                             ▼
 ┌──────────┐              ┌────────────────────────────┐
 │ Laptop   │              │ Runner (Driver arayüzü)     │
 │ tunr CLI │              │ • DockerDriver (own-server, │
 │ dev modu │              │   gVisor, scale-to-zero)    │
 └──────────┘              │ • FlyDriver (stub/escape)   │
                           │ • tunr-shim (edge HMAC+log) │
                           │ • [Faz1] SQLite + Litestream│
                           └───────────┬────────────────┘
                                       │
                    ┌──────────────────▼─────────────────┐
                    │ Kontrol Düzlemi (v0: relay binary)  │
                    │ • REST /v1/apps /v1/deployments SSE │
                    │ • buildd (Nixpacks build agent)     │
                    │ • apps/deployments/routes (Postgres)│
                    │ • sealed env, idle sweeper          │
                    └──────────────┬──────────────────────┘
                                   │  (tek doğruluk kaynağı)
              ┌────────────────────┼────────────────────┐
              ▼                    ▼                     ▼
        tunr CLI            MCP araçları           Next.js Dashboard
     (insan yüzü)        (AJAN yüzü — birincil)     (insan yüzü)
```

### 3.2 Vibecoder DNA → evrim haritası (koru + geliştir)

| Bugünkü özellik | İlkel hâli | Pivottaki evrimi | Nerede |
|---|---|---|---|
| **Freeze Mode** | Snapshot/kalıcılık | CloudUpstream freeze failover → app snapshot/rollback | relay `CloudUpstream`, Faz0→1 |
| **Demo Mode** (yazma kes) | Viewer rolü | Gate: `viewer` = güvenli metotlar, `editor` = tümü | relay Gate, Faz2 |
| **Auto-Login** (cookie enjekte) | Kimlik enjeksiyonu | Gate: gerçek OAuth → imzalı `X-Tunr-User` başlığı | relay Gate, Faz2 |
| **Inject-Widget** | Google Doc yorumları | Canlı app üstünde yorum/pin → `get_feedback` MCP → ajan düzeltme döngüsü | relay + widget, Faz2 |
| **Parola/Bearer/IP allowlist** | Erişim ilkelleri | Paylaşım diyaloğu ("linki olan / şu e-postalar / @şirket") altyapısı | Faz2 |
| **Relay** | Kontrol düzlemi | Wake aktivatörü + auth kapısı + log + widget | Faz0+ |
| **`tunr mcp`** | Ajan-yerel API | Tam MCP kontrol düzlemi (deploy/db/secret/share/...) | **her fazda paralel** |
| **docker-compose self-host** | Kurumsal ortam | BYOC runner | Faz3 |

**İlke:** Vibecoder feature bayrakları (`--demo`, `--freeze`, `--inject-widget`, `--auto-login`) tünel modunda **aynen korunur** (geriye uyumluluk); bulut app'lerde aynı davranış Gate/rol/yorum katmanı olarak *sunucu tarafında* yeniden doğar.

---

## 4. Kritik Mimari Kararlar

| # | Karar | Seçenekler | Öneri | Gerekçe |
|---|---|---|---|---|
| **K1** | **Runner (v0)** | (a) Own-server Docker+gVisor · (b) Fly Machines · (c) Driver soyutlaması + ikisi | ✅ **KİLİTLENDİ: (a) Own-server Docker+gVisor** + `Driver` arayüzü | Mevcut tek sunucu (`167.233.102.96`) zaten docker-compose; `driver.go` + `cloud_upstream.go` iskeletleri own-server için yazılmış; vendor bağımlılığı/maliyeti yok. Fly `FlyDriver` stub olarak durur (çok bölge gerekince). |
| **K2** | Kontrol düzlemi yerleşimi | Relay binary'sine ekle · Ayrı `relay/cmd/cp` binary | **Relay binary'sine `/v1/*`** (v0), sonra gerekirse ayır | login/JWT altyapısı zaten relay'de; ayrı servis 3 kat operasyon. Paket sınırı `relay/internal/controlplane/` ile temiz tutulur. |
| **K3** | Veri katmanı | app başına SQLite + Litestream · app başına Postgres | **SQLite-per-app** (Faz1), Postgres opsiyon (Faz4) | "verisiyle fork" büyüsü + boşta-app ekonomisi SQLite ile mümkün; Postgres cluster küçük yazılım için israf. |
| **K4** | Auth/kimlik | Dashboard Firebase + relay JWT ayrı kalsın · Edge OAuth'ta birleştir | v0: ayrı; **Faz2 Gate = edge OAuth** (Google/GitHub) tek noktada | Gate zaten OAuth'u edge'e koyuyor; dashboard login'i JWT session ile devam, app-giriş'i Gate'ten. |
| **K5** | Monorepo hijyeni | `landing/app/` kendi iç `.git`'i var (iç içe repo) | ✅ **KİLİTLENDİ: ana repoya dahil et** (iç `.git`'i kaldır) | Pivot dashboard'u ağır değiştirecek; tek CI/deploy/geçmiş en temizi. Faz 0 ilk adımlarından biri. |
| **K6** | MCP konumu | Sadece stdio (bugün) · + remote/HTTP MCP | v0: stdio; **Faz2: relay üstünde HTTP MCP** de | Ajanın CI/bulutta da kontrol düzlemine erişmesi için ("her işi agent yapsın"). |
| **K7** | Lisans | Hepsi PolyForm Shield · CLI/SDK açık + control-plane kapalı | **Open-core**: CLI+SDK Apache-2/MIT, control-plane FSL/kapalı | Dağıtımı maksimize et, rakip-tünel korumasını control-plane'e taşı (Sentry modeli). ⚠️ opsiyonel/sonraki. |

---

### 4.1 Runner derinleştirmesi — own-server (kilitli karar) neden ve nasıl

Girdi teknik-tasarım MD'si "donanım yok" varsayımıyla Fly'ı seçmişti; **elde sunucu olması dengeyi in-house lehine çeviriyor.** Fly'ın tasarımda satın aldığı üç şeyin her birinin doğrudan in-house karşılığı var:

**1) İzolasyon (Fly'da Firecracker microVM) — geçişin tek gerçek bedeli.**
Yabancı/AI-üretimi kodu **çıplak Docker'la koşturma.** `gVisor (runsc)` Docker runtime olarak takılır — ~yarım günlük kurulum, syscall saldırı yüzeyini ciddi daraltır. Üstüne: `--security-opt no-new-privileges`, `--read-only` rootfs (+ `tmpfs /tmp`), bellek/CPU/pid limitleri, ve `com.docker.network.bridge.enable_icc=false` ile izole bridge ağı. **Hepsi `driver.go` iskeletinde hazır.** İdeal: iş yüklerini relay/Postgres'ten **ayrı bir kutuya (veya en az ayrı VM'e)** koymak; tek kutudaysak bu **"bilinen v0 riski"** olarak dürüstçe kayda geçer (landing'de aksi iddia edilmez).

**2) Wake-on-request (Fly'da Flycast/suspend bedavaydı) — kendi kutumuzda relay yapar.**
Tüm trafik zaten relay'den geçiyor. Yaşam döngüsü:
- `running` → **5 dk boşta** → `docker pause` (cgroup freezer; RAM'de kalır, **dönüş anlık**)
- → **2 saat boşta** → `docker stop` (RAM boşalır; **dönüş 2–5 sn**, relay'deki `wake_timeout` bunu zaten tolere ediyor)

**RAM matematiği lehimize:** ~100–150 MB'lık uyuyan app'lerle 64 GB kutuda **yüzlerce "warm" app** tutulur → "boşta app ≈ bedava" ekonomisinin in-house kanıtı. Idle sweeper (§5 Faz0) `CloudUpstream.LastSeen()` okuyup `Sleep`/`Stop` çağırır.

**3) Operasyon yükü — gerçek maliyet, ama iki karşı kazanç var.**
Yedek, disk, kernel güncellemesi artık bizde (gerçek maliyet). Ama:
- **Build hattı yarıya iner:** registry push **tamamen kalkar** — image zaten runner'ın kullandığı daemon'da lokal. Teknik dokümandaki **§14 doğrulama listesinin çoğu çöpe gider** (Flycast IP tahsisi, registry token, suspend sınırları... hepsi Fly'a özgüydü). `buildd` `-push=false` ile koşar.
- **Stratejik hizalanma:** rapor zaten Faz 2'de "kendi runner filomuz" diyordu → bugünden o yola girmiş oluruz. Üstelik `iptables` ile **default-deny egress in-house'da Fly'dakinden çok daha uygulanabilir** → İzin Manifesti vizyonuna erken kapı.

**Karar çerçevesi (basit):** kutu **≥ 4 vCPU / 16 GB** ise (Next.js build'leri builder'da RAM yer) → in-house v0. İskelet kod **backend-bağımsız** yazıldı: `runner.Driver` arayüzü var, `DockerDriver` v0'ı taşır, `FlyDriver` stub durur — coğrafi genişleme gerekirse Fly bir "driver" olarak geri döner, **tek satır mimari değişmez.**

**İskelet dosya notları (v0'da doğrudan kullanılacak):**
- `driver.go` — Docker CLI exec'iyle çalışan, gVisor'lı, `pause/unpause` (uyku) + `start/stop` (soğuk) yaşam döngülü sürücü.
- `cloud_upstream.go` — kritik desen **"probe → wake → proxy"**: istek asla retry edilmez (body-replay derdi yok), hedefe TCP probe atılır, gerekirse `Waker.Wake()` çağrılır, hedef açılınca **tek sefer** proxy'lenir; HMAC'li `X-Tunr-Edge` imzası + freeze-cache fallback içeride.
- `buildd/main.go` — Dockerfile varsa onu, yoksa Nixpacks'i koşan; path-traversal korumalı tar açan; SSE ile log akıtan tek dosyalık ajan.

> **Donanım aksiyonu:** implementasyona başlamadan mevcut sunucunun (`167.233.102.96`) vCPU/RAM'ini teyit et. ≥4 vCPU/16 GB değilse ya (a) relay/Postgres ile aynı kutuda "bilinen v0 riski" kabulüyle devam, ya (b) iş yükleri için ayrı bir VM/kutu ekle — build'ler RAM yediği için builder'a nefes alanı önemli.

---

## 5. Faz-Faz Geliştirme Planı (dosya seviyesinde)

> Her faz: **Backend (CLI/relay/CP/runner/builder) · Veri · MCP · Frontend · Kabul kriterleri.** Kesilemez çekirdek kalın.

### FAZ 0 — Deploy v0 (tracer bullet) · ~1–2 hafta

**Hedef:** `tunr deploy` (veya Claude Code'dan tek MCP çağrısı) → ~60 sn'de `https://{name}.tunr.sh` canlı; laptop kapansa yaşar; boşta uyur, istekte uyanır. Trafik relay'den geçer.

**Relay / Edge**
- `relay/internal/relayx/` (yeni paket, kaynak: `cloud_upstream.go`): `CloudUpstream` (probe→wake→ReverseProxy, edge HMAC başlığı, freeze failover), `RouteStore`, `Waker` arayüzü.
- `proxy.go` dispatch entegrasyonu (§2.2): `registry.Lookup` başarısızsa `routes.LookupCloud`.
- **Freeze'i relay'e taşı** (`internal/proxy/freeze.go`'dan uyarla → `FreezeCache` arayüzünü besle).
- Route cache geçersizleme: Postgres `LISTEN routes_changed` (pgx) + 10 sn TTL poll fallback.

**Runner** (`relay/internal/runner/`, kaynak: `driver.go`)
- `Driver` arayüzü + **`DockerDriver`** (own-server): `EnsureApp/Deploy/Wake/Sleep/Stop/Destroy/Status/Logs`. gVisor runtime (`runsc`), `--network tunr-apps` (ICC kapalı), kotalar, `no-new-privileges`, read-only rootfs.
- Sunucu kurulumu: gVisor kur, `tunr-apps` bridge network, builder host.

**Builder** (`relay/internal/builder/` veya ayrı `cmd/buildd`, kaynak: buildd `main.go`)
- Nixpacks tabanlı build agent; `POST /build` → SSE log; Dockerfile varsa `docker build`, yoksa `nixpacks build`. Own-server'da registry push gerekmez (image daemon'a lokal). Tek build kuyruğu (mutex).

**Kontrol Düzlemi** (`relay/internal/controlplane/`)
- `POST /v1/apps`, `POST /v1/apps/{id}/deployments` (multipart tar.gz), `GET /v1/deployments/{id}/events` (SSE), `GET /v1/apps`, `DELETE /v1/apps/{id}`.
- Auth: mevcut `tunr login` JWT'si (keychain). Env'ler sealed (libsodium sealed box).
- Idle sweeper goroutine: `CloudUpstream.LastSeen()` → 5 dk `Sleep`, 2 saat `Stop`.

**CLI** (`cmd/tunr/deploy.go` — yeni komut ailesi)
- `tunr deploy [dir] --name --env --port --region --json --no-cache`
- `tunr apps list|delete|logs`
- Paketleme: tar.gz (`.gitignore`+`.tunrignore`, `.env*`/`node_modules`/`.git` hariç, 50MB tavan), SSE tüketimi, canlı log çıktısı.
- `internal/config` + `.tunr.schema.json`: `app` bloğu (`name`, `region`, `internal_port`, `env`).

**tunr-shim** (`cmd/tunr-shim/` — ~2MB statik binary)
- Start komutunu sarar; `X-Tunr-Edge` HMAC doğrular (komşu erişimini keser); stdout/stderr'i CP'ye WS ile akıtır (runtime log); hazırlık kapısı.

**Veri:** `apps`, `deployments`, `routes` tabloları + `NOTIFY`. Tünel subdomain'leri de `routes`'a `kind='tunnel'` yazılır (tek isim uzayı).

**MCP:** `tunr_deploy_app`, `tunr_list_apps`, `tunr_get_logs(kind: build|http|runtime)`, `tunr_delete_app`, `tunr_get_deploy_status`. CLI `--json` olaylarıyla birebir şema.

**Frontend:** `landing/app/app/dashboard/apps/page.tsx` (yeni) — app listesi (ad, canlı URL, status, son deploy, "Logs"). Landing'e "deploy" anlatı bloğu + waitlist. (Share dialog Faz2.)

**✅ Kabul (kesilemez çekirdek):** boş Next.js dizininden tek komutla URL < 60 sn · 10 dk sonra makine `suspended` → istek → yanıt < 3 sn · **laptop kapat → app yaşar** · MCP "publish this" → URL · `apps delete` → temiz.

---

### FAZ 1 — Kalıcılık & App Kapsülü · Ay 1–2

**Hedef:** App = `kod + SQLite + secrets + ortam` taşınabilir birim; **`tunr detach`** killer feature.

- **Scale-to-zero cila:** idle sweeper + wake gecikmesi ölçümü (hedef < 1 sn); relay "Waking your app…" ara sayfası (auto-refresh).
- **`tunr detach` / `attach`** (hybrid route): çalışan tüneli paketle → runner'a taşı → **aynı URL** buluttan; tünel koparsa cloud'a otomatik failover. HN başlığı hazır: *"Close your laptop, your localhost app keeps running."*
- **SQLite-per-app** + Litestream/nesne depolamaya sürekli replikasyon → verisiyle snapshot/fork.
- **Secrets:** `tunr secret set` (sealed) + `set_secret` MCP.
- **Cron:** `tunr cron "0 9 * * 1" /report` + `create_cron` MCP.
- **Runtime log akışı** (shim WS → CP → `tunr logs`/dashboard).
- **snapshot/rollback v1:** `tunr rollback` kod+veri geri alır.

**Frontend:** `dashboard/apps/[id]/page.tsx` — Overview / Logs (canlı) / Env / Database / Versions (rollback butonu) / Settings sekmeleri.

**✅ Kabul:** detach sonrası aynı URL kesintisiz · SQLite yazımı yeniden deploy'da korunur · rollback < 10 sn · cron tetiklenir.

---

### FAZ 2 — "Google Doc gibi paylaş" (Gate) · Ay 2–4

**Hedef:** RFS kuzey yıldızı — app'i doküman gibi paylaş; auth app'in *önünde*.

- **tunr Gate** (relay): paylaşım politikası (herkese açık / linki olan / şu e-postalar / `@şirket` / SSO); Google/GitHub OAuth; imzalı **`X-Tunr-User`** JWT başlığı app'e; **roller HTTP seviyesinde** (viewer=güvenli metot [demo mode genelleşmesi], editor=tümü, admin=+ayarlar).
- **Paylaşım diyaloğu + app sayfası** (dashboard): isim, sahip, "Aç", erişim iste.
- **Widget v2:** canlı app üstünde yorum/pin → `tunr_get_feedback` MCP → ajan düzeltme → yeniden deploy → sürüm geçmişinde v1→v2.
- **Ajan-yönelik dokümantasyon:** `X-Tunr-User` okuyan 5 satırlık middleware'ler (Express/Hono/FastAPI/Next) + `llms.txt` + **"tunr skill"** (Claude Code plugin) → ajan deploy+share desenini kendiliğinden kurar.
- **İzin Manifesti kartı:** app açılmadan "şuna erişiyor / şuna erişemiyor" (runtime politikasından türetilir, yalan söyleyemez).
- **Remote/HTTP MCP** (relay üstünde) — ajan CI/bulutta da kontrol düzlemine erişir.

**Frontend:** Paylaşım modalı (Google-Doc benzeri rol seçimi), yorum katmanı, versiyon timeline, İzin Manifesti kartı.

**✅ Kabul:** iş arkadaşı linke tıklar → Google ile girer (app'te 0 satır auth) → editor yazar · danışman viewer salt-okunur + yorum · yorum ajana düşer.

---

### FAZ 3 — Ekipler & Kurumsal Ortam · Ay 4–6

- Org çalışma alanları + şirket-içi app dizini; OIDC/SSO; ortam şablonları (izinli egress, ortak secret kasası, zorunlu SSO domaini, bölge); **`tunr connect`** (şirket-içi kaynağı buluta güvenli aç — tünel DNA'sının ters yönü); **default-deny egress**; BYOC runner (kontrol düzlemi tunr'da, compute şirketin VM'inde — docker-compose self-host evrimi); audit log; **Team plan**.

### FAZ 4 — Ekosistem · Ay 6+

- Remix/fork (verisiyle); şablon galerisi; "Edit with your agent" derin linkleri (app sayfasından Claude Code/Cursor'da aç); AI-builder'lara **platform API**; app başına **Postgres opsiyonu** (büyüyen app çıkış rampası + lock-in korkusunu öldürür); `tunr export` (kapsül tar).

---

## 6. Frontend Dönüşüm Planı (kullanıcının özel isteği)

**Bilgi mimarisi:** "Tunnels" merkezli dashboard → **"Apps" merkezli**. Tünel bir "Preview/Live" alt-özelliği olur.

**Yeni/değişen sayfalar (`landing/app/app/dashboard/`):**
- `apps/page.tsx` — app listesi (status, URL, son deploy, kullanıcı sayısı). **Ana ekran.**
- `apps/[id]/page.tsx` — Overview · Logs · Env · Database · Versions · **Sharing** · Settings.
- `apps/[id]/share` (modal) — Google-Doc rol seçimi (Faz2).
- `tunnels/page.tsx` — kalır, "Live preview tunnels" olarak konumlanır.
- `feedback/page.tsx` — "yorumlar" (widget v2) ile birleşir.
- Sidebar: **Apps** en üste; Tunnels, Domains, Feedback, Settings altında.

**Landing (`landing/*.html`):** hero yeniden yazımı ("localhost'ta doğan yazılımın yaşayacağı yer"); deploy+share + MCP/ajan anlatısı; 60 sn demo videosu gömme; pricing evrimi: **Free** (sınırsız tünel + 3 uyuyan app + `tunr.app` subdomain), **Pro ~$15/ay** (20 app, custom domain, always-on, roller), **Team** ($8–12/koltuk). Karşılaştırma sayfaları güncellenir (Val Town, Replit/Lovable/Bolt eklenir).

**Design system:** mevcut token'lar (`--bg #080b14`, `--cyan #00d4ff`, Inter/JetBrains Mono) **korunur** — yalnız içerik/bilgi mimarisi değişir.

**Teknik borç:** `landing/app/` iç içe `.git`'i çözülür (K5); `landing/firebase-key.json` ve `.env.local.example`'daki sızıntı **temizlenir/rotasyon** (bkz. hafıza `firebase-secret-exposed`); eski statik `landing/dashboard.html` + `admin.html` silinir.

---

## 7. Ajan-Yerel Kontrol Düzlemi (MCP) — "her işi agent yapsın"

**İlke:** *Dashboard'da yapılabilen her şey MCP'de de yapılabilir; MCP birincil arayüz, CLI ve dashboard onun insan yüzleridir.*

**Hedef araç seti (faz faz açılır):**
- Faz0: `deploy_app`, `list_apps`, `get_logs`, `get_deploy_status`, `delete_app`.
- Faz1: `rollback`, `snapshot`/`restore`, `create_db`/`query_db`, `set_secret`, `create_cron`.
- Faz2: `set_share_policy`, `invite_user`, `get_feedback`.
- Faz3: `attach_connect` (iç kaynak bağla), org/ortam araçları.

**Dağıtım kanalları:** MCP registry'leri, **Claude Code skill/plugin**, Cursor rules, `llms.txt`, `AGENTS.md` şablonu. Dokümantasyonu **insanlara değil ajanlara** yaz — dönemin en ucuz dağıtım kanalı.

**Transport:** bugün stdio (`tunr mcp`); Faz2'de relay üstünde **remote HTTP MCP** (ajan CI/bulutta da erişir).

---

## 8. Veri Modeli & Protokol Değişiklikleri

- **Yeni tablolar:** `apps` (id, user_id, name UNIQUE, region, internal_port, edge_secret, status), `deployments` (id, app_id, seq, image_ref, status, error, env_enc, UNIQUE(app_id,seq)), `routes` (subdomain PK, kind: tunnel|cloud|hybrid, cloud_url, wake_timeout). Migration: `relay/migrations/003_apps.sql`.
- **Route invalidasyonu:** `LISTEN/NOTIFY routes_changed`.
- **Wire protokol:** tünel protokolü (`MsgType*`) **dokunulmaz** — deploy REST/SSE üzerinden gider. Yalnız `hybrid` route mantığı relay dispatch'e eklenir (detach). Eski peer'lar bilinmeyen route kind'ını tolere eder.
- **Config:** `.tunr.json` `app` bloğu + schema güncellemesi.

---

## 9. Güvenlik & Trust/Safety

- **v0 dürüst durum:** İzolasyon = **gVisor (runsc)** + `no-new-privileges` + read-only rootfs + mem/CPU/pid limitleri + `icc=false` bridge (hepsi `driver.go`'da). İdeal: iş yükleri relay/Postgres'ten ayrı kutu/VM'de; tek kutudaysak "bilinen v0 riski" olarak kayda geçer (bkz. §4.1). Paylaşılan bridge'de komşu erişimini **shim HMAC** keser; kotalar; sealed env (yalnız CP'de çözülür); free hesap frenleri (3 app / 10 deploy-gün / 256MB). Egress v0'da açık (landing'de default-deny **iddia edilmez**) — ama in-house olduğumuz için `iptables` default-deny egress Faz 1'de Fly'dakinden çok daha kolay gelir.
- **Faz1+:** default-deny egress (iptables allowlist / gVisor), İzin Manifesti.
- **Koru:** mevcut güvenlik sertleştirmeleri (magic-link/verify prod gating, WS origin kontrolü, rate-limit cap, JWT alg=HS256, Paddle HMAC), `SECURITY:` yorumları, keychain — hiçbiri regresyona uğramaz.
- **Abuse:** subdomain interstitial ("kullanıcı uygulaması" bandı), imaj/subdomain itibar taraması (Faz1+).

---

## 10. Riskler (pre-mortem)

| Risk | Panzehir |
|---|---|
| Vercel/Cloudflare aynısını yapar | Hız + huni sahipliği (tünel kullanıcıları) + ajan-agnostik konum + paylaşım derinliği |
| Val Town zaten var | Her stack (yalnız JS değil), lokal↔bulut sürekliliği, kurumsal Gate; Val Town'dan anti-lock-in dürüstlüğünü kopyala |
| Abuse maliyeti (ücretsiz hosting = phishing) | T&S ilk sınıf; interstitial + kotalar + tarama |
| Boşta-app ekonomisi / wake gecikmesi | scale-to-zero + SQLite; snapshot-restore'a erken yatırım; suspend fallback→stop |
| 2 kişilik ekip, geniş kapsam | Fazların acımasız sıralaması; Faz0–1'de yalnız deploy+detach, "auth" kelimesi Faz2'ye kadar yasak |
| Tünel kullanıcısı yabancılaşması | Tünel ücretsiz/birinci sınıf kalır; mesaj "tünelin artık devamı var" |

---

## 11. İlk Sprint (uygulamaya geçince)

**Branch:** `git checkout -b pivot/cloud` (main dokunulmaz).

**Tracer bullet (Gün 1 ölçütü):** relay `relayx.CloudUpstream` + `RouteStore` + `proxy.go` dispatch + `routes` tablosu + LISTEN/NOTIFY; sunucuda **elle açılmış** bir hello-world container'a subdomain bağla ve `https://x.tunr.sh` → relay → wake → servis zincirini **build olmadan** kanıtla.

**Sonra:** DockerDriver → buildd → CLI `deploy` paketleme+SSE → `.tunr.json` app bloğu → MCP `deploy_app` → shim → dashboard `apps` sayfası → demo.

**Task'a dönüştürme:** implementasyona geçince bu §5/Faz0 maddeleri TaskCreate ile iş kalemlerine bölünecek.

---

## 12. Kararlar

**✅ Kilitlenenler (2026-07-23):**
1. **K1 — Runner:** Own-server Docker+gVisor + `Driver` arayüzü (Fly stub olarak kalır).
2. **K5 — `landing/app` iç içe git:** Ana repoya dahil edilecek (iç `.git` kaldırılır) — Faz 0 ilk adımı.
3. **İlk sprint:** Tracer bullet önce (relay CloudUpstream + elle açılmış container'a subdomain, build'siz kanıt).

**⏳ Sonraya bırakılanlar:**
4. **Lisans + fiyatlandırma yeniden değerlendirmesi (iş sonu ajandası — kullanıcı isteği):** Faz 0/1 çalışır hâle geldikten sonra mevcut open-source konumu (PolyForm Shield) ve fiyatlandırmayı (bugün Free / Pro $5) birlikte gözden geçir. Adaylar: open-core (CLI/SDK Apache-2/MIT + control-plane kapalı/FSL) ve pivot fiyat evrimi (Free + 3 uyuyan app / Pro ~$15 / Team koltuk). **Faz 0'ı bloklamaz** — deploy çalışınca konuşulur.
5. Frontend "apps" iskeletinin backend'e paralel mi yoksa sonra mı yapılacağı — tracer bullet yeşile döndükten sonra netleşir.

*Bu doküman, girdi raporlarındaki vizyonu mevcut kodun gerçek dosyalarına bağlar; hiçbir Faz 0 satırı sonraki fazlarda çöpe gitmeyecek şekilde (route şeması + shim + Driver soyutlaması) ileriye dönük kurgulanmıştır.*
