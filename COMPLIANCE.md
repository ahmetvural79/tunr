# Uyumluluk ve Veri Güvenliği (Compliance)

> **tunr v1.0** | tunr.sh | Güncel: Mart 2026

Bu doküman tunr'nun veri işleme pratiklerini, güvenlik kontrollerini ve uyumluluk durumunu açıklar. Open source araçlar için şeffaflık çok önemli.

---

## Veri Sınıflandırması

| Kategori | Örnekler | Koruma Seviyesi |
|----------|----------|-----------------|
| **PII (Kişisel)** | E-posta adresi | SHA-256 hash'li, şifreli iletim, 90 gün sonra anonimleştir |
| **Kimlik Bilgisi** | JWT token, auth token | OS keychain, belleğe kısa süre alım, log'a asla |
| **Tunnel Trafiği** | HTTP istek/yanıt gövdesi | uçtan uca şifreli (TLS 1.3), relay'de saklanmaz |
| **Meta Veri** | URL, method, status code | 30 gün (Pro), 1 gün (Free), PostgreSQL |
| **Fatura** | Plan bilgisi | Paddle MoR — kart numaraları tunr'ya ulaşmaz |
| **Log** | Sistema event'ları | 90 gün, PII kaldırılmış |

---

## Veri İşleme Prensipleri

### Veri Minimizasyonu
- Tunnel içeriği relay sunucuda **saklanmaz** — sadece geçici hafıza tamponu
- İstek body'leri inspector buffer'da max **5MB** ile kırpılır (varsayılan)
- Kullanıcı e-posta adresi dışında kişisel veri toplanmaz

### Veri Silme
- **Free:** 24 saat sonra tüm istek logları silinir
- **Pro:** 30 gün
- **Team:** 90 gün
- Hesap kapatma: 30 gün içinde tüm veri tamamen silinir (GDPR Art. 17)

### Veri Konumu
- Relay sunucu: Fly.io Amsterdam (AMS) — AB GDPR kapsamında
- PostgreSQL: Fly.io managed Postgres (aynı bölge)
- Kullanıcı verisi AB sınırları dışına aktarılmaz

---

## Erişim Kontrolü

### Kimlik Doğrulama
- Magic link (email) — şifre yok, credential stuffing riski yok
- JWT token: 24 saat TTL, OS keychain'de saklanır
- MFA: Yol haritasında (Faz 7)

### Yetki Modeli
| Kaynak | Erişim |
|--------|--------|
| Kendi tunnel'ları | Tam erişim |
| Başkasının tunnel | Erişim yok (registry isolation) |
| Inspector kayıtları | Sadece kendi tunnel'ları |
| Billing bilgileri | Sadece hesap sahibi |
| Admin API | Yok (open source — admin yoktur) |

### Header Sanitizasyonu
Şu header'lar inspector kayıtlarında `[REDACTED]` olarak gösterilir:
- `Authorization`, `Cookie`, `Set-Cookie`
- `X-Api-Key`, `X-Auth-Token`, `X-Secret`
- `Proxy-Authorization`

---

## Güvenlik Kontrolleri

### TLS/Şifreleme
- Tüm relay trafiği TLS 1.3 (Caddy/Fly.io)
- HSTS: 1 yıl + preload
- CLI ↔ Relay: `InsecureSkipVerify: false` (test tarafından doğrulanmış)
- Wildcard TLS: Cloudflare DNS challenge (Let's Encrypt)

### Webhook Güvenliği
- Paddle webhook: HMAC-SHA256 imza doğrulama
- Replay attack koruması: 5 dakika timestamp window
- Timing-safe karşılaştırma (`hmac.Equal`)

### Ağ Güvenliği
- Dashboard: `127.0.0.1` only (dışarıya açmaz)
- Replay: Sadece localhost hedefi (SSRF koruması)
- Private IP aralıkları bloklanmış (10.x, 172.16-31.x, 192.168.x)

---

## Olay Müdahale

### Güvenlik Açığı Bildirimi
Bkz. [SECURITY.md](./SECURITY.md)

### Olay Süreci
1. Tespit → 1 saat içinde iç bildirim
2. Etki analizi → 4 saat içinde
3. Geçici çözüm → 24 saat içinde
4. Kalıcı düzeltme → 72 saat içinde
5. Kullanıcı bildirimi → etki varsa 72 saat içinde (GDPR Art. 33/34)
6. Post-mortem → 1 hafta içinde (herkese açık)

---

## Üçüncü Taraf Bağımlılıkları

| Hizmet | Amaç | Veri Erişimi | Sözleşme |
|--------|------|-------------|----------|
| Paddle | Ödeme işlemi | Plan, fatura bilgileri | Merchant of Record |
| Fly.io | Relay hosting | Şifreli trafik meta verisi | EU SCC |
| Cloudflare | DNS + TLS | DNS sorguları | EU SCC |
| Let's Encrypt | TLS sertifika | Domain bilgisi | Açık CA |

Üçüncü taraflarla **kullanıcı e-postası veya tunnel içeriği paylaşılmaz.**

---

## SOC 2 Yol Haritası

| Kontrol | Durum | Hedef Tarih |
|---------|-------|-------------|
| CC1: Control Environment | 🟡 Kısmi | Q3 2026 |
| CC2: Communication | 🟢 Tamamlandı | — |
| CC3: Risk Assessment | 🟡 Kısmi | Q3 2026 |
| CC6: Logical Access Controls | 🟢 Tamamlandı | — |
| CC7: System Operations | 🟡 Kısmi | Q4 2026 |
| CC8: Change Management | 🟢 Tamamlandı (CI/CD) | — |
| CC9: Risk Mitigation | 🟡 Kısmi | Q4 2026 |
| A1: Availability | 🔴 Planlandı | Q4 2026 |

**Hedef:** SOC 2 Type I — Q4 2026 | SOC 2 Type II — Q2 2027
