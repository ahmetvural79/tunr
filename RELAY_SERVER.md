# Sunucuda Relay Çalıştırma

Relay, `tunr share` ile açılan tünellerin trafiğini yönlendiren sunucu tarafıdır. Bu dosya relay kodunu sunucuda nasıl build edip çalıştıracağını özetler.

## Gereksinimler

- Go 1.22+
- Ortam: `TUNR_JWT_SECRET` (32+ karakter), `TUNR_DOMAIN=tunr.sh`, `PORT=8080`
- İsteğe bağlı: `DATABASE_URL` (PostgreSQL; yoksa in-memory)

## Adımlar

### 1. Relay kodunu sunucuya al

`relay/` public repoda yoksa, kendi private repondan veya yerel makineden kopyala:

```bash
# Örnek: yerel makineden rsync
rsync -avz --exclude '.git' ./relay/ user@SUNUCU_IP:/opt/tunr/src/relay/
```

Veya sunucuda ayrı bir private repo clone et.

### 2. Build

```bash
cd /opt/tunr/src/relay
CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /usr/local/bin/tunr-relay ./cmd/server
```

### 3. Ortam değişkenleri

`/opt/tunr/.env` içinde (veya systemd `EnvironmentFile`):

```
TUNR_JWT_SECRET=...   # en az 32 karakter (dashboard ile aynı)
TUNR_DOMAIN=tunr.sh
PORT=8080
DATABASE_URL=          # opsiyonel
```

### 4. Systemd servisi

```bash
sudo tee /etc/systemd/system/tunr-relay.service << 'EOF'
[Unit]
Description=tunr Relay Server
After=network.target

[Service]
User=tunr
WorkingDirectory=/opt/tunr
EnvironmentFile=/opt/tunr/.env
ExecStart=/usr/local/bin/tunr-relay
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tunr-relay

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now tunr-relay
```

### 5. Kontrol

```bash
systemctl status tunr-relay
journalctl -u tunr-relay -f -n 30
curl -s http://localhost:8080/api/v1/health
```

Caddy’de `relay.tunr.sh` ve `*.tunr.sh` bu sunucunun 8080 portuna yönlendirilmiş olmalı.
