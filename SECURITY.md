# Security Policy

## Tunr'ya Hoş Geldiniz 👋

tunr açık kaynaklı bir projedir. Bu hem güzel (topluluk katkısı!) hem de sorumluluk gerektiren (güvenlik!) bir şey demek.

## Desteklenen Versiyonlar

| Versiyon | Güvenlik Desteği |
|----------|:----------------:|
| 0.x (pre-release) | ✅ Aktif |
| Geliştirme | ✅ Aktif |

## Güvenlik Açığı Bildirmek

**Lütfen güvenlik açıklarını GitHub Issues'da PUBLIC olarak yayınlamayın.**

Bu, diğer kullanıcıları tehlikeye atabilir. Onun yerine:

### Tercih Edilen Yöntem: GitHub Private Security Advisory

1. Bu reponun **"Security"** sekmesine gidin
2. **"Report a vulnerability"** butonuna tıklayın
3. Detayları doldurun ve gönderin

### Alternatif: E-posta

Şifreli iletişim için:
- **E-posta:** security@tunr.sh
- **PGP:** (Yakında — Faz 0'da eklenecek)

## Ne Bekleyebilirsiniz?

| Süre | Beklenti |
|------|----------|
| 48 saat | Bildirimi aldık, inceliyoruz |
| 7 gün | Severity değerlendirmesi |
| 30 gün | Fix veya mitigation planı |
| 90 gün | Fix yayınlanır (CVE varsa koordineli) |

## Kapsam İçi (In-Scope)

Bu konularda raporlar kabul ediyoruz:

- **Auth bypass** — token'ları atlatmak
- **SSRF** — iç ağ erişimi sağlamak (özellikle `tunr share` üzerinden)
- **path traversal** — dosya sistemi erişimi
- **injection** — OS command injection, log injection
- **information disclosure** — token/secret sızıntısı
- **tunnel hijacking** — başkasının tunnel'ını ele geçirmek
- **DoS** — single payload ile servis dışı bırakmak

## Kapsam Dışı (Out-of-Scope)

Bunlar için issue açabilirsiniz ama security advisory değil:

- Self-XSS (kendi tarayıcınızı kendiniz hacklemek)
- Rate limiting (makul seviyelerin üzerinde olmadıkça)
- Missing security headers (enhancement olarak açın)
- Sosyal mühendislik
- Fiziksel saldırılar

## Güvenli Geliştirme Prensipleri

Bu projede şu kurallara uyuyoruz:

1. **Sır saklama:** Token'lar log'a geçmez, config'e plaintext yazılmaz
2. **Input validation:** Her CLI argümanı doğrulanır
3. **Least privilege:** Gerektiğinden fazla izin istenmez
4. **Dependency audit:** `govulncheck` CI'da zorunlu
5. **TLS verification:** `InsecureSkipVerify: true` yazan PR reddedilir
6. **crypto/rand:** Güvenlik gerektiren yerlerde `math/rand` kullanılmaz

## Dependabot

Dependency güvenlik güncellemeleri için `dependabot.yml` aktif.
Kritik CVE'ler için 48 saat içinde patch yayınlanır.

## Hall of Fame

Güvenlik açığı bildirenlere minnettarız. Buraya isminizi ekleriz 🏆

(Henüz boş - belki siz ilk olursunuz?)

---

*Bu politika [Faz 0] itibariyle geçerlidir. Proje büyüdükçe güncellenecek.*
