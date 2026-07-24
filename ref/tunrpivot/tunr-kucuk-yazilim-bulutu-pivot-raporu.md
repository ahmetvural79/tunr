# tunr → Küçük Yazılım Bulutu
## YC "A Cloud for Small Software" RFS'ine Uyum İçin Pivot Raporu ve Planı

*Hazırlanma tarihi: 22 Temmuz 2026*

---

## 0. Yönetici Özeti

Bu raporun tek cümlelik tezi şu: **tunr, farkında olmadan zaten bir "küçük yazılım bulutu"nun ön kapısını inşa etmiş durumda; eksik olan şey arka taraf — yani uygulamanın laptop kapandıktan sonra da yaşamaya devam ettiği, verisiyle birlikte paylaşılabildiği ve bir Google Dokümanı gibi yetkilendirilebildiği katman.**

Pivot, tünel ürününü çöpe atmak değil; tüneli huninin tepesine koyup altına üç katman eklemek: (1) kalıcı çalıştırma (compute + veri), (2) kimlik ve paylaşım (auth, roller, davet), (3) ajan-yerel kontrol düzlemi (MCP + API). Mevcut özelliklerin neredeyse tamamı — freeze mode, demo mode, auto-login, inject-widget, relay, MCP — bu üç katmanın ilkel (primitive) versiyonları. Bu, "sıfırdan pivot" değil, "yarıda kalmış bir ürünü tamamlama" hikâyesi. YC başvurusunda anlatılacak founder-insight tam olarak bu.

**Acil zamanlama notu:** Alıntıladığınız RFS, YC'nin Fall 2026 batch RFS'i. Fall 2026 için zamanında başvuru son tarihi **27 Temmuz 2026, 20:00 PT** — yani bu rapor yazıldığı anda **5 gün** var. Zamanında başvuranlara 28 Ağustos'a kadar karar dönülüyor; geç başvurular da değerlendiriliyor ama tarih garantisi yok. Bu nedenle raporun sonunda 7 günlük değil, fiilen 5 günlük bir eylem planı var: bu hafta yapılacak şey ürünü bitirmek değil, tezi netleştirmek + 60 saniyelik "sihirli demo"yu kaydetmek + başvuruyu göndermek.

---

## 1. Mevcut Durum Analizi: tunr Bugün Ne?

### 1.1 Ürünün özeti

tunr, Go ile yazılmış tek binary bir CLI + relay altyapısı. Temel değer önerisi: "localhost'u 3 saniyede internete aç." HTTP/HTTPS + WebSocket, TCP, UDP ve TLS (E2E) tünelleri; çoklu bölge (ams / sea / sin); path routing; parola, bearer token, IP allowlist; TTL; QR; header manipülasyonu; HTTP inspector + replay; Python/Node SDK'ları; Docker ve docker-compose ile self-hosting (relay + Caddy + Postgres); Prometheus metrikleri; systemd servisi; `.tunr.json` ile çoklu tünel; ve **`tunr mcp`** ile Claude/Cursor/Windsurf entegrasyonu.

Bunların üstünde, kategoride kimsede olmayan dört "vibecoder süper gücü" var: **Freeze Mode** (sunucu çökerse son başarılı yanıtı bellekten servis et), **Demo Mode** (POST/PUT/DELETE'i proxy'de kes, salt-okunur demo), **Feedback Widget Injection** (HTML'e Marker.io tarzı görsel geri bildirim katmanı enjekte et), **Auto-Login Bypass** (demo hesabına otomatik giriş için cookie/JWT enjeksiyonu).

Fiyatlandırma: Free ($0; 2 saat tünel, 1 eşzamanlı, rastgele subdomain) ve Pro ($5/ay; sınırsız süre, 10 tünel, rezerve subdomain, custom domain). Lisans: PolyForm Shield 1.0.0 (rakip ürün yapmak yasak, gerisi serbest). Ekip: 2 kişi. Traction: erken aşama (GitHub ~9 yıldız, 5 release, v0.4.1).

### 1.2 Dürüst değerlendirme: tünel pazarının gerçeği

Tünel pazarı kalabalık ve fiyat tabanı düşük: ngrok, Cloudflare Tunnel (ücretsiz), Pinggy, LocalXpose, localtunnel, bore, zrok, Tailscale Funnel... Ürün karşılaştırma tablosunda tunr'ın kazandığı satır çok; ama pazarın acı gerçeği şu ki "tünel" bir **özellik**, bir **şirket** değil. Cloudflare bunu bedavaya dağıtıyor; ngrok bu yüzden yukarı pazara (API gateway) kaçıyor. $5/aylık bir tünelle savunulabilir bir şirket kurmak çok zor; ama tünel, **muazzam bir dağıtım hunisi**: dev sunucusunu tünelleyen herkes, "deploy" komutundan tek adım uzakta duran, kanıtlanmış niyetli bir kullanıcı.

Asıl ilginç olan şu: tunr'ın farklılaştırıcı özelliklerinin hiçbiri aslında "tünelleme" özelliği değil. Freeze, demo, widget, auto-login — bunların hepsi **"yazılımı başka bir insana gösterme/paylaşma"** problemine ait. Ekip, kullanıcılarının gerçek derdinin paket taşımak değil *yazılım paylaşmak* olduğunu sezmiş ve proxy katmanına paylaşım özellikleri gömmüş. Bu sezgi, RFS'in tam kalbine denk geliyor.

---

## 2. RFS'in Derin Okuması: Pete Koomen Aslında Ne İstiyor?

RFS kısa ama her cümlesi bir gereksinim. Ayrıştıralım:

**(a) "Purpose-built tools that will only ever have one or a small handful of users."** Hedef segment tanımı: 1–20 kullanıcılı uygulamalar. Bu, bütün mimari ve ekonomik kararları belirliyor — bu uygulamalar zamanın %99'unda uykuda, trafik yoğunluğu sıfıra yakın, ama *sayıları* milyonlarca olacak. Klasik bulut ekonomisi (her app için ayakta duran bir servis) burada iflas eder; **scale-to-zero + istekte uyanma** zorunlu.

**(b) "Easy to build, but still hard to deploy and share."** Problem cümlesi deploy DEĞİL, **deploy + share**. Vercel'e deploy etmek zaten kolay; zor olan "iş arkadaşına, sadece onun görebileceği şekilde, giriş ekranı yazmadan vermek."

**(c) "Incumbent clouds… designed for Big Software… at the cost of complexity. A cloud designed for small software could delete most of this complexity."** Ürün felsefesi: özellik eklemek değil, **kavram silmek**. Hedef kullanıcının bilmemesi gereken kavramlar listesi: region, VPC, load balancer, IAM policy, Dockerfile, CI pipeline, DNS kaydı, SSL sertifikası, connection string. tunr'ın "zero config" kültürü buraya birebir taşınabilir.

**(d) "Every company will want to customize the environment this software runs in."** Kurumsal katman gereksinimi: şirket başına ortam şablonları — izinli egress domainleri, ortak secret'lar, SSO domaini, veri lokasyonu, iç ağa erişim. (Aşağıda göreceğiz: tunr'ın tünel teknolojisi bu maddede *ters yönde* kullanılarak benzersiz bir avantaja dönüşüyor.)

