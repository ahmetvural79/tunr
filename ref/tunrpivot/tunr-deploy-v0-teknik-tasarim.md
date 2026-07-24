# `tunr deploy` v0 — Teknik Tasarım Dokümanı
### Nixpacks build · Fly Machines runtime · Relay eşlemesi

*Sürüm: v0 tasarımı, 22 Temmuz 2026. Hedef: 3 kodlama gününde çalışan mutlu yol + YC demo videosu.*

---

## 1. Amaç, Kapsam ve Kapsam Dışı

**Amaç:** Kullanıcı (veya MCP üzerinden bir coding ajanı) bir proje dizininde `tunr deploy` yazsın; ~30–60 saniye içinde uygulama `https://{name}.tunr.sh` adresinde, laptop kapansa da yaşayan, boştayken uyuyup istekte uyanan bir bulut kopyası olarak çalışsın. Trafik **mutlaka tunr relay'inden** geçsin (gelecekteki Gate/yorum/log/detach katmanlarının ön koşulu).

**v0 kapsamı:** Node (Next.js dahil) ve Python (FastAPI/Flask) mutlu yolu; tek makine/app; tek bölge; `--env` ile ortam değişkeni; build loglarının CLI'ya canlı akışı; `tunr apps list|delete`; MCP `deploy_app` aracı; HTTP erişim logları (relay'in mevcut inspector'ı üzerinden).

