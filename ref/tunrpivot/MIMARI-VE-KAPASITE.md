# tunr Cloud — Mimari Şeması, Çalışma Sistemi ve Kapasite Analizi

> Hedef sunucu spesifikasyonu: **16 GB RAM · 4 vCPU · 320 GB disk**
> (Not: mevcut prod kutusu `91.98.42.7` aslında **8 vCPU / 15 GB / 301 GB** — yani CPU 2× fazla, RAM biraz daha az. Aşağıdaki sayılar 16/4/320 hedefine göre, ölçümler gerçek prod'dan.)
> Ölçüm tarihi: 2026-07-24 · Kaynak: canlı `docker stats` + `df` + kod (`relay/internal/runner/driver.go`, `relay/cmd/runner/main.go`, `sweeper.go`).

---

## 1. Sistem Topolojisi (üst seviye)

```
                              İNTERNET
                                 │
                    ┌────────────┴─────────────┐
                    │  Cloudflare (apex/www)    │   *.tunr.sh + app.tunr.sh = DNS-only (origin'e direkt)
                    │  CSS 4h cache             │   apex/www = proxied + cache
                    └────────────┬─────────────┘
                                 │  :443
        ┌────────────────────────┴──────────────────────────────────┐
        │                    SUNUCU (91.98.42.7)                     │
        │                                                            │
        │   ┌──────────┐    tunr_default (bridge)                    │
        │   │  Caddy   │───────┬───────────────┬──────────────┐      │
        │   │ 80/443   │       │               │              │      │
        │   │  TLS     │   app.tunr.sh     *.tunr.sh      /api,/tunnel│
        │   └──────────┘       │               │              │      │
        │        │             ▼               ▼              ▼      │
        │        │      ┌────────────┐   ┌───────────────────────┐   │
        │   statik      │ Dashboard  │   │        RELAY          │   │
        │   landing     │ (Next.js)  │   │  (scratch, Docker'sız) │   │
        │   bind-mount  │  :3000     │   │        :8080          │   │
        │               └─────┬──────┘   │  ┌─────────────────┐  │   │
        │                     │          │  │ Registry        │  │   │
        │                     │          │  │ Proxy (subdomain)│  │   │
        │                     │          │  │ RouteStore      │  │   │
        │                     │          │  │ CloudUpstream   │  │   │
        │                     │          │  │ Control Plane   │  │   │
        │                     │          │  │ Idle Sweeper    │  │   │
        │                     │          │  └────────┬────────┘  │   │
        │                     │          └───────────┼───────────┘   │
        │                     │                      │ HTTP (RUNNER_URL)
        │                     │                      ▼               │
        │                     │          ┌───────────────────────┐   │
        │                     │          │   tunr-runner (sidecar)│   │
        │                     │          │  debian + docker CLI   │   │
        │                     │          │  + buildx + nixpacks   │   │
        │                     │          │  /var/run/docker.sock  │   │
        │                     │          └───────────┬───────────┘   │
        │                     │                      │ docker run/pause/stop
        │          ┌──────────┴────────┐             ▼               │
        │          │    Postgres 16     │   ┌──────────────────────┐ │
        │          │  (tek DB, paylaşılan)│  │  tunr-apps (bridge)   │ │
        │          │  relay + dashboard │◀──┤  icc=false izole      │ │
        │          └───────────────────┘   │  ┌────┐ ┌────┐ ┌────┐  │ │
        │                                   │  │app1│ │app2│ │app3│… │ │
        │   Firebase = SADECE OAuth         │  │gVisor(runsc) sandbox│ │
        │   (login; hesap DB'de)            │  └────┘ └────┘ └────┘  │ │
        │                                   └──────────────────────┘ │
        └────────────────────────────────────────────────────────────┘
```

**Ağ izolasyonu:** App container'ları `tunr-apps` bridge'inde, `enable_icc=false` ile — yani **tenant'lar birbirini göremez**. Relay bu ağa ayrıca bağlı ve `DOCKER-USER` iptables "relay-allow" kuralıyla app'lere erişebilen tek bileşen. Her app **gVisor (runsc)** sandbox'ında, read-only rootfs + tmpfs + pids-limit + memory/cpu quota ile çalışır.

---

## 2. Bileşen Envanteri ve Rolleri

| Bileşen | İmaj / Taban | Rol | Ölçülen RAM (idle) |
|---|---|---|---|
| **Caddy** | `tunr-caddy` (~154 MB) | TLS sonlandırma, subdomain yönlendirme, statik landing | ~15 MB |
| **Relay** | scratch (~16 MB) | Kontrol düzlemi + veri düzlemi; **Docker'a hiç dokunmaz** | ~4 MB |
| **Runner (sidecar)** | debian-slim + docker CLI + buildx + nixpacks (~300 MB) | Docker'ı süren tek yer: build + run + wake/sleep/stop | ~3 MB |
| **Dashboard** | Next.js (`next start`) (~1.45 GB imaj) | Kullanıcı arayüzü (app.tunr.sh) | ~87 MB |
| **Postgres 16** | `postgres:16` | Tek DB; relay + dashboard paylaşır (route, app, deployment, kullanıcı) | ~47 MB |
| **App container** | build çıktısı (slim/distroless ~150 MB) | Kullanıcı uygulaması, gVisor'da | **~46 MB** (küçük Node) |

**Kontrol düzlemi toplamı (app'ler hariç): ~156 MB.** İşletim sistemi + dockerd + gVisor daemon ile birlikte pratik taban rezervasyonu **~2 GB**.

---

## 3. Çalışma Akışları

### 3.1 Deploy (build + run)

```
CLI (tunr deploy)  ──JWT──▶  Relay /v1/deploy
   │  kaynak tar.gz                │  GetOrCreateApp, InsertDeployment
   │                              ▼
   │                       Runner /v1/deploy  (multipart: meta + tar.gz, SSE)
   │                              │  1. extract
   │                              │  2. build:
   │                              │       Dockerfile varsa → docker build
   │                              │       yoksa → SLIM Dockerfile üret (Node distroless / Python slim)
   │                              │       tanınmazsa → nixpacks fallback
   │                              │     (buildMu: AYNI ANDA 1 BUILD)
   │                              │  3. docker run --runtime runsc --network tunr-apps
   │                              │       --memory 256m --cpus 1 --read-only --pids-limit 256
   │                              ▼
   │◀── SSE loglar ──  {"event":"ready","endpoint":"http://<ip>:8080"}
   ▼
Relay: UpsertCloudRoute(subdomain→ip), SetAppStatus(live)
```

Build çıktısı disk'te bir imaj; container ayakta; route DB'ye ve RouteStore'a yazılır (Postgres LISTEN/NOTIFY ile diğer relay örneklerine yayılır).

### 3.2 İstek geldiğinde — sıcak app (running)

```
Browser → Caddy → Relay (subdomain lookup) → CloudUpstream
    → reverse-proxy → app container IP:8080 → cevap
```
Ekstra gecikme ~0. `CloudUpstream` hedefi RWMutex altında okur; imzalı `X-Tunr-Edge` başlığı ekler (tunr-shim doğrular).

### 3.3 İstek geldiğinde — uykudaki app (wake-on-request)

```
Browser → Relay → CloudUpstream (sleeping=true atomik bayrak)
    │  1. Wake ÖNCE (probe değil — paused container TCP handshake'e cevap verir,
    │     dolayısıyla yalnızca probe onu uyandırmaz)
    │       paused → docker unpause  (~anında, cgroup freezer)
    │       stopped → docker start   (saniyeler; IP DEĞİŞİR!)
    │  2. Runner Wake, container'ın GÜNCEL IP'sini döner
    │  3. CloudUpstream.updateHost(ip) — cold-stop sonrası staled route düzeltilir
    │  4. dialable olana kadar probe → reverse-proxy
    ▼
```

### 3.4 Scale-to-zero (Idle Sweeper durum makinesi)

```
        deploy / istek
           │
           ▼
      ┌─────────┐   5 dk boşta    ┌──────────┐   2 saat boşta   ┌──────────┐
      │ RUNNING │ ───────────────▶│  PAUSED  │ ────────────────▶│ STOPPED  │
      │ RAM: tam│                 │RAM resident│                │ RAM: 0   │
      └─────────┘◀── istek ───────└──────────┘◀── istek ────────└──────────┘
         unpause (~anında)            start (saniyeler, yeni IP)
```

- **PAUSED** = `docker pause` (cgroup freezer). RAM **resident kalır** → RAM bütçesine sayılır. Uyanma anlık.
- **STOPPED** = `docker stop`. RAM **serbest**, sadece disk. Uyanma saniyeler + **IP değişir** (bu yüzden Wake IP'yi geri döndürür ve `CloudUpstream.updateHost` route'u günceller).

> **Kapasitenin özü:** RAM sınırı yalnızca **{running + paused}** app'lere uygulanır. **stopped** app'ler RAM tüketmez, sadece disk. Bu yüzden "kaç app" sorusunun üç farklı cevabı var (aşağıda).

---

## 4. Kaynak Modeli (ölçülen)

| Öğe | Değer | Kaynak |
|---|---|---|
| App bellek cap'i (varsayılan) | 256 MB (`--memory 256m`) | `driver.go` |
| App CPU cap'i (varsayılan) | 1.0 vCPU (`--cpus 1`) | `driver.go` |
| App **gerçek** RSS (küçük idle Node) | **~46 MB** | canlı `docker stats` |
| gVisor (runsc) sandbox ek yükü | ~15–30 MB / container | genel |
| Kontrol düzlemi (app'ler hariç) | ~156 MB | canlı `docker stats` |
| Taban rezervasyon (OS+docker+gVisor+kontrol) | ~2 GB | tahmin + ölçüm |
| App başına slim imaj | ~150–250 MB (katmanlar paylaşımlı) | build testi (170 MB) |
| Eski nixpacks imajı | ~886 MB | önceki ölçüm |
| Build eşzamanlılığı | **1** (`buildMu`) | `runner/main.go` |
| Pause eşiği / Stop eşiği | 5 dk / 2 saat | `sweeper.go` |

---

## 5. Kapasite — "Aynı anda kaç uygulama?" (16 GB / 4 vCPU / 320 GB)

Taban rezervasyon ~2 GB düşülünce **~14 GB** app'lere kalır.

| Boyut | Sınırlayan | Formül | Sonuç |
|---|---|---|---|
| **Sıcak app (running+paused) — garantili** (hepsi 256 MB cap'i dolu) | RAM | 14 GB ÷ (256+30) MB | **~48 app** |
| **Sıcak app — tipik küçük idle** (oversubscribe; ~70 MB gerçek) | RAM | 14 GB ÷ 70 MB | **~200 (temkinli 100–150)** |
| **Toplam barındırılan** (çoğu cold/stopped, 0 RAM) | Disk | ~280 GB ÷ ~150 MB (paylaşımlı katman) | **~1.000–1.500 app** |
| **Aynı anda canlı trafik alan** | CPU (4) | idle ~%0; hafif istek < 1 çekirdek | **onlarca** |
| **Aynı anda build (deploy hızı)** | `buildMu`=1 | seri | **1** |

### Manşet cevap

- **~50 app** tam kaynak garantisiyle (256 MB cap dolu) **aynı anda sıcak**.
- **~100–150 tipik küçük idle app** oversubscribe ile aynı anda sıcak (çoğu araç gerçekte ~46 MB kullanıyor).
- **~1.000+ app toplam barındırılır** (çoğu cold=0 RAM) — çünkü scale-to-zero, boştaki çoğunluğu sıfır RAM'de tutar.
- **Deploy hızı: aynı anda 1 build** (kuyruk; her build ~30 sn–3 dk).

> Gerçek "eşzamanlı" sayı **aktif oran**a bağlı: app'lerin %10'u herhangi bir anda aktifse → 1.000 barındırılan ≈ 100 sıcak (rahat sığar). %50 aktifse → ~300 barındırılan tavanı.

Mevcut kutu 8 çekirdek olduğundan **serving tarafında 2× daha rahat**; RAM 15 GB olduğundan sıcak-app sayısı ~%6 daha düşük.

---

## 6. Darboğazlar ve Ölçekleme Kolları (ileride ortak kullanım araştırması için)

| Darboğaz | Bugün | Kol / iyileştirme |
|---|---|---|
| **RAM (sıcak app tavanı)** | ~50 garantili / ~150 tipik | Daha agresif pause (5 dk → 60–90 sn); per-app cap'i 128 MB'a düşürmek; swap eklemek (şu an 0!) |
| **Build seri (buildMu=1)** | 1 eşzamanlı | Build havuzu (N paralel), veya build'i ayrı bir "builder" node'a taşımak |
| **CPU** | 4 çekirdek, idle app'lerde bol | Aktif app'ler artınca çekirdek eklemek; CPU cap'i 0.5'e düşürmek |
| **Disk (HDD)** | 320 GB, imajlar paylaşımlı | Registry + garbage collection; snapshot'lar için ayrı volume; build cache prune (zaten 20 GB'de tutuluyor) |
| **Swap yok** | 0 B | Swap RAM oversubscribe'ı güvenli kılar — sıcak app tavanını ~2× artırabilir |
| **Tek node** | her şey tek kutuda | Runner'ı ayırmak; çok bölgeli relay (balancer altyapısı hazır: ams/sea/sin) |
| **Postgres tek** | paylaşımlı | Büyürse ayrı DB node |

### Pratik öneri (ortak kullanım öncesi)
1. **Swap ekle** (8–16 GB) → oversubscribe güvenli, sıcak tavan yükselir.
2. **Pause eşiğini düşür** (60–90 sn) → aynı RAM'de daha çok app cold'a iner.
3. **Per-app cap'i düşür** (128 MB varsayılan, ihtiyaç halinde büyüt) → garantili sıcak sayısı 2×.
4. **Build'i ayır** → deploy kuyruğu darboğazını kaldırır.
5. **Registry + GC** → disk uzun vadede şişmez.

Bu 5 adım tek kutuda **~50 → ~150+ garantili sıcak app** ve **~1.000 → ~3.000+ barındırılan app** aralığına taşır.
