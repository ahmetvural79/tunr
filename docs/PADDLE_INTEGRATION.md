# Paddle entegrasyonu — repo özeti

Bu belge, **tunr** deposunda Paddle (Paddle Billing) ile ilgili tüm parçaları, ortam değişkenlerini ve veri akışını tek yerde toplar. İki farklı backend hattı vardır: **Next.js (landing dashboard)** ve **Go relay sunucusu**. Üretimde Paddle webhook URL’sini yalnızca bir hedefe yönlendirmeniz gerekir; aksi halde aynı olayı iki kez işleyebilirsiniz.

---

## 1. Mimari özeti

| Bölüm | Rol | Webhook yolu | Plan güncellemesi |
|--------|-----|----------------|-------------------|
| **landing/app** (Next.js) | Dashboard’da Paddle.js checkout; Postgres’te `subscriptions` + `users.plan` | `POST /api/webhooks/paddle` | `subscription.*` olaylarında `users.plan` → `pro` / `free` |
| **relay** (Go) | Tünel/auth API’si; kullanıcıda `paddle_customer_id` | `POST /webhook/paddle` | `PaddleWebhookHandler`: price/product eşlemesi ile `pro` / `team` / `free` |
| **internal/billing + internal/api** (Go) | CLI inspector API’si için `PaddleClient` | `POST /webhook/paddle` (handler hazır) | Webhook’ta şu an yalnızca log; DB güncellemesi **TODO** |

---

## 2. Next.js — `landing/app`

### 2.1 Ortam değişkenleri

**Örnek dosya:** `landing/app/.env.local.example`

| Değişken | Kullanım |
|----------|-----------|
| `NEXT_PUBLIC_PADDLE_CLIENT_TOKEN` | Paddle.js v2 `Initialize({ token })` — istemci tarafı (public). |
| `NEXT_PUBLIC_PADDLE_ENV` | `sandbox` ise `Paddle.Environment.set('sandbox')`. |
| `NEXT_PUBLIC_PADDLE_PRICE_ID_MONTHLY` | Aylık fiyat ID (`pri_...`). |
| `NEXT_PUBLIC_PADDLE_PRICE_ID_YEARLY` | Yıllık fiyat ID. |
| `NEXT_PUBLIC_PADDLE_PRICE_ID` | Geriye dönük uyumluluk: aylık yoksa fallback. |
| `NEXT_PUBLIC_PADDLE_PRICE_ID_ANNUAL` | Kodda yıllık için alternatif fallback ismi. |
| `PADDLE_WEBHOOK_SECRET` | Sunucu tarafı webhook imza doğrulaması (`route.ts`). |
| `DATABASE_URL` | Postgres (`lib/db.ts`); webhook ve `users` güncellemeleri için. |

**Not:** `.env.local.example` içinde Paddle satırları çoğunlukla yorum satırıdır; gerçek değerler `.env.local` içinde verilir.

### 2.2 Faturalandırma sayfası (Paddle.js checkout)

**Dosya:** `landing/app/app/dashboard/settings/billing/page.tsx`

- `https://cdn.paddle.com/paddle/v2/paddle.js` script’i dinamik yüklenir.
- `NEXT_PUBLIC_PADDLE_CLIENT_TOKEN` yoksa script yüklenmez; `isPaddleReady` false kalır.
- `Paddle.Initialize({ token, eventCallback })`: `checkout.completed` sonrası checkout kapatılır, kısa süreli poll ile `/api/me` üzerinden `isPro` kontrol edilir (webhook gecikmesine karşı).
- `Paddle.Checkout.open({ items: [{ priceId, quantity: 1 }], customer: { email }, customData: { email } })`.
- Aylık/yıllık: `NEXT_PUBLIC_PADDLE_PRICE_ID_MONTHLY` / `NEXT_PUBLIC_PADDLE_PRICE_ID_YEARLY` (ve fallback env’ler).

### 2.3 Webhook API route