**Kapsam DIŞI (v0):** Gate/auth, paylaşım diyaloğu, SQLite servisi, cron, blue-green, custom domain, çoklu bölge, runtime stdout loglarının tam çözümü (shim'le kısmi — bkz. §8), default-deny egress, `detach`. Bunların hiçbiri v0'ı bloklamıyor ama route şeması ve shim tasarımı bunlara **ileriye dönük uyumlu** kurgulanıyor (§5, §8).

**Yol gösterici ilkeler:** (1) CLI aptal, kontrol düzlemi akıllı — Fly token'ları asla kullanıcı makinesine inmez; (2) her adım `--json` ile makine-okunur (MCP bedavaya gelir); (3) mevcut relay koduna cerrahi ekleme, yeniden yazım yok.

---

## 2. Mimari Genel Bakış

```
┌─────────────┐  tar.gz + meta   ┌───────────────────────────────┐
│ tunr CLI /  │ ───────────────► │ Kontrol Düzlemi (CP)           │
│ MCP ajanı   │ ◄─── SSE loglar  │ • REST API (relay binary'sine  │
└─────────────┘                  │   eklenir veya yanına konur)   │
                                 │ • Postgres (mevcut)            │
                                 │ • Fly org token (yalnız burada)│
                                 └───────┬──────────────┬────────┘
                                         │ build işi     │ Machines API
                                         ▼               ▼
                              ┌──────────────────┐  ┌──────────────────────┐
                              │ Builder Machine   │  │ Fly App (uygulama)    │
                              │ (Fly'da, dockerd  │  │ tunr-a-{id}           │
                              │  + nixpacks +     │  │ • 1 Machine, 256MB    │
                              │  buildd ajanı)    │  │ • autostop: suspend   │
                              │ push ────────────►│  │ • Flycast (private)   │
                              └──────────────────┘  └──────────┬───────────┘
                                   registry.fly.io              │ 6PN (org içi)
                                                                │
   Kullanıcı ── https://{name}.tunr.sh ──► ┌────────────────────▼─┐
   tarayıcısı                              │ Relay (mevcut, Fly'da)│
                                           │ route: subdomain →    │
                                           │  a) tünel oturumu     │
                                           │  b) CLOUD upstream:   │
                                           │     http://tunr-a-{id}│
                                           │     .flycast          │
                                           └───────────────────────┘
```

**En kritik altyapı kararı ve gerekçesi:** Relay, uygulama makinesine `{fly-app}.flycast` adresi üzerinden gider, **asla `.internal` üzerinden değil.** Çünkü Fly'ın autostop/autostart mekanizması Fly Proxy'de yaşar; `.internal` DNS'i makineye doğrudan gider ve **uyuyan makineyi uyandırmaz** — Flycast ise Fly Proxy'den geçtiği için istekte makineyi başlatır ve makine çalışmıyorken bile adres erişilebilir kalır. Bu tek satırlık tercih, v0'a scale-to-zero'yu neredeyse bedavaya kazandırır: `auto_stop_machines = "suspend"` ile makine boşta bellek durumuyla askıya alınır, istekte hızla geri döner; durmuş/askıdaki makinede CPU/RAM ücreti işlemez. Uygulamanın `0.0.0.0:{internal_port}` dinlemesi şarttır (Fly Proxy erişimi için) — bu, build aşamasındaki PORT konvansiyonumuzun (§6.3) gerekçesi.

---

## 3. Bileşen 1 — CLI: `tunr deploy`

### 3.1 Komut yüzeyi

```
tunr deploy [dizin] [flags]
  --name    <string>   subdomain/app adı (yoksa .tunr.json'dan; o da yoksa üret + kaydet)
  --env     KEY=VAL    tekrarlanabilir; makineye env olarak geçer
  --port    <int>      uygulamanın dinlediği port (varsayılan: PORT konvansiyonu, 8080)
  --region  <string>   v0'da tek değer kabul: "ams" (relay'e en yakın)
  --json               makine-okunur olay akışı (satır başına bir JSON)
  --no-cache           builder'da katman önbelleğini atla

tunr apps list | delete <name> | logs <name>   (v0: logs = HTTP erişim logları)
tunr rollback <name>                            (v0.1 — bkz. §11)
```

`.tunr.json` genişletmesi (mevcut şemaya `app` bloğu):

```json
{
  "$schema": "https://tunr.sh/schema/.tunr.schema.json",
  "port": 3000,
  "app": { "name": "sprint", "region": "ams", "internal_port": 8080, "env": { "NODE_ENV": "production" } }
}
```

### 3.2 Paketleme

CLI, dizini `tar.gz` yapar: `.gitignore` + `.tunrignore` kurallarına uyar; her koşulda dışlanır: `.git/`, `node_modules/`, `.next/cache`, `__pycache__/`, `.venv/`, `*.sqlite` (v0'da veri taşımıyoruz), `.env*` (**bilinçli**: gizli dosyalar asla arşive girmez; env yalnız `--env`/config ile gider — CLI, dizinde `.env` görürse "`.env` dosyanız yüklenmedi; değişkenleri --env ile geçin" uyarısı basar). Boyut tavanı v0: **50 MB** sıkıştırılmış (aşımda net hata). Arşiv özeti (SHA-256) meta'ya yazılır; ileride içerik-adresli önbellek için.

### 3.3 Kullanıcı deneyimi (çıktı taslağı — demo videosunun senaryosu)

```
$ tunr deploy --name sprint
  ▲ Packing… 214 files, 3.1 MB (node_modules skipped)
  ▲ Uploading… done
  ▲ Building (nixpacks · node 20 detected)
  │  ...build log satırları canlı akar (SSE)...
  ▲ Releasing image dep_9f2c…
  ▲ Health check: OK (1.8s)
  🚀 Live: https://sprint.tunr.sh        (sleeps when idle, wakes on request)
```

`--json` modunda aynı adımlar `{"event":"building","detail":…}` satırları olarak akar; MCP aracı bu akışı aynen tüketir.

---

## 4. Bileşen 2 — Kontrol Düzlemi (CP)

### 4.1 Yerleşim

v0'da ayrı servis açma: mevcut relay binary'sinin API tarafına (login/token altyapısı zaten orada) `/v1/apps` ve `/v1/deployments` uçları eklenir. Fly org token'ı ve registry kimliği yalnızca CP ortamında yaşar. (Kod düzeni bozulacaksa alternatif: `relay/cmd/cp` olarak ikinci binary, aynı Postgres — mimari fark yok.)

### 4.2 Veri modeli (Postgres)

```sql
CREATE TABLE apps (
  id            text PRIMARY KEY,            -- "a_x7k2…" (kısa ulid)
  user_id       text NOT NULL REFERENCES users(id),
  name          text NOT NULL UNIQUE,        -- subdomain ile aynı
  region        text NOT NULL DEFAULT 'ams',
  fly_app_name  text NOT NULL UNIQUE,        -- "tunr-a-x7k2…"
  internal_port int  NOT NULL DEFAULT 8080,
  edge_secret   text NOT NULL,               -- shim HMAC anahtarı (§8)
  status        text NOT NULL DEFAULT 'created', -- created|live|deleted
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE deployments (
  id           text PRIMARY KEY,             -- "dep_…"
  app_id       text NOT NULL REFERENCES apps(id),
  seq          int  NOT NULL,                -- app içinde artan; rollback bununla
  image_ref    text,                         -- registry.fly.io/tunr-a-…:dep_…
  status       text NOT NULL,                -- queued|building|pushing|releasing|healthy|failed
  error        text,
  env_enc      bytea,                        -- env snapshot, CP anahtarıyla mühürlü
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (app_id, seq)
);

-- Relay'in tek baktığı tablo. Şema, ileriki `detach`/hybrid için bugünden hazır:
CREATE TABLE routes (
  subdomain     text PRIMARY KEY,
  kind          text NOT NULL,               -- 'tunnel' | 'cloud' | 'hybrid'
  cloud_url     text,                        -- http://tunr-a-….flycast:80
  wake_timeout  int  NOT NULL DEFAULT 30,    -- sn; ilk bayt için (uyanma payı)
  updated_at    timestamptz NOT NULL DEFAULT now()
);
```

Tünel subdomain'leri de aynı `routes` tablosuna `kind='tunnel'` ile kaydedilir (mevcut in-memory kayıt kalkmak zorunda değil; tabloya yazım eklenir). Böylece **tek isim uzayı** garanti olur ve `detach` geldiğinde yalnız `kind`'ı `hybrid` yapmak yetecek.

### 4.3 API sözleşmesi (özet)

```
POST /v1/apps                    {name?, region?, internal_port?} → 201 {app}   (name çakışması → 409)
POST /v1/apps/{id}/deployments   multipart: meta(json) + source(tar.gz) → 202 {deployment_id}
GET  /v1/deployments/{id}/events → SSE: queued|building(log)|pushing|releasing|healthy|failed(error)
GET  /v1/apps                    → liste (durum + URL)
DELETE /v1/apps/{id}             → makineyi ve Fly app'i yok et, route sil
POST /v1/apps/{id}/rollback      {to_seq} → önceki image_ref ile release (v0.1)
```

Kimlik doğrulama: mevcut `tunr login` token'ı (OS keychain) — yeni bir şey yok.

---

## 5. Bileşen 5'i öne alalım — Relay Eşlemesi (en küçük ama en kritik değişiklik)

Bugün relay: `subdomain → aktif WebSocket tünel oturumu`. Değişiklik, bir upstream soyutlaması:

```go
type Upstream interface{ ServeHTTP(w http.ResponseWriter, r *http.Request) }

// Mevcut davranış:
type TunnelUpstream struct{ sess *Session }          // değişmiyor

// Yeni:
type CloudUpstream struct {
    target      *url.URL      // http://tunr-a-x7k2.flycast:80
    edgeSecret  string        // shim doğrulaması için imza üretimi (§8)
    wakeTimeout time.Duration // routes.wake_timeout
}
```

`CloudUpstream`, `httputil.ReverseProxy` üstüne üç davranış ekler: (1) **uyanma toleransı** — ilk bayta kadar `wake_timeout` (30 sn) bekler, `ResponseHeaderTimeout` buna göre; bağlantı reddi/timeout'ta 2 sn arayla 3 deneme (Fly Proxy makineyi kaldırırken oluşan yarış için); (2) her isteğe `X-Tunr-Edge: t=<unix>, s=HMAC(edge_secret, t‖host‖path)` başlığı ekler (§8'deki shim doğrular; shim yoksa zararsızca yok sayılır); (3) WebSocket upgrade'i standart reverse-proxy geçişiyle taşır (tünel tarafındaki özel köprüye gerek yok — bulut tarafında düz HTTP/WS).

**Route çözümü ve önbellek:** İstek geldiğinde önce in-memory tünel kaydına bakılır (bugünkü hızlı yol, sıcak yol değişmez); yoksa `routes` önbelleğine düşülür. Önbellek: process-içi map + **Postgres `LISTEN/NOTIFY`** ile geçersizleme (CP her route yazımında `NOTIFY routes_changed, '<subdomain>'`); NOTIFY bağlantısı koparsa 10 sn'lik TTL'li poll'a düşer. v0'da yalnız `tunnel|cloud` uygulanır; `hybrid` (tünel varsa tünel, yoksa cloud'a düş — `detach`'in kalbi) şemada hazır, kodda TODO.

**Freeze'in yeniden kullanımı (ucuz vitrin özelliği):** Relay'deki mevcut freeze önbelleği `CloudUpstream`'e de bağlanır: yeniden deploy sırasındaki 3–5 saniyelik makine değişim penceresinde upstream hata verirse son başarılı yanıt servis edilir. "Zero-downtime hissi"ni blue-green kurmadan verir ve mevcut özelliğin hikâyesini pivota bağlar.

---

## 6. Bileşen 3 — Builder (Nixpacks)

### 6.1 Neden sunucuda build? 

Kullanıcı makinesinde Docker varsayılamaz (vibecoder persona), registry push token'ı istemciye verilemez, ve build ortamının tekdüzeliği hata ayıklamayı basitleştirir. Bu yüzden build, tunr'a ait bir **builder makinesinde** koşar. (Fly'ın kendi `flyctl` uzak builder'ları da tam olarak "Docker koşan Fly makinesi"dir — aynı deseni kendimiz kuruyoruz ki flyctl'e ve kullanıcı başına Fly kimliğine bağımlılık olmasın.)

### 6.2 Builder makinesi

Fly app `tunr-builder` (ams), 2 vCPU / 2–4 GB, kalıcı volume (Docker katman önbelleği → tekrar build'ler dramatik hızlanır). İçinde: `dockerd`, `nixpacks` binary'si ve bizim ~300 satırlık Go ajanımız **buildd**:

```
buildd API (yalnız CP'den, org-içi + paylaşılan gizli anahtarla):
POST /build   multipart: meta{deployment_id, image_ref, env_build?} + source.tar.gz
              → SSE geri: log satırları + {status: pushed|failed}
```

buildd akışı: tar'ı `/work/{dep_id}`'e aç → **Dockerfile kuralı:** dizinde `Dockerfile` varsa doğrudan `docker build` (nixpacks atlanır; ileri kullanıcı kaçış kapısı) → yoksa `nixpacks build /work/{dep_id} --name {image_ref} [--no-cache]` → `docker push {image_ref}` (registry.fly.io; builder, deploy-scope'lu Fly token'la `docker login` olmuş durumda) → iş dizinini sil. Eşzamanlılık v0: **tek build kuyruğu** (global mutex) — 3 günlük kapsam için yeter; kuyruk pozisyonu SSE'de bildirilir. Builder da `autostop: "stop"` ile boşta uyur; CP build göndermeden önce Machines API ile start eder (builder'a istek Flycast'ten değil CP'den doğrudan start çağrısıyla gider, çünkü SSE uzun bağlantısını kendimiz yönetiyoruz).

### 6.3 PORT konvansiyonu ve start komutu

Sözleşme basit ve tek: **konteynere `PORT=8080` verilir; uygulama `0.0.0.0:$PORT` dinlemelidir.** Nixpacks'in Node sağlayıcısı `build` script'i varsa `npm run build`, start'ta `npm start` üretir (Next.js `start` PORT'a saygılıdır); Python'da `Procfile` yoksa üretilen komut uygulamaya göre değişir — v0 dokümantasyonu iki satırlık reçete verir: FastAPI için köke `Procfile: web: uvicorn main:app --host 0.0.0.0 --port $PORT`. CLI, health check zaman aşımında tam bu ipucunu basar (§12). Kullanıcı `--port` verirse `internal_port` o olur ve `PORT` env yine ona eşitlenir. `nixpacks.toml` varsa aynen saygı gösterilir.

---

## 7. Bileşen 4 — Fly Runtime

### 7.1 App + ağ + adres yaratımı (app başına bir kez)

```
1) POST https://api.machines.dev/v1/apps
   { "app_name": "tunr-a-x7k2", "org_slug": "tunr", "network": "tunr-shared" }   // ağ: bkz. §10
2) Flycast adresi tahsisi: private IPv6 (fly ips allocate-v6 --private eşdeğeri —
   Machines REST'te yoksa GraphQL mutation ya da CP'nin kabuktan flyctl çağrısı; §14 doğrulanacak)
3) Public IP TAHSİS EDİLMEZ. Uygulamaya internetten tek giriş: relay.
```

### 7.2 Machine yapılandırması (deployment başına)

```json
POST /v1/apps/tunr-a-x7k2/machines        // ilk deploy: create; sonrakiler: update (§7.3)
{
  "name": "web",
  "region": "ams",
  "config": {
    "image": "registry.fly.io/tunr-a-x7k2:dep_9f2c",
    "guest": { "cpu_kind": "shared", "cpus": 1, "memory_mb": 256 },
    "env":   { "PORT": "8080", "TUNR_APP": "sprint", …kullanıcı env'leri… },
    "services": [{
      "protocol": "tcp",
      "internal_port": 8080,
      "autostop": "suspend",
      "autostart": true,
      "min_machines_running": 0,
      "ports": [{ "port": 80, "handlers": ["http"] }],
      "concurrency": { "type": "requests", "soft_limit": 20, "hard_limit": 50 }
    }],
    "checks": {
      "alive": { "type": "tcp", "port": 8080, "interval": "10s", "timeout": "2s", "grace_period": "20s" }
    }
  }
}
```

Notlar: `suspend` bellek anlık görüntüsüyle hızlı dönüş sağlar (soğuk `stop`'a göre belirgin fark; suspend'in bazı yapılandırma sınırları var — §14 doğrulama listesinde; sınır yiyen app'te CP otomatik `stop`'a düşer). Durmuş/askıdaki makinede CPU/RAM faturalanmaz; kalıcı maliyet ≈ rootfs deposu → app başına ayda sentler, raporun "boşta app ≈ bedava" ekonomisinin v0 kanıtı. Uzun süreli işler (arka plan job'ı) suspend ile kesilebilir — v0 sınırı olarak dokümante edilir.

### 7.3 Güncelleme ve sağlık

Sonraki deploy'lar: `POST /v1/apps/{app}/machines/{id}` ile **in-place image update** (makine yeni image ile yeniden başlar; 3–10 sn pencere → freeze önbelleği kapatır, §5). CP, machine `state=started` + check `passing` olana dek Machines API'den bekler (`/wait` ucu + check poll, tavan 90 sn), sonra `routes` satırını yazar/NOTIFY eder ve deployment'ı `healthy` işaretler. Route **ancak sağlık sonrası** yazıldığı için ilk deploy'da kullanıcı asla yarı-canlı app görmez.

---

## 8. Bileşen 6 — `tunr-shim` (önerilen, küçük ama çok işlevli)

~2 MB statik Go binary'si; build sırasında buildd image'a ekler ve start komutunu sarar: `ENTRYPOINT ["/tunr-shim","--"] CMD [orijinal start]`. Shim `0.0.0.0:8080` dinler, çocuk süreci `PORT=8081`'de başlatır ve şunları yapar:

1. **Edge doğrulaması:** `X-Tunr-Edge` HMAC'i `TUNR_EDGE_SECRET` ile doğrular; geçersizse 403. Bu, paylaşılan org ağındaki (§10) "komşu app benim Flycast'ime istek atar" açığını v0'da fiilen kapatır ve yarının Gate'i için doğrulama noktasını şimdiden kurar.
2. **Log yakalama:** çocuğun stdout/stderr'ini halka arabelleğe alır ve CP'ye WebSocket ile akıtır → `tunr logs` v0'da build + HTTP loglarına ek olarak runtime logu da kazanır (Fly'ın log API'sine bağımlılık olmadan).
3. **Hazırlık kapısı:** çocuk portu açılana dek gelen isteği kısa süre bekletir (soğuk başlangıçta 502 yerine 1–2 sn gecikme).

Zaman daralırsa kesme sırası: önce (2) log akışı, sonra shim'in tamamı (o durumda çocuk doğrudan 8080 dinler ve §10 riski "bilinen açık" olarak kalır). Wrap mantığı bu ekip için yarım günlük iş; v0'a girmesi şiddetle önerilir.

---

## 9. MCP Araçları (kullanıcının "MCP ile yayınla" sezgisinin v0'ı)

MCP sunucusu zaten CLI süreci; araçlar CLI ile aynı iç fonksiyonları çağırır:

```json
{ "name": "deploy_app",
  "description": "Build & deploy the current project to tunr; returns a live URL.",
  "input_schema": { "type":"object", "properties": {
      "path": {"type":"string","description":"project dir; default cwd"},
      "name": {"type":"string"}, "env": {"type":"object"} } } }
```

Ek araçlar (her biri ince sarmalayıcı): `list_apps`, `get_deploy_status(deployment_id)`, `get_logs(name, kind: build|http|runtime)`, `delete_app(name)`, `rollback(name, to_seq?)`. Dönüşler CLI `--json` olaylarıyla birebir aynı şema — tek doğruluk kaynağı. Demo cümlesi: Claude Code'a "*bunu yayınla*" → ajan `deploy_app` çağırır → URL döner.

---

## 10. Güvenlik — v0 Dürüst Durum Tespiti

**Bilinen ve kabul edilen açık: paylaşılan 6PN.** v0'da tüm app'ler tek Fly ağında (`tunr-shared`). Fly'da app'ler kendi `network`'üne konabilir ama o zaman relay o ağlara erişemez (relay tek ağda yaşar); ağ-başına-app izolasyonu, relay'in her ağa uzanmasını gerektirir ve bu, Faz-2'deki "kendi runner filomuz" işinin ta kendisidir. v0 kararı: paylaşılan ağ + **shim HMAC'i** (uygulama katmanında komşu erişimini keser) + makine başına düşük kotalar. Egress açık (Fly'da default-deny pratik değil) — İzin Manifesti vizyonunun v0'da OLMADIĞI landing'de iddia edilmez.

**Diğerleri:** Fly org token + registry kimliği + `edge_secret` üretimi yalnız CP'de; env'ler Postgres'te CP anahtarıyla mühürlü (libsodium sealed box), yalnız machine-create anında çözülür; kötüye kullanım v0 frenleri — deploy için hesap şart (mevcut login), free hesapta 3 app / günde 10 deploy / app başına 256 MB RAM + shared CPU, `tunr.sh` subdomain'i zaten "kullanıcı içeriği" ayrımı sağlıyor; şüpheli imaj taraması v0'da yok (bilinçli).

---

## 11. Deploy Yaşam Döngüsü — Uçtan Uca Sıra

```
CLI/MCP        CP                buildd             Fly API           Relay
  │ pack+POST ─►│                                                        
  │             │ deployment=queued; builder'ı start et                  
  │             │── tar+meta ────►│ nixpacks/docker build               
  │◄── SSE ─────│◄── log akışı ───│ docker push → pushed                 
  │             │ (ilk deploy'da) app+flycast yarat ──►│                 
  │             │ machine create/update(image) ───────►│                 
  │             │ wait started + check passing ◄───────│                 
  │             │ routes UPSERT (kind=cloud, cloud_url) + NOTIFY ───────►│ cache güncel
  │◄─ healthy ──│ deployment=healthy, apps.status=live                   
  🚀 URL bas
```

Durum makinesi `queued→building→pushing→releasing→healthy|failed`; her geçiş SSE olayı ve Postgres'e yazım (CLI koparsa `tunr apps list` durumu gösterir). **Rollback (v0.1, yarım gün):** `to_seq`'in `image_ref`'i ile §7.3 akışı — build yok, ~10 sn; demoda "ajan bozdu → `tunr rollback` → düzeldi" anlatısı çok satar.

---

## 12. Hata Modları ve Kullanıcı Mesajları

| Durum | Tespit | CLI mesajı (aynen bu netlikte) |
|---|---|---|
| Build başarısız | nixpacks/docker çıkış kodu | Son 30 log satırı + "Node için `package.json`'da `start` script'i gerekir / Python için köke `Procfile` ekleyin: `web: uvicorn main:app --host 0.0.0.0 --port $PORT`" |
| Health timeout | 90 sn'de check passing yok | "App başladı ama :8080 dinlemiyor. `PORT` env'ini kullanın ya da `--port` verin." + son runtime logları (shim varsa) |
| İsim çakışması | 409 | "`sprint` alınmış. `--name sprint-acme` deneyin." |
| Arşiv > 50 MB | CLI'da yerel | "Yükleme 50 MB sınırını aşıyor; `.tunrignore` ile büyük dizinleri dışlayın." |
| Uyanma gecikmesi | ilk bayt > ~5 sn | (kullanıcıya değil, metrike) — v0.1: relay'de "Waking your app…" ara sayfası |
| `.env` tespit | CLI'da yerel | "`.env` yüklenmedi (bilerek). Değişkenler için: `tunr deploy --env KEY=VAL`" |

---

## 13. Üç Günlük Uygulama Planı ve Kesme Listesi

**Gün 1 — İzleyici mermi (tracer bullet):** `routes` tablosu + `CloudUpstream` + LISTEN/NOTIFY; elle Fly'da açılmış hazır bir hello-world makinesine subdomain bağla ve `https://x.tunr.sh`'in relay→flycast→uyandır zincirini uçtan uca kanıtla (build yokken!). CP'de `POST /apps` + Machines API ile app/makine yaratımı. *Gün sonu ölçütü: uyuyan makine, tarayıcı isteğiyle uyanıp relay'den servis veriyor.*

**Gün 2 — Build hattı:** builder image (dockerd+nixpacks+buildd), registry push, SSE log akışı, CLI `deploy` paketleme+yükleme+akış; PORT konvansiyonu; Next.js ve FastAPI şablonlarıyla mutlu yol yeşil. *Ölçüt: boş dizinden URL'ye tek komut.*

**Gün 3 — Cila + MCP + demo:** shim (wrap + edge HMAC; log akışı yetişirse), `--env`, `apps list|delete`, MCP `deploy_app`+`list_apps`, hata mesajları (§12), demo videosu çekimi.

**Kesme sırası (kayarsa):** shim log akışı → shim tamamı → `apps logs` → rollback → `--no-cache`. **Kesilemez çekirdek:** deploy→URL, uyu/uyan, MCP `deploy_app`, tek şablonda (Next.js) kusursuz demo.

---

## 14. Doğrulanacaklar (kodlamadan önce 1 saatlik kontrol — Fly API'leri oynak)

Flycast private-IPv6 tahsisinin güncel yolu (Machines REST'e eklendi mi, yoksa GraphQL/flyctl mi); Machines API'de `services[].autostop/autostart/min_machines_running` alan adlarının güncel imlası ve `suspend`'in geçerli sınırları (hangi guest/volume yapılandırmalarında reddedilir → CP'nin `stop` fallback'i); `registry.fly.io` push için deploy-scope'lu token üretimi; app-yaratımda `network` parametresinin Machines API'deki durumu; suspend→resume gerçek gecikmesi ams'te (hedef < 1 sn, kabul < 3 sn — demo öncesi ölç); Fly Proxy'nin uyandırma sırasında isteği bekletme tavanı (bizim 30 sn `wake_timeout`'un altında kaldığını teyit et).

## 15. Demo Kabul Testi (video çekiminden önce yeşil olacaklar)

Next.js app: deploy → URL 60 sn altı; FastAPI app: Procfile reçetesiyle deploy; yeniden deploy: kesinti hissi yok (freeze devrede); 10 dk bekle → makine `suspended` → istek → yanıt < 3 sn; **laptop'u kapat → app yanıt vermeye devam ediyor** (videonun final karesi); MCP: Claude Code'dan "publish this" → URL; `tunr apps delete` → 404 + Fly kaynakları temiz.

---

*Bu doküman v0 içindir; §5'teki route şeması ve §8'deki shim, sonraki fazların (detach, Gate, runtime logları, izolasyon) kancalarını bugünden bırakır — v0'da yazılan hiçbir satır Faz 1–2'de çöpe gitmez.*
