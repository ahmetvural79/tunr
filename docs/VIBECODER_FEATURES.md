# Vibecoder Müşteri Demo Özellikleri

Müşterilerinize veya dış ekiplere ürününüzü (mobil, web, desktop api) gösterirken en büyük stres yerel bir hatanın sunumu bozmasıdır. `tunr` bu kaygıyı ortadan kaldırmak için Phase 8 güncellemesinde 4 adet çok spesifik, akıllı proxy katmanı (middleware) yaratmıştır. Bu makalede bu özelliklerin arkasındaki teknik mantık ve kullanım yer almaktadır. 

### `--demo`: The Safe Read-Only Mod
`tunr share --port 8080 --demo`

Geliştiriciler yerel bilgisayar dizinlerindeki projeyi müşteriye test ettirirken formların submit edilip sahte siparişlerin databasede kirlilik yaratmasını istemeyebilir.

**Nasıl Çalışır?** 
1. HTTP isteği `tunr` LocalProxy katmanına girer.
2. Metod kontrol edilir. Eğer istek `POST`, `PUT`, `PATCH`, veya `DELETE` ise,
3. Proxy bu isteğin localhost'taki (port 8080) backend uygulamanıza yönlendirilmesini **iptal eder.** (Request Drop)
4. Proxy bunun yerine frontend'i tatmin edecek bir JSON cevabını `{"status": "demo_success", "message": "Demo mode: Request mocked"}` `200 Success` HTTP state kodu eşliğinde manuel olarak oluşturup anında geri döner.
5. Siteniz çalışmaya ve etkileşime devam eder, ama veritabanınıza hiçbir veri yazılmaz.

### `--freeze`: Localhost Kalkanı (Snapshoting)
`tunr share --port 8080 --freeze`

Tam demoyu yaparken kodunuzu güncellediniz veya uygulamanız (Nodemon, Air vs) crash verdi ve restart atmak zorunda kaldı. Klasik tünellerde dış cliente hemen 502 Bad Gateway veya "Connection Refused" ekranı çıkar. Müşteri korkar.

**Nasıl Çalışır?** 
1. Proxy, tünel açık olduğu sürece arka planda localhostunuzdan dönen başarılı "2xx Success" (HTML, CSS, JSON) statik cevaplarını memory tabanlı bir Hash table'da tutar.
2. İşler ters gittiğinde: Localhostunuz `500 Server Error` verirse veya proxy localhosta bağlanamazsa (`TCP dial error`), proxy müşteriye asla hata göstermez. 
3. Doğrudan bu cache memory'e başvurup o endpoint'in son çalışan sorunsuz versiyonunu ekrana basar.
4. Client kesintisiz demoyu incelerken, sizin backend'i fixleyip ayağa kaldırmanız için kritik dakikalar kazandırılmış olur.

### `--inject-widget`: Transparan UI ve Log Yakalayıcı
`tunr share --port 8080 --inject-widget`

Müşteri ürünü test ederken WhatsApp üzerinden ekran görüntüsü alıp hatayı tarif etmesi geliştirici için işkencedir.

**Nasıl Çalışır?** 
1. Sunucunuz HTML dokümanı (`text/html`) döndürdüğü anda proxy bu yanıtı hafızada tutalar ("response intercepting").
2. HTML'in GZip/Deflate sıkıştırması varsa anında byte-array olarak decode edilir.
3. HTML'in içindeki tag'ler regex destekli analizle taranıp kapatanan `</body>` tagı bulunur.
4. Buraya `tunr`nun CDN tarafında barındırdığı iki parçalı bir remote javascript bloğu gömülür ve orjinal HTML dosyası tekrar aynı boyutta sıkıştırılıp (GZip) müşteriye gönderilir. 
5. Müşteri şeffaf feedback butonuna tıklayıp ekranın neresinde sorun olduğunu yazar (Pinning). 
6. Ayrıca script, istemcinin tarayıcısında konsola basılan tüm `window.onerror` loglarını sessizce toplar.
7. Bilgiler Tunr'nun kendi `/__tunr/feedback` route'una (proxy tarafından intercept edilen localhost'a görünmeyen fake route'lar) POST atılır ve sizin terminal monitörünüze saniyesinde sarı ve mavi loglar ile düşer.

### `--auto-login`: Otomatik Ziyaretçi Kimliği
`tunr share --port 8080 --auto-login "Cookie: user_session_token=abcdef5551"` 

B2B veya SaaS sistemleri sunulurken ilk barikat Auth / Login sayfasıdır. Müşteri şifre unuttumla uğraşmamalı ve direkt ana panele düşmelidir.

**Nasıl Çalışır?**
1. Tarayıcıdan bir query parametresi yakalanır veya tunr tünel aktif edilirken bu argüman doğrudan tünel state'ine yazılır.
2. Chrome / Safari vs üzerinden gelen saf müşterinin HTTP request header'larına, `tunr` LocalProxy katmanında proxy manipülasyonu yapılarak istenen kimlik kartı (`Cookie` veya `Authorization` beareri) eklenir ve localhost'a öyle forwardlanır.
3. Local sunucunuz isteği direkt Auth bariyerini geçmiş bir Admin gibi yorumlayıp kapıları açar. Müşterinin signup olma süreci bypass edilir.