**Dosya:** `landing/app/app/api/webhooks/paddle/route.ts`

- **İmza:** `PADDLE_WEBHOOK_SECRET` ile HMAC-SHA256; payload `ts:rawBody` formatında; `timingSafeEqual` ile `h1` karşılaştırması.
- **`NODE_ENV === 'production'`** iken imza zorunlu; geliştirmede imza kontrolü atlanabilir.
- **Olaylar:**
  - `subscription.created`, `subscription.updated`: `subscriptions` tablosuna `INSERT ... ON CONFLICT (paddle_subscription_id) DO UPDATE`; ödeme durumu `active`/`trialing` ise `users.plan = 'pro'`, değilse `users.plan = 'free'` (e-posta çözümlendiyse).
  - `subscription.canceled`: `subscriptions` güncelleme + varsa `users.plan = 'free'`.
  - `subscription.past_due`: yalnızca `subscriptions.status`.
- **E-posta çözümü:** Önce webhook içindeki `custom_data.email` / `customer.email`; yoksa `subscriptions` tablosundan `paddle_customer_id` ile son kayıt.

**Çıkarım — `subscriptions` tablosu alanları (koddan):**  
`paddle_subscription_id` (unique), `paddle_customer_id`, `user_email`, `status`, `plan_price_id`, `current_period_end`, `updated_at` (ve muhtemelen `created_at`; INSERT’te belirtilmeyen sütunlar DB default’una bağlıdır).

### 2.4 Plan bilgisi (dashboard)

**Dosya:** `landing/app/app/api/me/route.ts`  
`getSessionUserWithPlan()` → `users.plan`; `isPro = plan === 'pro' || plan === 'team'`.

**Dosya:** `landing/app/lib/auth.ts`  
Oturum JWT’si plan taşımaz; plan her seferinde `users` tablosundan okunur (Paddle sonrası güncel plan görünür).

### 2.5 Veritabanı bağlantısı

**Dosya:** `landing/app/lib/db.ts` — `pg` `Pool`, `DATABASE_URL`.

---

## 3. Go relay sunucusu — `relay/cmd/server`

### 3.1 Ortam değişkenleri

| Değişken | Varsayılan | Açıklama |
|----------|------------|----------|
| `PADDLE_WEBHOOK_SECRET` | *(boş)* | Boşsa `/webhook/paddle` **kaydı yapılmaz**. |
| `PADDLE_PRO_PRICE_ID` | *(boş)* | Pro eşlemesi (Paddle price id). |
| `PADDLE_TEAM_PRICE_ID` | *(boş)* | Team eşlemesi. |
| `PADDLE_PRO_PRODUCT_ID` | *(boş)* | Pro ürün id fallback. |
| `PADDLE_TEAM_PRODUCT_ID` | *(boş)* | Team ürün id fallback. |
| `PADDLE_DEFAULT_PAID_PLAN` | `pro` | Satır kalemlerinden plan çıkarılamazsa kullanılan plan. |

Relay ayrıca `DATABASE_URL`, `TUNR_JWT_SECRET`, `TUNR_DOMAIN`, `PORT` vb. kullanır (Paddle dışı).

### 3.2 Webhook işleyici

**Dosya:** `relay/internal/relay/paddle_webhook.go`

- İmza doğrulama: `Paddle-Signature` (`ts` + `h1`), 5 dakika zaman penceresi, HMAC-SHA256.
- **Olaylar:** `subscription.activated`, `subscription.updated`, `subscription.canceled`, `subscription.paused`, `subscription.past_due`, `transaction.completed`.
- Plan çözümü: önce `TeamPriceID` / `TeamProductID`, sonra `ProPriceID` / `ProProductID`, yoksa `DefaultPaidPlan`.
- **DB:** `relay/internal/db/db.go` — `users.paddle_customer_id`, `UpdateUserPlanByCustomerID`, `LinkPaddleCustomerByEmail`.