**(e) "Auth & permissions are hard."** RFS'in açıkça isimlendirdiği tek teknik zorluk. Çözümün uygulama *içinde* değil, uygulamanın *önünde* olması gerektiğine dair güçlü bir ipucu — çünkü ajanların yazdığı auth koduna güvenilemez ve teknik olmayan kullanıcı auth yazamaz.

**(f) "Allowing nontechnical users to share arbitrary code is tricky to do securely."** İki ayrı problem: (1) izolasyon (kötü/hatalı kod komşusuna, altyapıya, internete zarar vermesin), (2) **güven okunabilirliği** (paylaşılan uygulamayı açan kişi neye izin verdiğini anlayabilsin — telefonlardaki uygulama izinleri gibi).

**(g) "As easy to share with your colleagues as a Google Doc."** Kuzey yıldızı metafor. Google Doc paylaşımının bileşenleri: tek link; link davranışı seçimi (herkese açık / linki olan / belirli kişiler / şirket domaini); roller (görüntüleyici / yorumcu / düzenleyici); yorumlar; sürüm geçmişi; sahiplik devri. Bunların her birinin "uygulama" karşılığı tanımlanmalı — ve kritik fark: bir dokümanı paylaşmak *içeriğini* paylaşmaktır; bir uygulamayı paylaşmak da **kodu + verisi + yapılandırmasıyla** paylaşmak olmalı. (Bu içgörü bölüm 5'te "App Kapsülü" konseptine dönüşecek.)

**Bağlam sinyalleri:** Koomen, "AI Horseless Carriages" makalesinin yazarı ve YC içinde 350+ araçlık dahili ajan altyapısını kuran kişi — yani bu RFS teorik değil, kendi yaşadığı acıdan geliyor: YC'nin içinde ajanlarla onlarca küçük araç ürettiler ve bunları güvenle çalıştırıp paylaşacak yer bulmakta zorlandılar. Başvuruda bu diline konuşmak ("we built demo-mode and freeze-mode because our users' real problem was *sharing software*, not tunneling packets") çok güçlü olur.

---

## 3. Rekabet Haritası: Bu Yarışta Kimler Var, Boşluk Nerede?

| Oyuncu | Yaklaşım | Güçlü yanı | tunr'a açık kalan boşluk |
|---|---|---|---|
| **Val Town** | Tarayıcıda yaz, 100ms'de deploy; JS/TS + Deno; cron, e-posta, SQLite; Townie ajanı; branch/remix | En olgun "small software" ürünü; anti lock-in söylemi | **Sadece JS/TS**, kendi editörüne çekiyor; vibecoder'ların gerçek dünyası ise lokal repo + Claude Code/Cursor + Next.js/Python. Kurumsal auth/ortam katmanı zayıf |
| **Smallweb** | Self-host "internet klasörü": her klasör bir subdomain | Zarif felsefe, hacker kitlesi | Self-host = teknik olmayanlar dışarıda; ekip/paylaşım/auth yok; ticari itici güç yok |
| **Cloudflare VibeSDK + Workers for Platforms** | Kendi vibe-coding platformunu tek tıkla kur; üretilen appler izole Worker olarak deploy | Devasa altyapı, milyonlarca app ölçeği | Workers runtime kısıtları (her stack çalışmaz); hedefi *platform kuranlar*, son kullanıcı değil; "Google Doc gibi paylaş" katmanı yok |
| **Freestyle.sh vb. (AI-builder infra)** | AI app-builder şirketlerine deploy altyapısı (B2B) | Ajan-çağında doğru soyutlamalar | Son kullanıcıya/ekibe doğrudan ürün değil; tunr B2C+B2B2C oynayabilir |
| **Replit / Lovable / Bolt / v0** | Kapalı döngü: kendi ajanıyla yaz + kendi bulutunda barındır | Tam entegre deneyim | **Duvarlı bahçe**: Claude Code/Cursor/Codex CLI ile *lokal* çalışan (ve en hızlı büyüyen) kitleye hizmet etmiyorlar. "Ajan-agnostik altyapı" koltuğu boş |
| **Vercel / Netlify** | Repo-merkezli, frontend-ağırlıklı PaaS | Marka, DX | "Proje" töreni (repo, build, env, domain); 50 minik app için seat-fiyatlaması saçmalaşıyor; org-içi auth-by-default yok |
| **Fly.io / Railway / Render** | Genel amaçlı container PaaS | Esnek compute | Hâlâ Big Software ergonomisi (Dockerfile, config, ops); paylaşım/kimlik katmanı hiç yok |
| **ngrok** | Tünelden API gateway'e yukarı kayış | Marka, kurumsal satış | Küçük yazılım bulutuna gitmiyor; tünel→platform yolunun mümkün olduğunu ispatlıyor |

**Boşluğun tanımı:** *"Ajan-agnostik, stack-agnostik, CLI/MCP-öncelikli, lokal↔bulut sürekliliği olan, paylaşımı Google Doc gibi yapan küçük yazılım bulutu"* koltuğu boş. Val Town buna en yakın oyuncu ama dili (yalnız JS/TS) ve mekânı (tarayıcı editörü) onu sınırlıyor. Lovable/Bolt/Replit kapalı döngüde; Claude Code ve Cursor kullanıcısının — yani "ciddi vibecoder" çoğunluğunun — **doğal bir "publish" düğmesi yok.** tunr o düğme olabilir.

---

## 4. Kritik İçgörü: tunr'ın Özellik DNA'sı Zaten RFS'i İşaret Ediyor

Bu tablo, YC başvurusunun ve yeni landing page'in omurgası olmalı — çünkü pivotu "yön değişikliği" değil "yarım kalan cümlenin tamamlanması" olarak çerçeveliyor:

| Bugünkü tunr özelliği | Aslında neyin ilkel hali? | Küçük Yazılım Bulutu'ndaki evrimi |
|---|---|---|
| Freeze Mode (çökme önbelleği) | Kalıcılık / snapshot | Uygulamanın tam durumunun (dosya sistemi + süreç + SQLite) snapshot'ı; laptop kapansa da app yaşar |
| Demo Mode (yazma isteklerini kes) | **Görüntüleyici rolü** | Rol tabanlı yetki: viewer = GET-only, editor = tam erişim — proxy'de, uygulama kodu değişmeden |
| Auto-Login Bypass (cookie enjeksiyonu) | **Kimlik enjeksiyonu** | Relay'de gerçek OAuth (Google/Microsoft/GitHub) → app'e imzalı `X-Tunr-User` başlığı; app hiç auth kodu yazmaz |
| Inject-Widget (görsel geri bildirim) | **Google Doc yorumları** | Canlı uygulama üstünde yorum/pin katmanı; yorumlar ajana beslenip otomatik düzeltme PR'ına dönüşür |
| Parola / Bearer / IP allowlist | Erişim kontrol ilkelleri | "Linki olan herkes / şu e-postalar / şirket domaini" paylaşım diyaloğunun altyapısı |
| Relay (tüm trafiğin geçtiği kapı) | **Kontrol düzlemi** | Wake-on-request aktivatörü, auth kapısı, log/inspector, widget — hepsi zaten trafiğin geçtiği tek noktada |
| `tunr mcp` + `--json` + SDK'lar | Ajan-yerel API | Deploy/db/secret/share/log/rollback araçlarıyla tam MCP kontrol düzlemi; Claude Code'un "deploy düğmesi" |
| docker-compose self-hosting | Kurumsal ortam özelleştirme | BYOC runner: kontrol düzlemi bizde, compute şirketin kendi VM'inde |
| Path routing, TTL, multi-region | Trafik yönetimi | Aynen taşınır |

Ve tünelin küçük yazılım için **ölümcül kusuru**, pivotun tam gerekçesi: tünel, laptop açıkken yaşar. Küçük yazılımın tanımıysa "ben uyurken de iş arkadaşım kullanabilsin." Bu boşluğu kapatan tek bir komut, bütün pivotu özetler: **`tunr detach`** — aşağıda.

---

## 5. Ürün Vizyonu ve Çekirdek Konseptler

### 5.1 Yeni tek cümle

Eski: *"Expose your local server in 3 seconds."*
Yeni adaylar (İngilizce, çünkü YC başvurusu ve landing page için):

1. *"tunr is the cloud for small software: your agent builds it, tunr runs and shares it — like a Google Doc."*
2. *"Every day, thousands of apps are born in Claude Code and die on localhost. tunr is where they go to live."*
3. *"The missing deploy button for coding agents."*

(1) RFS diline birebir konuşuyor; (2) hikâye anlatımı için en güçlüsü; (3) GTM kancası. Başvuruda (2) ile aç, (1) ile tanımla.

### 5.2 Konsept A — App Kapsülü: kod + veri + yapılandırma = tek paylaşılabilir birim

Kullanıcının sezgisi ("uygulamayı backend'i ve veritabanıyla paylaşmak") tam isabet, ve bunu ürünleştirmenin adı **App Kapsülü**: her app, `kod + SQLite dosyası(ları) + secrets + ortam tanımı`ndan oluşan tek bir taşınabilir artefakt. Sonuçları:

*Paylaşmak* = kapsüle erişim vermek (Google Doc'ta olduğu gibi). *Fork/Remix* = kapsülün dallanması — **istenirse verisiyle birlikte** ("bu sprint takipçisini kopyala ama verileri sıfırla" ya da "verisiyle kopyala, kendi ekibim için uyarlayayım"). *Sürüm geçmişi* = kod + verinin zaman içindeki snapshot'ları; `tunr rollback` hem kodu hem veriyi geri alabilir (küçük yazılımda bu güvenlik hissi paha biçilmez — ajan yanlış migration yazarsa tek komutla dün akşama dön). *Taşınabilirlik* = kapsül dışa aktarılabilir (`tunr export` → tar dosyası: kod + SQLite + compose); anti lock-in söylemi Val Town'dan öğrenilecek en değerli ders — "credible exit" güven yaratıyor.

Teknik temel: **app başına SQLite** (+ nesne depolamaya sürekli replikasyon, Litestream/LiteFS deseni). Küçük yazılım için Postgres cluster'ı israf; SQLite hem ekonomiyi hem de "verisiyle fork" büyüsünü mümkün kılan şey. (Postgres, sonraki fazlarda "büyümüş app'ler" için opsiyon olur.)

### 5.3 Konsept B — `tunr detach`: lokal↔bulut sürekliliği (kimsede olmayan özellik)

Akış: `tunr share -p 3000` ile app laptop'tan canlı yayında. Tek komut — `tunr detach` (veya `tunr pin`) — tunr, çalışan uygulamayı paketleyip (build + veri senkronu) bulut runner'a taşır ve **aynı URL** kesintisiz buluttan servis etmeye başlar. Laptop kapanır; app yaşamaya devam eder. Dahası, relay her iki upstream'i de bildiği için **otomatik failover** yapılabilir: tünel koptuğunda bulut kopyasına düş (bu, Freeze Mode'un genelleşmiş, "gerçek" hali). Sonra laptop açılınca `tunr attach` ile canlı geliştirmeye dönersin; bulut kopyası güncel kalır.

Bu özellik üç iş yapıyor birden: (1) benzersiz demo — hiçbir rakipte yok, YC videosunun bel kemiği; (2) mevcut tünel kullanıcılarını sıfır sürtünmeyle bulut müşterisine çeviren dönüşüm mekanizması; (3) "lokalde ajanınla geliştir, bulutta yaşat" felsefesinin somut hali — Replit/Lovable'ın tarayıcıya hapsettiği döngünün özgür alternatifi.

### 5.4 Konsept C — tunr Gate: kimlik ve izinler proxy'de yaşar

RFS'in "auth & permissions are hard" cümlesinin cevabı: **auth, uygulamanın içinde değil önünde.** Relay zaten her isteği görüyor; Gate şunları yapar: (a) paylaşım politikasına göre girişi zorlar (herkese açık / linki olanlar / belirli e-postalar / `@sirket.com` domaini / SSO); (b) Google/Microsoft/GitHub OAuth (ekipler için OIDC/SAML) ile kullanıcıyı doğrular; (c) app'e imzalı bir `X-Tunr-User` JWT başlığı geçirir — app istersen kimseyi tanımaz, istersen bu başlıktan kullanıcıyı okur; (d) **rolleri HTTP seviyesinde uygular**: viewer = sadece güvenli metotlar (bugünkü demo mode!), editor = hepsi, admin = + ayarlar.

Kritik ek: coding ajanlarının bu deseni *öğrenmesi* için `X-Tunr-User` başlığını okuyan 5 satırlık middleware'ler (Express/Hono/FastAPI/Next) + `llms.txt` + bir "tunr skill" yayınlanır. Böylece Claude Code'a "kullanıcı bazlı yapılacaklar listesi yap, tunr'a yayınla" dendiğinde ajan doğru deseni kendiliğinden kurar. **Dokümantasyonu insanlara değil ajanlara yazmak** — bu dönemin en ucuz dağıtım kanalı.

### 5.5 Konsept D — İzin Manifesti: "arbitrary code"u güvenle paylaşılabilir kılmak

RFS'in (f) maddesinin cevabı iki parça. **İzolasyon:** her app kendi microVM/sandbox'ında; ağ varsayılanı **default-deny egress** (yalnız beyaz listedeki domainlere çıkış); CPU/RAM/disk kotaları; tenant'lar arası ağ yok. **Güven okunabilirliği:** bir app seninle paylaşıldığında, açmadan önce telefon uygulaması izin ekranı gibi bir kart görürsün: *"Bu uygulama şunlara erişiyor: api.stripe.com'a ağ çıkışı; adın ve e-postan (Gate'ten); kendi SQLite veritabanı. Şunlara erişemiyor: dosyaların, diğer uygulamalar, keyfî internet."* Bu kart koda güvenden değil, **runtime politikasından** türetilir — yani yalan söyleyemez. "Teknik olmayan birine ajan-yazımı kodu güvenle paylaşma" problemini gerçekten çözen, pazarlaması da güçlü bir özellik.

### 5.6 Konsept E — `tunr connect`: tünel DNA'sının ters yönde zaferi

RFS'in (d) maddesi (şirket ortam özelleştirmesi) için tunr'ın haksız avantajı: bugün tünel *localhost'u dışarı* açıyor; yarın aynı teknoloji *şirket içini küçük yazılıma* açar. Şirket ağındaki bir makinede `tunr connect --name internal-db --to 10.0.0.5:5432` çalışır; buluttaki küçük app'ler (yalnız o org'un app'leri, yalnız izinliyse) `internal-db.connect.tunr.internal` üstünden şirketin iç Postgres'ine/iç API'sine güvenle erişir. "İç veritabanımıza bakan minik dashboard'u ajanla yaz, ekibe paylaş" senaryosu — kurumsal küçük yazılımın %80'i budur — tek başına bu özellikle açılır. Buna ek olarak org düzeyi **ortam şablonları** (izinli egress listesi, ortak secret kasası, zorunlu SSO domaini, bölge tercihi) ve **BYOC runner** (kontrol düzlemi tunr'da, compute şirketin VM'inde — mevcut docker-compose self-hosting'in evrimi).

### 5.7 Sihirli demo (60 saniye — YC videosu ve landing page kahramanı)

1. Terminal: Claude Code'a "ekibimiz için basit bir sprint takipçisi yaz" denir; ajan yazar.
2. Ajan kendiliğinden (tunr MCP üzerinden) `deploy` çağırır → 15 saniyede `sprint.acme.tunr.app` canlı.
3. Kullanıcı dashboard'da paylaşım diyaloğunu açar: "@acme.com'daki herkes — Editör; dış danışman ali@x.com — Görüntüleyici."
4. İş arkadaşı linke tıklar → Google ile girer (app'te tek satır auth kodu yok) → veri girer.
5. Danışman aynı linkte salt-okunur görür, bir butonun üstüne yorum pini bırakır.
6. Yorum ajana düşer; ajan düzeltir, yeniden deploy eder; sürüm geçmişinde v1→v2 görünür.
7. Kapanış: laptop kapatılır — app çalışmaya devam eder. *"Small software, alive."*

Bu demonun her adımı mevcut DNA'nın uzantısı ve toplamı, RFS'teki her cümleye tek tek cevap veriyor.

---

## 6. Mimari Evrim

### 6.1 Hedef mimari (kuş bakışı)

```
                        ┌──────────────────────────────┐
  Kullanıcılar ───────► │  tunr Edge (relay'in evrimi) │
  (tarayıcı)            │  • TLS, subdomain routing     │
                        │  • Gate: OAuth, roller, JWT   │
                        │  • Wake-on-request            │
                        │  • Widget/yorum enjeksiyonu   │
                        │  • Log/inspector              │
                        └──────┬────────────┬──────────┘
                               │            │
                    (canlı tünel)      (kalıcı app)
                               │            │
                        ┌──────▼─────┐ ┌────▼─────────────────┐
                        │ Laptop     │ │ Runner filosu         │
                        │ tunr CLI   │ │ • app başına microVM/ │
                        │ (dev modu) │ │   sandbox, scale-to-0 │
                        └────────────┘ │ • SQLite + Litestream │
                                       │ • secrets, cron       │
                                       │ • egress allowlist    │
                                       └───────────┬──────────┘
                                                   │
                                     ┌─────────────▼─────────────┐
                                     │ Kontrol düzlemi            │
                                     │ • API + MCP + dashboard    │
                                     │ • kapsül deposu (kod+veri  │
                                     │   snapshot'ları, S3)       │
                                     │ • org/ortam politikaları   │
                                     └───────────────────────────┘
```

### 6.2 Mimarinin en değerli farkındalığı: relay = wake-on-request aktivatörü

Küçük yazılım ekonomisinin kilidi şu: 10.000 app × ortalama 3 kullanıcı × günde 20 istek = ihmal edilebilir trafik, ama klasik modelde 10.000 ayakta süreç. tunr'da **bütün trafik zaten relay'den geçiyor**; o hâlde uyuyan bir app'e istek gelince relay runner'a "uyandır" der, snapshot'tan geri yükleme (Firecracker snapshot restore / benzeri) yüzlerce milisaniyede olur, istek servis edilir, birkaç dakika sessizlikte app yeniden uyur. Boştaki app'in maliyeti ≈ birkaç MB nesne depolama. **Marjinal app maliyeti sıfıra yakın → free tier cömert olabilir → viral paylaşım döngüsü finanse edilebilir.** Fly.io benzer mekanikleri kanıtladı; tunr'ın farkı bunu uçtan uca tek üründe, paylaşım katmanıyla birlikte sunması.

### 6.3 Runtime kararı: hangi kod nasıl çalışır?

Üç seçenek değerlendirildi: (a) JS/TS isolate (Val Town / Workers modeli) — en ucuz ama stack kısıtlı; vibecoder gerçekte Next.js, Python/FastAPI, Go, her şey üretiyor; (b) tam VM — genel ama ağır; (c) **container → microVM, scale-to-zero** — doğru denge. Öneri: build tarafında Dockerfile *istemeyen* otomatik algılama (Nixpacks/Railpack tarzı: package.json görür Node kurar, requirements.txt görür Python kurar); çalıştırma tarafında Firecracker-sınıfı izolasyon. **v0'da kendi microVM filonu kurma** — Fly Machines API'si veya kendi birkaç VM'inde Docker+gVisor ile başla (relay zaten Fly üstünde), izolasyon katmanını gelir geldikçe derinleştir. Bu pragmatizm 5 günlük planın da temeli.

### 6.4 Veri ve platform servisleri

Sıralama bilinçli — Val Town'ın kullanım verisinin işaret ettiği talep sırası: (1) **SQLite** (app başına, otomatik yedekli, verisiyle fork edilebilir), (2) **secrets** (`tunr secret set STRIPE_KEY=…`), (3) **cron** (`tunr cron "0 9 * * 1" /report` — "her pazartesi rapor at" küçük yazılımın ekmeği), (4) **blob depolama**, (5) **e-posta al/gönder** (app başına `sprint@acme.tunr.app` adresi — otomasyon senaryolarını patlatır), (6) KV/queue-lite. Hepsi hem CLI, hem dashboard, hem **MCP aracı** olarak açılır — ajan "veritabanı lazım" dediğinde kendisi `create_db` çağırabilmeli.

### 6.5 MCP kontrol düzlemi (kullanıcının sezgisinin ürünleşmiş hali)

Mevcut `tunr mcp` genişletilir — hedef araç seti: `deploy_app` (dizin/tar → URL), `list_apps`, `get_logs`, `rollback`, `create_db` / `query_db`, `set_secret`, `set_share_policy` (kim, hangi rol), `invite_user`, `get_feedback` (widget yorumlarını çek — ajanın düzeltme döngüsü için), `snapshot` / `restore`, `create_cron`, `attach_connect` (iç kaynak bağla). İlke: **dashboard'da yapılabilen her şey MCP'de de yapılabilir.** Dağıtım: MCP registry'leri, Claude Code plugin/skill, Cursor rules, `llms.txt`, `AGENTS.md` şablonu. "Gerekirse MCP" değil — **MCP birincil arayüz**, CLI ve dashboard onun insan yüzleri.

### 6.6 Güvenlik ve kötüye kullanım (trust & safety)

Ciddiye alınmazsa şirketi öldürecek tek operasyonel konu. Katmanlar: microVM/sandbox izolasyonu; default-deny egress; kaynak kotaları; subdomain'lerde phishing/malware taraması ve itibar sistemi (ücretsiz tier'da rastgele subdomain + interstitial "bu bir kullanıcı uygulamasıdır" bandı — tünel sağlayıcıların kanla öğrendiği ders); hız limitleri; ödeme doğrulamalı kalıcı domainler; ihlal bildirim akışı. İzin Manifesti (5.5) burada hem güvenlik hem pazarlama görevi görür.

---

## 7. Yol Haritası

### Faz 0 — "Tezi kanıtla + YC başvurusu" (bu hafta, 5 gün)

Kapsam: `tunr deploy` v0 (Nixpacks build → mevcut Fly altyapısında ya da tek VM'de container → relay'in aynı subdomain'i bulut upstream'e bağlaması); MCP'ye `deploy_app` aracı; 60 saniyelik sihirli demonun 1–2 ve 7. adımlarını içeren video; landing page'e yeni anlatının eklenmesi (tünel silinmez, "Preview" katmanı olarak yeniden çerçevelenir); YC başvurusu. Paylaşım diyaloğu ve Gate bu hafta *yapılmaz* — başvuruda "shipped / next" ayrımı dürüstçe yazılır.

### Faz 1 — "Kalıcılık" (Ay 1–2)

Scale-to-zero runner + wake-on-request; app başına SQLite + otomatik yedek; secrets; cron; log akışı dashboard'da; **`tunr detach`** (killer feature — lansman anı budur, HN başlığı hazır: *"Show HN: Close your laptop, your localhost app keeps running"*); kapsül snapshot/rollback v1.

### Faz 2 — "Google Doc gibi paylaş" (Ay 2–4)

tunr Gate (Google/GitHub OAuth, `X-Tunr-User`, viewer/editor rolleri — demo mode'un genelleşmesi); paylaşım diyaloğu + app sayfası (isim, sahip, "Aç", erişim iste); widget v2 = canlı app üstünde yorumlar + `get_feedback` MCP aracı; sürüm geçmişi UI; framework middleware'leri + ajan-yönelik dokümantasyon seti.

### Faz 3 — "Ekipler ve kurumsal ortam" (Ay 4–6)

Org çalışma alanları + şirket içi app dizini ("ekibimizin araçları"); OIDC/SSO; ortam şablonları ve egress politikaları; **`tunr connect`**; BYOC runner; audit log. Fiyatlandırmada Team katmanı burada açılır.

### Faz 4 — "Ekosistem" (Ay 6+)

Remix/fork (verisiyle); şablon galerisi; "Edit with your agent" derin linkleri (app sayfasından tek tıkla kodu Claude Code/Cursor'da aç); AI-builder şirketlerine platform API'si (Freestyle'ın pazarına B2B2C girişi); app başına Postgres opsiyonu ("büyüyen küçük yazılım" için çıkış rampası — lock-in korkusunu da öldürür).

### Mevcut özellikler için tut/dönüştür kararları

Tünel: **tut**, "canlı önizleme + detach kaynağı" olarak yeniden konumla. Demo/auto-login: **dönüştür** → Gate rollerine. Freeze: **dönüştür** → snapshot/failover. Widget: **dönüştür** → yorumlar. TCP/UDP/TLS tünelleri: **tut ama vitrine koyma** (nakit akışı ve `connect`'in teknik temeli). Inspector/replay: **tut**, bulut app'lere de genişlet. Prometheus/servis kurulumu: bakım modu.

---

## 8. İş Modeli, Metrikler, GTM

**Fiyatlandırma evrimi:** Free — sınırsız tünel (huni) + 3 uyuyan app + tunr.app subdomain; Pro **$12–15/ay** — 20 app, custom domain, always-on seçeneği, gelişmiş roller (bugünkü $5 çapa fiyatı bilinçli terk edilmeli: "tünel" için $5 tavan, "yazılımın yaşadığı yer" için $15 taban); Team **$8–12/koltuk/ay** — org dizini, SSO, politikalar, audit, `connect`; Usage — kota üstü compute/depolama; Platform API — AI-builder'lara metered.

**Kuzey yıldızı metriği:** *haftalık aktif paylaşılan app* (≥2 farklı kullanıcının eriştiği app sayısı). Huni: tünel oturumu → deploy dönüşümü → app'in 30 gün hayatta kalması → app başına paylaşım linki açılışı → >1 kullanıcılı app oranı. Sağlık: time-to-URL (hedef <20 sn), wake gecikmesi (hedef <1 sn), abuse oranı.

**GTM sırası:** (1) mevcut tünel kullanıcıları (sıfır maliyetli ilk kohort); (2) ajan ekosistemi dağıtımı — MCP registry'leri, Claude Code skill, Cursor dizinleri, `llms.txt` (ajanlara görünür olmak = kullanıcıya görünür olmak); (3) HN/X lansman anları (Faz 1'de detach, Faz 2'de Gate); (4) içerik: "Claude Code ile yaz, tunr'la yaşat" tarifleri; (5) Faz 3'te ekip satışı — ilk 10 tasarım-ortağı şirket (danışmanlıklar ve iç-araç kültürü güçlü startuplar ideal).

---

## 9. Riskler ve Karşı Argümanlar (Pre-mortem)

**"Vercel/Cloudflare bunu yapar."** Yapabilirler; ama büyük platformların örgütsel çekimi Big Software'e ve mevcut fiyat modellerine doğru. Cloudflare VibeSDK'yı *platform kuranlara* veriyor, son kullanıcı ürünü yapmıyor. Savunma: hız + huni sahipliği (tünel kullanıcıları) + ajan-agnostik konum + paylaşım/kimlik katmanında derinlik. Ayrıca gerekirse Workers for Platforms *altyapı olarak kullanılabilir* — rakip aynı zamanda tedarikçidir.

**"Val Town zaten var."** En ciddi rakip; farklılaşma net tutulmalı: her stack (yalnız JS değil), ajanın yaşadığı yerde (lokal repo/CLI/MCP, tarayıcı editörü değil), lokal↔bulut sürekliliği, kurumsal Gate/ortam katmanı. Val Town'dan *kopyalanacak* şey: anti lock-in dürüstlüğü.

**Abuse maliyeti.** Ücretsiz hosting = phishing mıknatısı. Bütçe ve yol haritasında T&S ilk sınıf vatandaş (6.6); ihmal edilirse domain itibarı ve ödeme sağlayıcı ilişkileri çöker.

**Boşta-app ekonomisi.** Scale-to-zero + SQLite bunu çözer ama wake gecikmesi kötü yönetilirse ürün "yavaş" algılanır; snapshot-restore mühendisliğine erken yatırım şart.

**İki kişilik ekip, geniş kapsam.** Panzehir: fazların acımasız sıralanması; Faz 0–1'de *yalnız* deploy+detach; Gate'e kadar "auth" kelimesini telaffuz etmemek.

**Lisans.** PolyForm Shield, platform çağında katkıcıyı ürkütebilir; öneri: CLI + SDK'lar Apache-2/MIT (dağıtım maksimize), kontrol düzlemi kapalı veya FSL (Sentry modeli). "Open core, açık CLI" hem YC hem topluluk nezdinde temiz durur.

**Marka.** "tunr" tünel çağrışımlı ama kısa, akılda kalıcı ve "tune" (akort) okumasına da açık — *"tune your software to your team."* Erken aşamada isim değişikliğine enerji harcamaya değmez; anlatı değişsin, isim kalsın.

**Tünel kullanıcısı yabancılaşması.** Tünel ücretsiz ve birinci sınıf kaldıkça sorun yok; mesaj "tünel gitti" değil "tünelin artık bir devamı var."

---

## 10. YC Başvuru Stratejisi (son tarih: 27 Temmuz, 20:00 PT)

**Çerçeve:** Başvurunun kalbi traction değil (henüz yok, dürüst olun) — **insight + hız + huni** üçlüsü. Insight: bölüm 4'teki DNA hikâyesi ("tünel kullanıcılarımızın gerçek derdinin paket taşımak değil yazılım paylaşmak olduğunu gördük; demo-mode, freeze, feedback-widget'ı bu yüzden yaptık — meğer küçük yazılım bulutunun ilkellerini yazıyormuşuz"). Hız: v0.1→v0.4.1 arası sevkiyat temposu, 2 kişiyle Go'da uçtan uca relay+CLI+SDK+MCP. Huni: tünel = kanıtlanmış niyetli kullanıcı kaynağı.

**Yapılacaklar (başvuru öncesi):** 60 sn demo videosu (adım 1–2 ve 7: ajan yazar → MCP ile deploy → URL → laptop kapanır, app yaşar); one-liner olarak 5.1(2); "what have you built" bölümünde bugünkü tunr dürüstçe + `tunr deploy` v0; "why now" bölümünde RFS'e doğrudan atıf *ama* yaltaklanmadan — RFS'in kendisi de "bu fikirleri çalışıyor olmanız şart değil, ekstra validasyon sayın" diyor. Founder videosunda Türkçe aksanlı İngilizce hiç dert değil; enerji ve netlik her şey.

**Muhtemel zor sorulara hazır cevaplar:** "Vercel niye yapmasın?" → bölüm 9/1. "Val Town'dan farkın?" → bölüm 9/2. "Nasıl para kazanacaksın?" → bölüm 8. "Neden siz?" → tünel altyapısını zaten işletiyoruz; relay, wake-on-request'in yarısıdır; ve kullanıcı tabanımız pivotun ilk müşterisi.

**Kabul olmasa bile:** başvuru süreci tezi netleştirir; geç başvuru + Winter 2027 yolu açık; plan YC'den bağımsız olarak da doğru plan.

---

## 11. Önümüzdeki 5 Günün Planı (22–27 Temmuz)

**Gün 1 (bugün):** Karar + kapsam dondurma. `tunr deploy` v0 tasarımı: Nixpacks ile build, mevcut Fly hesabında machine olarak koş, relay'de subdomain→cloud upstream eşlemesi. Başvuru taslağının iskeletini aç.

**Gün 2–3:** `tunr deploy` v0 kodu (mutlu yol yeter: Node ve Python şablonları çalışsın, hata durumları sonra). MCP'ye `deploy_app` aracı. Basit "app yaşıyor" dashboard satırı.

**Gün 4:** Demo videosu çekimi (60 sn, tek alışta akıcı olana dek). Landing page'e yeni anlatı bloğu: *"tunr now runs your app after you close your laptop"* + waitlist.

**Gün 5 (27 Temmuz'dan önce):** Başvuru metni son okuma (bir yerlisine/İngilizcesi güçlü birine okutun), video yükleme, gönder. Gönderdikten sonra: HN "Show HN" taslağını hazırla ama **detach hazır olana sakla** — barutu erken yakma.

---

## 12. Sonuç

RFS'in istediği şeyle tunr'ın elindeki şey arasındaki mesafe, dışarıdan göründüğünden çok daha kısa. Pete Koomen "yazılımı Google Doc gibi paylaşılabilir yapın" diyor; tunr zaten yazılım paylaşmanın proxy-katmanı ilkellerini (roller, kimlik enjeksiyonu, yorumlar, çökme dayanıklılığı) yazmış, üstelik ajanların konuşabildiği (MCP) tek tünel olarak yazmış. Eksik olan, uygulamanın laptop'tan bağımsız yaşayacağı gövde: scale-to-zero compute, verisiyle taşınabilir App Kapsülü ve relay'in kapıya (Gate) evrimi. Sıralama net: bu hafta `deploy` v0 + YC başvurusu; iki ayda `detach` ile lansman; dört ayda Google-Doc-paylaşımı; altı ayda kurumsal ortam katmanı. Tünel ölmesin — huni olsun. Ve anlatı tek cümlede kalsın: **localhost'ta doğan yazılımın yaşayacağı yer.**

---

### Ek: Kaynaklar
- YC RFS — A Cloud for Small Software (Pete Koomen): https://www.ycombinator.com/rfs#a-cloud-for-small-software
- YC başvuru: https://apply.ycombinator.com (F26 son tarih: 27 Temmuz 2026, 20:00 PT)
- tunr: https://github.com/ahmetvural79/tunr · https://tunr.sh
- Val Town (docs, alternatives, blog): https://docs.val.town · https://www.val.town/alternatives
- Smallweb: https://www.smallweb.run
- Cloudflare VibeSDK / Workers for Platforms: https://blog.cloudflare.com/deploy-your-own-ai-vibe-coding-platform/
