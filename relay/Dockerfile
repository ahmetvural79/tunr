# tunr relay — Dockerfile
# Multi-stage build: builder + minimal runtime image
# Final image ~15MB — güvenli, küçük, hızlı başlar.

# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

# git ve ca-certificates (HTTPS bağlantısı için)
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Önce dependencies — Docker cache katmanını optimize et
COPY go.mod go.sum ./
RUN go mod download

# Sonra kaynak kod
COPY . .

# CGO_ENABLED=0: tamamen static binary (Alpine compat)
# -trimpath: build path bilgisi binary'ye gömülmesin (güvenlik)
# -ldflags: debug bilgisi ve symbol table sil (küçük binary)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath \
    -ldflags="-w -s" \
    -o /relay ./cmd/server

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM scratch

# ca-certificates: HTTPS bağlantıları için gerekli
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Timezone verisi
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
# Binary kendisi
COPY --from=builder /relay /relay

# GÜVENLİK: root olarak çalışma
# scratch image'da user yok, ama Fly.io'da USER belirleyebiliriz
# USER 1000:1000

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/relay", "health"] || exit 1

ENTRYPOINT ["/relay"]
