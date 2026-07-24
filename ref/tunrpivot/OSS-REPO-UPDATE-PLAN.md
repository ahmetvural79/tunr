# tunr Open-Source Repo — Pivot Sonrası Güncelleme Planı

*Hazırlanma: 2026-07-23. Bağlam: pivot özellikleri (`pivot/cloud` branch) çalışır ve prod'a (91.98.42.7) deploy edilmiş durumda. Bu doküman, public repo `github.com/ahmetvural79/tunr`'un pivota göre nasıl düzenleneceğini planlar. Uygulama: özellikler (tracer bullet + deploy uçtan uca) valide olduktan sonra.*

---

## 0. Mevcut repo topolojisi (ne public, ne değil)

| Alan | Durum | Not |
|---|---|---|
| `cmd/tunr/`, `internal/` (CLI) | **public** | PolyForm Shield 1.0.0 |
| `relay/` | **public** | pivot kodu dahil (cloud upstream, runner, buildd, control-plane) |
| `sdk/` | **public** | Python/Node/Go/JS |
| `landing/` | **gitignore** | dashboard + marketing; ayrı **private** repo `tunr-dashboard` |
| `scripts/` | **gitignore** | ops scriptleri (server-setup.sh dahil) |
| `relay/migrations/` | **gitignore** | şema (001/002/003) — self-host için gerekli ama şu an gizli |
| `.env*`, server compose, Caddyfile | **gitignore / server-local** | secret + hosted infra |

Pivot kodu `pivot/cloud`'da commit'li: `9bd7102` (relay cloud upstream), `94adee1` (buildd), `a4927e9` (deploy scriptleri).

---

## 1. Kararlar

### K-A. Open-core sınırı (lisans K7 kesinleşiyor)
- **Açık (permissive'e geç):** CLI (`deploy`/`apps`/`mcp` dahil), relay çekirdeği (tunnel + **cloud upstream** + runner `Driver` + `DockerDriver` + `buildd`), SDK'lar.
  - Öneri lisans: **CLI + SDK → MIT/Apache-2.0** (dağıtımı maksimize et, ajan ekosistemine gir); **relay → FSL-1.1-Apache-2.0** (Functional Source License: 2 yıl sonra Apache'ye döner, o zamana dek "rakip tünel/bulut servisi" kurmayı engeller — Sentry/Sourcegraph modeli). Alternatif: mevcut PolyForm Shield'i koru.
- **Kapalı/proprietary kalır:** dashboard (`tunr-dashboard`), hosted control-plane secret'ları + Paddle/billing, sunucu-yerel compose/Caddyfile/.env, marketing içeriği, kullanım/abuse itibar sistemleri.

### K-B. Migration'ları publish et
`relay/migrations/` gitignore'dan çıkar → repoya al (001/002/003). **Gerekçe:** relay açık kaynaksa şeması da açık olmalı; aksi halde self-host bozuk (tablolar yaratılamaz). Şema secret değil. `.gitignore` satır 62 kaldırılır.

### K-C. server-setup.sh'i publish et
`scripts/server-setup.sh` (gVisor + Docker + network kurulumu) genel bir reçete — self-host cloud-runner için gerekli. `scripts/`'i seçici publish et (server-setup.sh + update.sh public; e2e/ ops scriptleri kalabilir).

### K-D. Dashboard Dockerfile'ı repoya al
Dashboard `Dockerfile` şu an yalnız sunucuda (server-local, 172 byte). `tunr-dashboard` reposuna commit'le ki yeniden kurulum tekrarlanabilir olsun.

---

## 2. `pivot/cloud` → `main` merge

1. Ön koşul: tracer bullet (#8) + `tunr deploy` uçtan uca yeşil.
2. PR: `pivot/cloud` → `main`. CI iki modülü de lint+test ediyor (`.github/workflows/ci.yml`) — geçmeli.
3. Versiyon: **v0.5.0** (deploy = minor atlama). `cmd/tunr/main.go` Version, `sdk/python/pyproject.toml`, `sdk/node/package.json` senkron.
4. `CHANGELOG.md`: "Cloud deploy (preview): `tunr deploy`, wake-on-request, MCP `deploy_app`; relay cloud upstream + runner."

---

## 3. Doküman güncellemesi (public)

- **README.md:** yeni tek cümle ("cloud for small software"); deploy/share/MCP anlatısı; tünel = on-ramp; landing ile tutarlı hero.
- **docs/ARCHITECTURE.md:** cloud upstream + runner `Driver` + control-plane + route store + LISTEN/NOTIFY diyagramı.
- **Yeni `docs/CLOUD_RUNNER.md`:** own-server + gVisor izolasyon modeli, `server-setup.sh`, `tunr-apps` ağı, relay'in docker-erişimi (soket+CLI / sidecar), scale-to-zero (pause→stop).
- **Yeni `docs/DEPLOY.md`:** `tunr deploy` akışı, `.tunr.json` `app` bloğu, `apps list/delete/logs`, hata modları.
- **`.tunr.schema.json`:** `app` bloğu (name/region/internal_port/env) eklenir.
- **MCP dokümanı:** `deploy_app`, `list_apps`, `get_logs`, `rollback`, `create_db`, `set_secret`, `set_share_policy`, `get_feedback`... araç listesi.
- **Ajan-yönelik dağıtım:** `llms.txt`, `AGENTS.md` şablonu, **"tunr skill"** (Claude Code plugin) — ajan deploy+share desenini kendiliğinden kursun.

---

## 4. Güvenlik / hijyen (merge öncesi)

- **Secret sızıntısı:** `landing/firebase-key.json` + `.env.local.example` içindeki canlı Firebase anahtarını **temizle + rotate et** (bkz. hafıza `firebase-secret-exposed`). landing/ zaten gitignore ama rotasyon şart.
- Mevcut güvenlik sertleştirmelerini koru (magic-link gating, WS origin, rate-limit cap, JWT HS256, Paddle HMAC, keychain).
- Migration publish edilince: içinde secret olmadığını teyit et (yalnız şema).

---

## 5. Sıralama

**Faz A — özellikler biter bitmez:**
`pivot/cloud`→`main` merge · `relay/migrations/` publish · README + ARCHITECTURE + DEPLOY + CLOUD_RUNNER · `.tunr.schema.json` app bloğu · v0.5.0 + CHANGELOG.

**Faz B — dağıtım + lisans:**
Lisans geçişi (open-core: CLI/SDK MIT/Apache, relay FSL) · `llms.txt` + `AGENTS.md` + tunr skill · server-setup.sh publish · self-host cloud-runner rehberi · Firebase secret rotasyonu.

**Faz C — ekosistem:**
MCP registry kaydı · Cursor rules · "Claude Code ile yaz, tunr'la yaşat" içerikleri.

---

*Not: Bu plan repo'yu "açık CLI + açık relay çekirdeği, kapalı dashboard/control-plane" open-core konumuna taşır — hem self-host'u onarır (migrations + setup script public) hem de ticari savunmayı (relay FSL + kapalı hosted katman) korur.*