### 3.3 JWT ve plan

**Dosya:** `relay/internal/auth/jwt.go` — `Claims` içinde `plan` alanı var; yorumda Paddle webhook sonrası yeni token verilmesi gerektiği belirtilir (plan değişince eski JWT’deki plan eski kalabilir).

---

## 4. Go — `internal/billing/paddle.go` (ortak kütüphane)

Paddle Billing API v1 (`Paddle-Version: 1`):

- **Base URL:** sandbox `https://sandbox-api.paddle.com`, prod `https://api.paddle.com`.
- **Metodlar:** `GetSubscription`, `IsPro`, `GetLimits` (ürün eşlemesinde şu an sabit `PlanPro` notu — “Phase 3” yorumu), `VerifyWebhookSignature`, `HandleWebhook` (log + TODO’lar), `CreateCheckoutSession` (`POST /transactions`).

**Testler:** `internal/billing/paddle_test.go` — imza doğrulama, replay süresi, `GetLimits` fallback.

Bu paket relay tarafında doğrudan webhook için kullanılmıyor; relay kendi `paddle_webhook.go` ile çalışıyor.

---

## 5. Go — `internal/api/server.go` (inspector API)

- `POST /webhook/paddle` → `billing.PaddleClient.HandleWebhook` (yapılandırılmışsa).
- Paddle client yoksa 200 + `{"status":"ignored"}`.
- **Not:** Depoda `cmd/tunr` veya başka bir giriş noktası bu `api` paketini import ederek sunucuyu başlatmıyor gibi görünüyor; pratikte **kullanılmayan / ileride bağlanacak** bir parça olabilir.

---

## 6. Diğer dosyalar

| Dosya | İçerik |
|--------|--------|
| `relay/caddy/Caddyfile` | Landing için CSP: `https://cdn.paddle.com` (script + default-src). |
| `sdk/js/tunr.js` | JSDoc örnekte “Stripe/Paddle webhook” ifadesi (SDK test yardımcısı; gerçek entegrasyon değil). |
| `.gitignore` | `paddle.json` (muhtemel yerel Paddle yapılandırma / çıktı). |

---

## 7. Önemli farklar ve dikkat noktaları

1. **İki webhook implementasyonu:** Next route’u `subscription.created` kullanır; Go relay `subscription.activated` dinler. Paddle Billing olay adları hesabınıza göre doğrulanmalıdır.
2. **Veri modeli:** Landing tarafı `subscriptions` + `users.plan` (ve webhook’ta `paddle_customer_id` eşlemesi subscriptions üzerinden); relay tarafı `users.paddle_customer_id` doğrudan.
3. **Team planı:** Next billing UI ve webhook akışı örnekleerde çoğunlukla **pro/free** odaklı; relay `team` planını da eşleyebilir.
4. **Güvenlik:** Production’da Next webhook’ta imza zorunlu; relay’de her zaman imza zorunlu.

---

## 8. Dosya listesi (Paddle ile ilişkili)

- `landing/app/.env.local.example`
- `landing/app/app/dashboard/settings/billing/page.tsx`
- `landing/app/app/api/webhooks/paddle/route.ts`
- `landing/app/app/api/me/route.ts`
- `landing/app/lib/db.ts`
- `landing/app/lib/auth.ts`
- `relay/cmd/server/main.go`
- `relay/internal/relay/paddle_webhook.go`
- `relay/internal/db/db.go`
- `relay/internal/auth/jwt.go`
- `relay/caddy/Caddyfile`
- `internal/billing/paddle.go`
- `internal/billing/paddle_test.go`
- `internal/api/server.go`
- `sdk/js/tunr.js` (yalnızca dokümantasyon yorumu)

---

*Bu dosya kod tabanı taramasıyla üretilmiştir; Postgres şema dosyası repoda aranmadıysa `subscriptions` / `users` sütunları tam DDL ile doğrulanmalıdır.*
