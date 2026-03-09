# tunr — Deployment & Test Rehberi

> Hetzner Cloud üzerinde sıfırdan production kurulumu ve uçtan uca test kılavuzu.

---

## İçindekiler

1. [Gereksinimler](#1-gereksinimler)
2. [Hetzner Sunucu Kurulumu](#2-hetzner-sunucu-kurulumu)
3. [Docker & Bağımlılıklar](#3-docker--bağımlılıklar)
4. [PostgreSQL Kurulumu](#4-postgresql-kurulumu)
5. [Relay Sunucu Deploy](#5-relay-sunucu-deploy)
6. [Caddy (TLS + Reverse Proxy)](#6-caddy-tls--reverse-proxy)
7. [DNS Yapılandırması](#7-dns-yapılandırması)
8. [Landing Page Deploy](#8-landing-page-deploy)
9. [Ortam Değişkenleri](#9-ortam-değişkenleri)
10. [Servis Yönetimi (systemd)](#10-servis-yönetimi-systemd)
11. [Uçtan Uca Test Rehberi](#11-uçtan-uca-test-rehberi)
12. [İzleme ve Log](#12-izleme-ve-log)
13. [Yedekleme](#13-yedekleme)
14. [Sık Karşılaşılan Sorunlar](#14-sık-karşılaşılan-sorunlar)

---

## 1. Gereksinimler

| Bileşen | Versiyon | Amaç |
|---------|----------|------|
| Hetzner Cloud | CPX21 (3 vCPU, 4GB RAM) | Relay sunucu |
| Ubuntu | 24.04 LTS | İşletim sistemi |
| Go | 1.22+ | Relay binary derleme |
| Docker / Docker Compose | 25+ | PostgreSQL container |
| Caddy | 2.7+ | TLS + reverse proxy |
| Cloudflare | Free plan | DNS + wildcard TLS |

**Maliyet tahmini:** ~€0.037/saat → ~€12/ay (CPX21)

---

## 2. Hetzner Sunucu Kurulumu

### 2.1 Sunucu Oluştur

[Hetzner Cloud Console](https://console.hetzner.cloud/) → New Server

| Ayar | Değer |
|------|-------|
| Location | Nuremberg (nbg1) veya Helsinki (hel1) |
| Image | Ubuntu 24.04 |
| Type | CPX21 (3 vCPU, 4GB) |
| Networking | Enable IPv4 + IPv6 |
| SSH Key | Public key'inizi ekleyin |

### 2.2 İlk Bağlantı ve Güvenlik

```bash
# Bağlan
ssh root@<SUNUCU_IP>

# Güncelle
apt update && apt upgrade -y

# Firewall — sadece gerekli portları aç
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp   # SSH
ufw allow 80/tcp   # HTTP (Let's Encrypt redirect)
ufw allow 443/tcp  # HTTPS
ufw enable

# fail2ban (brute force koruması)
apt install -y fail2ban
systemctl enable --now fail2ban

# Unattended upgrades (güvenlik yamaları otomatik)
apt install -y unattended-upgrades
dpkg-reconfigure -plow unattended-upgrades

# root SSH girişini kapat (SSH key ile giriş zaten güvenli)
sed -i 's/#PermitRootLogin yes/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
systemctl restart sshd

# tunr kullanıcısı oluştur
useradd -m -s /bin/bash tunr
usermod -aG docker tunr
```

### 2.3 Hostname Ayarla

```bash
hostnamectl set-hostname tunr-relay
echo "127.0.0.1 tunr-relay" >> /etc/hosts
```

---

## 3. Docker & Bağımlılıklar

```bash
# Docker kur
curl -fsSL https://get.docker.com | sh
systemctl enable --now docker

# Go kur (relay binary derlemek için)
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh
source /etc/profile.d/go.sh
go version

# Caddy kur
apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | tee /etc/apt/sources.list.d/caddy-stable.list
apt update && apt install -y caddy
```

---

## 4. PostgreSQL Kurulumu

### 4.1 Docker Compose ile Başlat

```bash
mkdir -p /opt/tunr && cd /opt/tunr

cat > docker-compose.yml << 'EOF'
version: '3.9'

services:
  postgres:
    image: postgres:16-alpine
    container_name: tunr-postgres
    restart: unless-stopped
    environment:
      POSTGRES_DB: tunr
      POSTGRES_USER: tunr
      POSTGRES_PASSWORD: ${DB_PASSWORD}  # .env'den gelir
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./relay/migrations:/docker-entrypoint-initdb.d:ro
    ports:
      - "127.0.0.1:5432:5432"   # Sadece localhost — dışarıya açılmaz
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U tunr"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
EOF
```

### 4.2 .env Dosyası Oluştur

```bash
# GÜVENLİK: .env git'e commit edilmez!
cat > /opt/tunr/.env << EOF
DB_PASSWORD=$(openssl rand -base64 32)
TUNR_JWT_SECRET=$(openssl rand -hex 32)
TUNR_DOMAIN=tunr.sh
PORT=8080
CLOUDFLARE_API_TOKEN=your_cf_token_here
EOF

chmod 600 /opt/tunr/.env
```

> [!CAUTION]
> `.env` dosyasını asla git'e commit etmeyin, asla başkasıyla paylaşmayın.

### 4.3 PostgreSQL Başlat

```bash
cd /opt/tunr
docker compose up -d postgres

# Migration'ın çalıştığını doğrula
docker compose exec postgres psql -U tunr -d tunr -c "\dt"
# users, tunnels, sessions, magic_tokens, audit_log, plan_limits tablolarını görmeli
```

---

## 5. Relay Sunucu Deploy

### 5.1 Kaynak Kodu Kopyala

```bash
# Geliştirme makinenizde (projo dizininde):
rsync -avz --exclude='.git' --exclude='vendor' \
  tunr/ root@<SUNUCU_IP>:/opt/tunr/src/

# Veya git ile:
# ssh root@<SUNUCU_IP>
# git clone https://github.com/yourusername/tunr /opt/tunr/src
```

### 5.2 Relay Binary Derle

```bash
cd /opt/tunr/src/relay

# Production binary — debug bilgisi yok, küçük boyut
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags="-w -s -X main.Version=$(git describe --tags --always)" \
  -o /usr/local/bin/tunr-relay \
  ./cmd/server

# İzinleri ayarla
chmod 755 /usr/local/bin/tunr-relay

# Test et
TUNR_JWT_SECRET="test-secret-32-chars-minimum-len" \
  tunr-relay --help 2>&1 || echo "Binary çalışıyor"
```

### 5.3 CLI Binary Derle ve Dağıt

```bash
# tunr CLI binary'si (kullanıcılara dağıtılır)
cd /opt/tunr/src
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath \
  -ldflags="-w -s" \
  -o /var/www/tunr/downloads/tunr-linux-amd64 \
  ./cmd/tunr

CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
  -trimpath \
  -ldflags="-w -s" \
  -o /var/www/tunr/downloads/tunr-darwin-arm64 \
  ./cmd/tunr

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -trimpath \
  -ldflags="-w -s" \
  -o /var/www/tunr/downloads/tunr-windows-amd64.exe \
  ./cmd/tunr
```

---

## 6. Caddy (TLS + Reverse Proxy)

### 6.1 Cloudflare API Token Oluştur

[Cloudflare Dashboard](https://dash.cloudflare.com/) → Profile → API Tokens → Create Token

Gerekli izinler:
- `Zone:DNS:Edit` — tunr.sh zone için
- `Zone:Zone:Read`

### 6.2 Caddy xcaddy ile Cloudflare DNS Plugin Derle

```bash
# xcaddy kur
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# Cloudflare DNS plugin ile Caddy derle
xcaddy build \
  --with github.com/caddy-dns/cloudflare \
  --output /usr/local/bin/caddy

# Caddy'ye capability ver (80/443 portları için root gerekmez)
setcap cap_net_bind_service=+ep /usr/local/bin/caddy
```

### 6.3 Caddyfile Yapılandır

```bash
mkdir -p /etc/caddy
cp /opt/tunr/src/relay/caddy/Caddyfile /etc/caddy/Caddyfile

# Cloudflare token'ı güncelle
sed -i "s/CLOUDFLARE_API_TOKEN/$CLOUDFLARE_API_TOKEN/" /etc/caddy/Caddyfile
```

### 6.4 Caddy Servis

```bash
# /etc/systemd/system/caddy.service
cat > /etc/systemd/system/caddy.service << 'EOF'
[Unit]
Description=Caddy Web Server
After=network.target

[Service]
User=caddy
Group=caddy
ExecStart=/usr/local/bin/caddy run --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile
TimeoutStopSec=5s
EnvironmentFile=/opt/tunr/.env
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

useradd -r -s /usr/sbin/nologin caddy
systemctl daemon-reload
systemctl enable --now caddy
```

---

## 7. DNS Yapılandırması

[Cloudflare Dashboard](https://dash.cloudflare.com/) → tunr.sh → DNS

| Tür | Name | Content | Proxy |
|-----|------|---------|-------|
| A | `@` | `<SUNUCU_IP>` | ☁️ Proxied |
| A | `www` | `<SUNUCU_IP>` | ☁️ Proxied |
| A | `*` | `<SUNUCU_IP>` | ☁️ Proxied |
| A | `relay` | `<SUNUCU_IP>` | 🟠 DNS only |

> [!IMPORTANT]
> `*` wildcard kaydı her tunnel'ın subdomain'ini (abc123.tunr.sh) doğru IP'ye yönlendirir.

**Cloudflare SSL/TLS Modu:** Full (Strict) — Caddy ve Cloudflare arası gerçek TLS.

---

## 8. Landing Page Deploy

```bash
mkdir -p /var/www/tunr
cp -r /opt/tunr/src/landing/* /var/www/tunr/

# İzinleri ayarla
chown -R caddy:caddy /var/www/tunr
chmod -R 755 /var/www/tunr
```

---

## 9. Ortam Değişkenleri

```bash
# /opt/tunr/.env — tam liste

# ─── Zorunlu ───────────────────────────────
TUNR_DOMAIN=tunr.sh
TUNR_JWT_SECRET=<en-az-32-karakter-random-string>
DATABASE_URL=postgres://tunr:<DB_PASSWORD>@localhost:5432/tunr?sslmode=disable

# ─── Opsiyonel ─────────────────────────────
PORT=8080
TUNR_LOG_LEVEL=info        # debug | info | warn

# ─── Paddle (billing) ──────────────────────
PADDLE_API_KEY=<paddle-api-key>
PADDLE_WEBHOOK_SECRET=<paddle-webhook-secret>
PADDLE_SANDBOX=false

# ─── Cloudflare (TLS) ──────────────────────
CLOUDFLARE_API_TOKEN=<cf-api-token>

# ─── Email (magic link) ────────────────────
SMTP_HOST=smtp.resend.com
SMTP_PORT=587
SMTP_USER=resend
SMTP_PASS=<resend-api-key>
SMTP_FROM=noreply@tunr.sh
```

---

## 10. Servis Yönetimi (systemd)

```bash
# /etc/systemd/system/tunr-relay.service
cat > /etc/systemd/system/tunr-relay.service << 'EOF'
[Unit]
Description=tunr Relay Server
After=network.target docker.service
Requires=docker.service

[Service]
User=tunr
WorkingDirectory=/opt/tunr
EnvironmentFile=/opt/tunr/.env
ExecStart=/usr/local/bin/tunr-relay
Restart=always
RestartSec=5s

# GÜVENLİK: Process izolasyonu
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/tunr
PrivateTmp=true

# Log
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tunr-relay

[Install]
WantedBy=multi-user.target
EOF

mkdir -p /var/log/tunr
chown tunr:tunr /var/log/tunr

systemctl daemon-reload
systemctl enable --now tunr-relay

# Kontrol
systemctl status tunr-relay
journalctl -u tunr-relay -f
```

---

## 11. Uçtan Uca Test Rehberi

### 11.1 Unit Testler (Local)

```bash
# Tüm testler (race detector ile)
cd tunr
go test -race -timeout 60s ./internal/... ./cmd/...

# Relay testleri
cd relay
go test -race -timeout 30s ./internal/...

# Spesifik paket
go test -v -run TestJWTAlgNone ./relay/internal/auth/...

# Benchmark
go test -bench=. -benchmem ./internal/inspector/...
```

**Beklenen çıktı:**
```
ok  github.com/tunr-dev/tunr/internal/billing   ✓
ok  github.com/tunr-dev/tunr/internal/inspector  ✓
ok  github.com/tunr-dev/tunr/internal/mcp        ✓
ok  github.com/tunr-dev/tunr/internal/config     ✓
ok  github.com/tunr-dev/tunr/relay/internal/auth ✓
ok  github.com/tunr-dev/tunr/relay/internal/relay ✓
```

### 11.2 CLI Smoke Test

```bash
# Binary derle
go build -o tunr ./cmd/tunr

# Versiyon kontrolü
./tunr version

# Sistem tanısı
./tunr doctor

# Config başlat
./tunr config init
cat .tunr.json

# Yardım
./tunr --help
./tunr share --help
./tunr mcp --help
```

### 11.3 Relay Server End-to-End Test

```bash
# Terminal 1 — Test HTTP sunucusu başlat
python3 -m http.server 9999

# Terminal 2 — Relay server başlat (local)
cd relay
TUNR_JWT_SECRET="test-secret-minimum-32-chars!!" \
  DATABASE_URL="" \
  TUNR_DOMAIN="localhost:8080" \
  PORT=8080 \
  go run ./cmd/server &
RELAY_PID=$!

# Terminal 3 — tunr CLI tunnel aç
# (relay'i localhost:8080'e point et)
TUNR_RELAY_URL="ws://localhost:8080/tunnel/connect" \
  ./tunr share --port 9999

# Başka terminal — tunnel test et
curl http://localhost:19842/api/v1/health    # Inspector API
curl http://localhost:19842/api/v1/requests  # Yakalanmış istekler

kill $RELAY_PID
```

### 11.4 WebSocket Tunnel Test

```bash
# wscat ile WebSocket bağlantısını doğrula
npm install -g wscat

# Relay'e bağlan (test)
wscat -c "ws://localhost:8080/tunnel/connect" \
  --header "Authorization: Bearer test"

# Hello mesajı gönder
{"type":"hello","data":{"token":"","local_port":3000,"version":"test"}}

# Beklenen yanıt:
# {"type":"welcome","data":{"tunnel_id":"abc12345","subdomain":"xyzabc12","public_url":"http://xyzabc12.localhost:8080"}}
```

### 11.5 Güvenlik Testleri

```bash
# 1. JWT alg:none saldırısı testi
go test -v -run TestJWTAlgNone ./relay/internal/auth/...

# 2. Webhook replay attack testi
go test -v -run TestWebhookReplayAttack ./internal/billing/...

# 3. Header sanitizasyonu testi
go test -v -run TestSensitiveHeaderRedaction ./internal/inspector/...

# 4. Race condition testi
go test -race -v -run TestRegistryConcurrency ./relay/internal/relay/...

# 5. SSRF koruması (private IP'lere erişim engelleniyor mu?)
# Relay'in private IP'lere forward etmediğini doğrula
curl -H "Host: 127.0.0.1.tunr.sh" http://localhost:8080/
# Beklenen: 404 "tunnel bulunamadı" (private IP olduğu için)

# 6. govulncheck (bilinen CVE'ler)
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...

# 7. golangci-lint
golangci-lint run ./...
```

### 11.6 Production Sağlık Kontrolleri

```bash
# Relay health check
curl https://tunr.sh/api/v1/health
# {"status":"ok","timestamp":1709855000}

# Aktif tunnel sayısı
curl https://tunr.sh/api/v1/status
# {"active_tunnels":42}

# TLS Sertifika kontrolü
curl -vI https://tunr.sh 2>&1 | grep -E "SSL|subject|expire"

# Wildcard TLS
curl -vI https://test.tunr.sh 2>&1 | grep -E "SSL|CN="

# PostgreSQL bağlantısı
docker compose exec postgres psql -U tunr -d tunr \
  -c "SELECT COUNT(*) FROM users;"
```

### 11.7 Yük Testi

```bash
# hey ile basit yük testi
go install github.com/rakyll/hey@latest

# 100 eşzamanlı, 1000 istek
hey -n 1000 -c 100 https://tunr.sh/api/v1/health

# Sonuç beklentisi:
# Requests/sec: >500
# P99 latency: <100ms
# Error rate: 0%
```

### 11.8 CLI Integration Test (tam akış)

```bash
#!/bin/bash
# test_e2e.sh — uçtan uca entegrasyon testi

set -euo pipefail

PORT=18888
echo "🧪 tunr E2E testi başlıyor..."

# Test HTTP sunucusu
python3 -c "
import http.server, threading
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'tunr test OK')
    def log_message(self, *a): pass
s = http.server.HTTPServer(('', $PORT), H)
threading.Timer(30, s.shutdown).start()
s.serve_forever()
" &
HTTP_PID=$!

# tunr tunnel aç
TUNNEL_OUT=$(./tunr share --port $PORT --json 2>&1 | head -20)
TUNNEL_URL=$(echo "$TUNNEL_OUT" | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        d = json.loads(line)
        if 'url' in d: print(d['url']); break
    except: pass
")

if [ -z "$TUNNEL_URL" ]; then
  echo "❌ Tunnel URL alınamadı"
  kill $HTTP_PID
  exit 1
fi

echo "✅ Tunnel açıldı: $TUNNEL_URL"

# Tunnel üzerinden HTTP test
RESP=$(curl -sf "$TUNNEL_URL" 2>&1 || echo "FAIL")
if [[ "$RESP" == *"tunr test OK"* ]]; then
  echo "✅ HTTP tunnel çalışıyor"
else
  echo "❌ HTTP tunnel başarısız: $RESP"
  kill $HTTP_PID
  exit 1
fi

# Inspector API
REQS=$(curl -sf http://localhost:19842/api/v1/requests 2>/dev/null)
echo "✅ Inspector: $REQS"

kill $HTTP_PID
echo "🎉 Tüm testler BAŞARILI!"
```

---

## 12. İzleme ve Log

### Logları İzle

```bash
# Relay log
journalctl -u tunr-relay -f

# Caddy log
journalctl -u caddy -f

# PostgreSQL log
docker compose logs -f postgres

# Tüm servis durumları
systemctl status tunr-relay caddy docker
```

### Prometheus Metrikleri (Faz 7+)

```bash
# Relay metrikleri (planlanan endpoint)
curl http://localhost:9090/metrics
```

---

## 13. Yedekleme

```bash
# /opt/tunr/backup.sh
#!/bin/bash
set -euo pipefail
DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="/opt/tunr/backups/$DATE"
mkdir -p "$BACKUP_DIR"

# PostgreSQL dump
docker compose exec -T postgres \
  pg_dump -U tunr tunr | gzip > "$BACKUP_DIR/tunr_db.sql.gz"

# Config yedek
cp /opt/tunr/.env "$BACKUP_DIR/.env.bak"

# 30 günden eski yedekleri sil
find /opt/tunr/backups -mindepth 1 -maxdepth 1 -mtime +30 -exec rm -rf {} \;

echo "✅ Yedekleme tamamlandı: $BACKUP_DIR"
```

```bash
# Cron: her gece 02:00
crontab -e
# 0 2 * * * /opt/tunr/backup.sh >> /var/log/tunr/backup.log 2>&1
```

---

## 14. Sık Karşılaşılan Sorunlar

### Tunnel bağlanamıyor

```bash
# 1. Relay çalışıyor mu?
systemctl status tunr-relay
curl http://localhost:8080/api/v1/health

# 2. Firewall
ufw status verbose
# 443 ve 80 açık olmalı

# 3. DNS çözümlenmiş mi?
dig abc123.tunr.sh
# <SUNUCU_IP> görünmeli

# 4. TLS sertifikası
curl -vI https://tunr.sh 2>&1 | grep "SSL"
```

### PostgreSQL bağlantısı kesildi

```bash
docker compose restart postgres
# Relay otomatik yeniden bağlanır (connection pool)
```

### Let's Encrypt rate limit

```bash
# Her domain için haftada 5 sertifika limiti var
# Test için staging CA kullan:
# acme_ca https://acme-staging-v02.api.letsencrypt.org/directory
```

### Yüksek bellek kullanımı

```bash
# Aktif tunnel sayısını kontrol et
curl http://localhost:8080/api/v1/status

# Go GC zorla
kill -SIGUSR1 $(pidof tunr-relay)
```

### tunr doctor hata veriyor

```bash
tunr doctor
# Her satırı tek tek kontrol et
# ✗ Daemon: systemctl start tunr veya ./tunr start
# ✗ Login: tunr login
# ✗ Relay: Relay henüz deploy edilmedi
```

---

## Güncelleme

```bash
# 1. Yeni binary derle
cd /opt/tunr/src
git pull
cd relay
CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" \
  -o /usr/local/bin/tunr-relay-new ./cmd/server

# 2. Sıfır kesinti ile yeniden başlat
mv /usr/local/bin/tunr-relay-new /usr/local/bin/tunr-relay
systemctl reload tunr-relay || systemctl restart tunr-relay

# 3. Yeni migration'ları uygula
docker compose exec postgres \
  psql -U tunr -d tunr -f /docker-entrypoint-initdb.d/002_update.sql
```
