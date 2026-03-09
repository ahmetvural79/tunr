# Tunr CLI Referansı

Bu doküman, `tunr` komut satırı aracının (CLI) resmi kullanım referansıdır.

## Temel Kurulum ve Kullanım

### `tunr doctor`
Sisteminizin ve tunr kurulumunuzun sağlığını kontrol edin.
* Bağlantı ve binary kontrolü
* Config dosyası doğrulaması
* Relay sunucu bağlantısı
* İşletim sistemi servis durumu

### `tunr config`
Workspace ve kullanıcı ayarlarını yönetin.
* `tunr config init`: Bulunduğunuz proje dizininde bir `.tunr.json` oluşturur.

---

## Tünel Yönetimi (Core Commands)

### `tunr share`
Localport'u < 3 saniyede genel (public) bir URL üzerinden dünyaya açar.

**Zorunlu Parametreler:**
* `-p, --port <int>`: Yönlendirilecek lokal port (Örn: `3000`)

**Opsiyonel Parametreler:**
* `-s, --subdomain <string>`: Özel alt alan adı tahsisi (Pro hesaplara özel). Örn: `myapp` -> `myapp.tunr.sh`
* `--no-open`: Tünel başarılı çalıştığında tarayıcıyı otomatik açmayı engeller.
* `--json`: Konsol çıktısını izole bir şekilde scriptlerde kullanılmak üzere JSON formatına basar (CI/CD / MCP bot uyumu).

**Vibecoder Demo Bayrakları:**
Freelancer ve ajansların müşterilerine kusursuz bir ürün denetimi sunabilmeleri ve aksamaları önlemeleri için geliştirilmiş ileri seviye pro-proxy bayrakları.

* `--demo`: Okuma modunu (read-only) açar. Zararlı POST, PUT, DELETE metotlarını proxy seviyesinde durdurur. Geriye "201 Created" veya "200 Success" gibi sahte mock json'lar döndürür. Müşteri butona tıklayabilir ama "Siparişi Sil" tuşu backend veritabanını bozmaz.
* `--freeze`: Hata-Tölerans Modu. Sunucunuz çökse bile proxy son başarılı 200 GET isteklerini (HTML, CSS, imaj) önbelleğinden (`X-Tunr-Freeze-Cache`) vermeye devam eder. Müşteri hiçbir şey hissetmez.
* `--inject-widget`: Sunulan HTML belgesinin sonuna şeffaf bir "Feedback" arayüzü ile hata ayıklama aracı ekler. Ekranda pin bırakılabilir ve js consol'unda alınan remote `window.onerror` logları eşzamanlı olarak kendi CLI monitörünüze düşer.
* `--auto-login <string>`: Her bağlantıda tarayıcıya otomatik olarak auth cookie'si veya JWT `Authorization` header'ı enjekte eder. Örn: `--auto-login "session=demo-user-1"`. Müşteriniz linki açtığında login ekranıyla uğraşmadan direkt panelde olur.

#### Örnekler
```bash
# Basit Paylaşım
tunr share --port 8080

# Subdomain Atamalı
tunr share --port 8080 -s benimapp

# Komple Vibecoder Müşteri Paketini Aç
tunr share -p 3000 --demo --freeze --inject-widget --auto-login "Cookie: session=xyz"
```

---

## Arka Plan ve Daemon Çalışma

### `tunr start`
Arka planda (daemon olarak) kalıcı tünel açar. Sunucu veya terminal kapansa da işlem arkada devam eder.
```bash
tunr start --port 3000
```

### `tunr stop`
Arkada sessizce çalışan tünelleri güvenlice durdurur.
```bash
tunr stop
```

### `tunr status`
Mevcut aktif tünellerin (hem ön hem arka plan) tüm durumunu, istatistiklerini tabulalar halinde listeler.
```bash
tunr status
```

---

## Log ve Debug İşlemleri

### `tunr logs`
Tüneller üzerinden geçen tüm request (istek) verilerini anlık (real-time websocket stream) terminal üzerinden gösterir.
```bash
tunr logs
```

### `tunr replay`
Kaydedilen bir HTTP request id'sine göre o işlemin client üzerinden sanki yeniden tetikleniyormuş gibi birebir aynısını test için sunucuya aktarır. 
```bash
tunr replay abc-123-xyz
```

### `tunr open`
Eğer CLI terminalini tercih etmiyorsanız, request loglarını ve ayarlamaları görmek adına tunr'nun görsel arabirimini barındıran Embedded React Localhost Dashboard'u sistem tarayıcınızda açar.

---

## AI Bot/Agent Entegrasyon

### `tunr mcp`
Claude Desktop veya Cursor IDE içerisinde doğrudan bir Model Context Protocol sunucusunu başlatarak Yapay Zekaların tünel oluşturmasına ve logları okumasına izin verir. 
```bash
tunr mcp
```
